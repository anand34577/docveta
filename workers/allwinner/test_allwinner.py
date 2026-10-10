"""Tests for the Allwinner worker's tensor (de)quantisation (no NPU needed).

    python -m unittest test_allwinner      (from workers/allwinner, with the SDK installed)
"""

import unittest

import numpy as np

import worker as w


def roundtrip(x, fmt, quant, **q):
    out = np.zeros(x.shape, dtype=w.NP_DTYPE[fmt])
    w.quantize(x, out, fmt, quant, **q)
    return out, w.dequantize(out, fmt, quant, **q)


class Quantisation(unittest.TestCase):
    pixels = np.arange(256, dtype=np.uint8).reshape(1, 1, 16, 16)

    def test_uint8_identity_is_raw_copy(self):
        out, back = roundtrip(self.pixels, w.FMT_UINT8, w.Q_AFFINE, scale=1.0, zero_point=0)
        np.testing.assert_array_equal(out, self.pixels)
        np.testing.assert_array_equal(back, self.pixels.astype(np.float32))

    def test_uint8_affine(self):
        out, back = roundtrip(self.pixels, w.FMT_UINT8, w.Q_AFFINE, scale=2.0, zero_point=10)
        self.assertEqual(out[0, 0, 0, 0], 10)
        self.assertEqual(out.max(), 138)  # rint(255/2 + 10)
        np.testing.assert_allclose(back, self.pixels, atol=1.0)

    def test_int16_dfp(self):
        _, back = roundtrip(self.pixels, w.FMT_INT16, w.Q_DFP, fl=7)
        np.testing.assert_allclose(back, self.pixels, atol=1 / 128)

    def test_int16_dfp_clips(self):
        out, _ = roundtrip(self.pixels, w.FMT_INT16, w.Q_DFP, fl=10)  # 255*1024 > int16
        self.assertEqual(out.max(), 32767)

    def test_bf16_and_fp16(self):
        x = np.linspace(0, 1, 64, dtype=np.float32).reshape(1, 1, 8, 8)
        _, back = roundtrip(x, w.FMT_BFP16, w.Q_NONE)
        np.testing.assert_allclose(back, x, atol=1 / 128)
        _, back = roundtrip(x, w.FMT_FP16, w.Q_NONE)
        np.testing.assert_allclose(back, x, atol=1e-3)

    def test_nchw_from_nhwc_view(self):
        nhwc = np.random.default_rng(0).integers(0, 256, (1, 4, 6, 3), dtype=np.uint8)
        out = np.zeros((1, 3, 4, 6), dtype=np.uint8)
        w.quantize(nhwc.transpose(0, 3, 1, 2), out, w.FMT_UINT8, w.Q_AFFINE)
        np.testing.assert_array_equal(out[0, 1], nhwc[0, :, :, 1])


class InputNormalisation(unittest.TestCase):
    """The NPU input must equal the ONNX engine's PP-OCR detection preprocessing (BGR, normalised)."""

    img = np.random.default_rng(1).integers(0, 256, (1, 6, 8, 3), dtype=np.uint8)

    def npu_input(self, norm, dtype, fmt, quant, **q):
        out = np.zeros((1, 3, 6, 8), dtype=dtype)
        w.write_input(w.input_lut(norm, np.dtype(dtype), fmt, quant, **q), self.img, out)
        return w.dequantize(out, fmt, quant, **q)

    def test_detection_uint8(self):  # quantisation reported by det.nb on a Cubie A7A
        got = self.npu_input(w.DET_NORM, np.uint8, w.FMT_UINT8, w.Q_AFFINE, scale=0.0186584, zero_point=114)
        bgr = self.img[..., ::-1].astype(np.float32) / 255
        ref = ((bgr - [0.485, 0.456, 0.406]) / [0.229, 0.224, 0.225]).transpose(0, 3, 1, 2)
        np.testing.assert_allclose(got, ref, atol=0.0186584 / 2 + 1e-6)


    def test_recognition_matches_cpu_input(self):
        """A line strip on the NPU (uint8, grey padding) equals the CPU's float input."""
        from docveta_worker import ppocr

        crop = np.random.default_rng(2).integers(0, 256, (30, 100, 3), dtype=np.uint8)
        ref, _ = ppocr.rec_input_float(crop)  # (1, 3, 48, 160), padding 0
        x, bucket, content = ppocr.rec_input(crop, (160,))
        scale, zp = 2 / 255, 128  # a uint8 quantisation covering [-1, 1]
        got = self.npu_input_from(x, w.REC_NORM, scale, zp)
        np.testing.assert_allclose(got[..., :content], ref[..., :content], atol=scale / 2 + 1e-6)
        np.testing.assert_allclose(got[..., content:], 0, atol=scale)  # padding is grey, not black

    @staticmethod
    def npu_input_from(x, norm, scale, zp):
        out = np.zeros((1, 3) + x.shape[1:3], dtype=np.uint8)
        w.write_input(w.input_lut(norm, np.dtype(np.uint8), w.FMT_UINT8, w.Q_AFFINE, scale=scale, zero_point=zp), x, out)
        return w.dequantize(out, w.FMT_UINT8, w.Q_AFFINE, scale=scale, zero_point=zp)


class FakeNet:
    """Stands in for an NPU reader: reports the input width it got as one 'character' per 160 px."""

    def __init__(self, width, calls):
        self.width, self.calls = width, calls

    def run(self, x):
        self.calls.append(x.shape[2])
        probs = np.zeros((1, 40, 3), np.float32)
        probs[0, :, 0] = 1  # blank everywhere...
        probs[0, 0, :] = [0, 1, 0]  # ...but one "a" at the start
        return probs


class Reading(unittest.TestCase):
    """Every line is read on the NPU, wide ones in pieces cut at word gaps (fake readers, no NPU)."""

    def models(self, calls):
        m = w.Models.__new__(w.Models)
        m.stats = {"npu": 0, "split": 0, "cpu": 0}
        m.npu_rec = {"en": {320: FakeNet(320, calls), 640: FakeNet(640, calls)}}
        m.charsets = {"en": ["a", " "]}
        m.read_cpu = lambda crop, s: self.fail("read on the CPU")
        return m

    def read(self, m, line):
        from docveta_worker import ppocr

        h, width = line.shape[:2]
        box = ppocr.TextBox(np.array([[0, 0], [width - 1, 0], [width - 1, h - 1], [0, h - 1]], dtype=np.float32), 1.0)
        return m.read(line, box, "en")

    def test_short_lines_use_the_narrowest_reader_that_fits(self):
        calls = []
        r = self.read(self.models(calls), np.full((48, 300, 3), 255, np.uint8))
        self.assertEqual((r.text, calls), ("a", [320]))

    def test_wide_lines_are_cut_at_word_gaps_and_stay_on_the_npu(self):
        line = np.full((48, 1500, 3), 255, np.uint8)
        for x in range(0, 1500, 100):  # "words" 70 px wide, 30 px apart
            line[10:38, x:x + 70] = 0
        calls = []
        m = self.models(calls)
        r = self.read(m, line)
        self.assertEqual(m.stats, {"npu": 1, "split": 1, "cpu": 0})
        self.assertTrue(all(c <= 640 for c in calls) and len(calls) == 3)
        self.assertEqual(r.text, "a a a")  # pieces joined with spaces: they were cut in gaps


if __name__ == "__main__":
    unittest.main()
