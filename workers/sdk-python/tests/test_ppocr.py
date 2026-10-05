"""Hardware-independent tests for the PP-OCR pre/post-processing.

Run: python -m unittest test_ppocr   (needs numpy and opencv-python-headless)
"""

import unittest

import numpy as np

from docveta_worker import ppocr


class DetectionTest(unittest.TestCase):
    def test_db_postprocess_finds_rectangles(self):
        prob = np.zeros((200, 400), dtype=np.float32)
        prob[50:70, 40:300] = 0.95   # a text line
        prob[120:135, 60:160] = 0.9  # another
        boxes = ppocr.sort_boxes(ppocr.db_postprocess(prob))
        self.assertEqual(len(boxes), 2)
        x0, y0, x1, y1 = boxes[0].rect
        # Unclipping expands the shrunk DB region a little in every direction.
        self.assertLess(x0, 40)
        self.assertGreater(x1, 299)
        self.assertLess(y0, 50)
        self.assertGreater(y1, 69)
        self.assertLess(boxes[0].rect[1], boxes[1].rect[1])

    def test_low_score_regions_are_dropped(self):
        prob = np.zeros((100, 100), dtype=np.float32)
        prob[10:30, 10:80] = 0.4  # above bitmap threshold, below box threshold
        self.assertEqual(ppocr.db_postprocess(prob), [])

    def test_tiled_detection_maps_back_to_page_coordinates(self):
        size = 320
        page = np.full((1200, 900, 3), 255, dtype=np.uint8)
        calls = []

        def infer(x):
            calls.append(x.shape)
            # Pretend every tile has one line at the same tile position.
            out = np.zeros((1, 1, size, size), dtype=np.float32)
            out[0, 0, 100:120, 40:200] = 0.95
            return out

        boxes = ppocr.detect(page, infer, size)
        self.assertGreater(len(calls), 1)
        self.assertTrue(all(s == (1, size, size, 3) for s in calls))
        for b in boxes:
            x0, y0, x1, y1 = b.rect
            self.assertGreaterEqual(x0, 0)
            self.assertLessEqual(x1, 900)
            self.assertLessEqual(y1, 1200)

    def test_merge_joins_fragments_of_one_line(self):
        a = ppocr.TextBox(np.array([[0, 0], [100, 0], [100, 20], [0, 20]], dtype=np.float32), 0.9)
        b = ppocr.TextBox(np.array([[105, 1], [200, 1], [200, 21], [105, 21]], dtype=np.float32), 0.8)
        c = ppocr.TextBox(np.array([[0, 100], [50, 100], [50, 120], [0, 120]], dtype=np.float32), 0.8)
        merged = ppocr.merge_boxes([a, b, c])
        self.assertEqual(len(merged), 2)


class RecognitionTest(unittest.TestCase):
    def test_rec_input_buckets(self):
        crop = np.zeros((30, 300, 3), dtype=np.uint8)  # -> width 480 at height 48
        x, bucket, content = ppocr.rec_input(crop)
        self.assertEqual(x.shape, (1, 48, 640, 3))
        self.assertEqual(bucket, 640)
        self.assertEqual(content, 480)
        huge = np.zeros((10, 2000, 3), dtype=np.uint8)
        x, bucket, content = ppocr.rec_input(huge)
        self.assertEqual(bucket, 1280)
        self.assertEqual(content, 1280)

    def test_ctc_decode_with_word_positions(self):
        charset = ["a", "b", "c", " "]  # index 1..4; 0 is blank
        seq = [1, 1, 0, 2, 0, 4, 0, 3, 3, 0]  # "ab c"
        probs = np.full((len(seq), 5), 0.01, dtype=np.float32)
        for t, k in enumerate(seq):
            probs[t, k] = 0.9
        r = ppocr.ctc_decode(probs, charset)
        self.assertEqual(r.text, "ab c")
        self.assertAlmostEqual(r.confidence, 0.9, places=4)
        self.assertEqual([w for w, _, _ in r.words], ["ab", "c"])
        self.assertLess(r.words[0][2], r.words[1][1] + 1e-6)

    def test_crop_rotates_vertical_text(self):
        img = np.zeros((200, 200, 3), dtype=np.uint8)
        box = ppocr.TextBox(np.array([[10, 10], [30, 10], [30, 150], [10, 150]], dtype=np.float32), 0.9)
        crop = ppocr.crop_box(img, box)
        self.assertGreater(crop.shape[1], crop.shape[0])


