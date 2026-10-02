"""SDK tests that don't need a Docveta server: rasterisation, result format, searchable PDF.

Run: python -m unittest discover -s tests
"""

import io
import json
import os
import tempfile
import unittest

from PIL import Image, ImageDraw
from pypdf import PdfReader
from reportlab.pdfgen import canvas

from docveta_worker.mock import MockEngine
from docveta_worker.pages import iter_pages
from docveta_worker.runner import page_json
from docveta_worker.textlayer import build_searchable_pdf


def scanned_pdf(path: str, pages: int = 2) -> None:
    """A PDF whose pages are images only (like a scanner produces)."""
    c = canvas.Canvas(path, pagesize=(595, 842))
    for i in range(pages):
        img = Image.new("RGB", (1240, 1754), "white")
        ImageDraw.Draw(img).rectangle([100, 100, 1100, 300], outline="black", width=5)
        bio = io.BytesIO()
        img.save(bio, "PNG")
        bio.seek(0)
        from reportlab.lib.utils import ImageReader

        c.drawImage(ImageReader(bio), 0, 0, 595, 842)
        c.showPage()
    c.save()


class SDKTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name

    def tearDown(self):
        self.tmp.cleanup()

    def ocr(self, path, mime, page_from=None, page_to=None):
        eng = MockEngine()
        pages = []
        for p in iter_pages(path, mime, page_from, page_to, 150):
            r = eng.recognize(None, p.image, p.page_no, ["en"])
            pages.append(page_json(p.page_no, p.image.width, p.image.height, p.dpi, r))
        return {"schema": "ocr-result/v1", "engine": {"name": "mock", "version": "1"}, "pages": pages}

    def test_pdf_page_range_and_result_format(self):
        src = os.path.join(self.dir, "scan.pdf")
        scanned_pdf(src, 3)
        res = self.ocr(src, "application/pdf", 2, 3)
        self.assertEqual([p["page"] for p in res["pages"]], [2, 3])
        p = res["pages"][0]
        self.assertAlmostEqual(p["dpi"], 150, delta=1)
        self.assertIn("Mock page 2", p["text"])
        word = p["blocks"][0]["lines"][0]["words"][0]
        self.assertEqual(len(word["bbox"]), 4)
        json.dumps(res)  # serialisable

    def test_searchable_pdf_from_pdf(self):
        src = os.path.join(self.dir, "scan.pdf")
        scanned_pdf(src, 2)
        res = self.ocr(src, "application/pdf")
        out = os.path.join(self.dir, "out.pdf")
        build_searchable_pdf(src, "application/pdf", res, out)
        reader = PdfReader(out)
        self.assertEqual(len(reader.pages), 2)
        text = reader.pages[1].extract_text()
        self.assertIn("Electricity", text)
        self.assertIn("page", text)

    def test_searchable_pdf_from_image_with_exif_rotation(self):
        src = os.path.join(self.dir, "photo.jpg")
        img = Image.new("RGB", (800, 600), "white")
        exif = Image.Exif()
        exif[0x0112] = 6  # rotated 90° CW: displays as portrait
        img.save(src, "JPEG", exif=exif.tobytes())
        res = self.ocr(src, "image/jpeg")
        self.assertEqual((res["pages"][0]["width"], res["pages"][0]["height"]), (600, 800))
        out = os.path.join(self.dir, "out.pdf")
        build_searchable_pdf(src, "image/jpeg", res, out)
        reader = PdfReader(out)
        box = reader.pages[0].mediabox
        self.assertGreater(float(box.height), float(box.width))  # portrait
        self.assertIn("Amount", reader.pages[0].extract_text())

    def test_multipage_tiff(self):
        src = os.path.join(self.dir, "fax.tiff")
        frames = [Image.new("RGB", (400, 500), c) for c in ("white", "white", "white")]
        frames[0].save(src, save_all=True, append_images=frames[1:])
        res = self.ocr(src, "image/tiff")
        self.assertEqual([p["page"] for p in res["pages"]], [1, 2, 3])

    def test_corrupt_pdf_is_reported(self):
        from docveta_worker.engine import EngineError

        src = os.path.join(self.dir, "bad.pdf")
        with open(src, "wb") as f:
            f.write(b"%PDF-1.7 garbage")
        with self.assertRaises(EngineError) as ctx:
            list(iter_pages(src, "application/pdf", None, None, 150))
        self.assertEqual(ctx.exception.code, "corrupt_input")


if __name__ == "__main__":
    unittest.main()