def _box(i: int, width: int = 300) -> "ppocr.TextBox":
    y = 10 + i * 40
    return ppocr.TextBox(np.array([[10, y], [10 + width, y], [10 + width, y + 30], [10, y + 30]], dtype=np.float32), 0.9)


class ScriptDetectionTest(unittest.TestCase):
    """read_lines picks the page's script from the text itself, not only the language hint."""

    def reader(self, truth: dict, calls: list):
        # truth[i] = (script the line is written in, its text); the right script reads it at 0.95,
        # any other script produces a weak 0.3 guess.
        def read(box, script):
            i = int((box.rect[1] - 10) // 40)
            calls.append((i, script))
            real, text = truth[i]
            return ppocr.RecResult(text if script == real else "x?", 0.95 if script == real else 0.3, [])
        return read

    def test_hindi_page_with_english_hint(self):
        truth = {i: ("devanagari", f"पंक्ति {i}") for i in range(5)}
        calls: list = []
        boxes = [_box(i) for i in range(5)]
        found, script = ppocr.read_lines(boxes, self.reader(truth, calls), ["en"], ["devanagari", "en", "ta"])
        self.assertEqual(script, "devanagari")
        self.assertEqual([r.text for _, r in found], [t for _, t in truth.values()])
        self.assertEqual(ppocr.page_language(["en"], script), "hi")

    def test_english_page_stays_fast(self):
        truth = {i: ("en", f"line {i}") for i in range(20)}
        calls: list = []
        found, script = ppocr.read_lines([_box(i) for i in range(20)], self.reader(truth, calls), ["en"], ["devanagari", "en", "ta"])
        self.assertEqual(script, "en")
        self.assertEqual(len(found), 20)
        self.assertEqual({s for _, s in calls}, {"en"})  # other models never ran
        self.assertEqual(ppocr.page_language(["en-IN"], script), "en-IN")

    def test_mixed_page_reads_each_line_in_its_script(self):
        truth = {i: ("en", f"line {i}") for i in range(10)}
        truth[3] = ("devanagari", "नाम")
        truth[7] = ("devanagari", "पता")
        found, script = ppocr.read_lines([_box(i) for i in range(10)], self.reader(truth, []), ["en"], ["devanagari", "en"])
        self.assertEqual(script, "en")
        self.assertEqual([r.text for _, r in found], [t for _, t in truth.values()])

    def test_hindi_line_the_english_reader_garbles_confidently(self):
        # The real English model reads भारत सरकार as "HRd HRQR" at 0.76: still worth a second look.
        def read(box, script):
            i = int((box.rect[1] - 10) // 40)
            if i == 4:
                return ppocr.RecResult("HRd HRQR" if script == "en" else "भारत सरकार", 0.76 if script == "en" else 0.99, [])
            return ppocr.RecResult(f"line {i}", 0.99 if script == "en" else 0.9, [])
        found, script = ppocr.read_lines([_box(i) for i in range(5)], read, ["en"], ["en", "devanagari"])
        self.assertEqual(script, "en")
        self.assertEqual(found[4][1].text, "भारत सरकार")

    def test_noise_is_not_turned_into_another_script(self):
        def read(box, script):
            return ppocr.RecResult("~~", 0.55 if script == "en" else 0.6, [])  # unreadable for everyone
        found, _ = ppocr.read_lines([_box(0)], read, ["en"], ["devanagari", "en"])
        self.assertEqual([r.text for _, r in found], ["~~"])  # the English guess, not a "better" foreign one

    def test_single_script_and_empty(self):
        self.assertEqual(ppocr.read_lines([], lambda b, s: None, ["hi"], ["en"]), ([], "en"))
        found, script = ppocr.read_lines([_box(0)], lambda b, s: None, ["hi"], ["en"])
        self.assertEqual((found, script), ([], "en"))


if __name__ == "__main__":
    unittest.main()
