"""Builds a searchable PDF: the original page images/PDF pages plus an invisible text
layer positioned from OCR word boxes (works for every engine that reports boxes).

Unicode text needs a TrueType font that covers the script. We look for Noto/DejaVu
fonts on the system; words in scripts without an available font are left out of the
text layer (they are still searchable inside Docveta, which indexes the OCR text itself).
"""

from __future__ import annotations

import io
import logging
import os
import unicodedata
from typing import Any, Optional

from PIL import Image
from pypdf import PdfReader, PdfWriter
from reportlab.lib.utils import ImageReader
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.pdfgen import canvas

from .engine import EngineError

log = logging.getLogger("docveta_worker")

_FONT_DIRS = [
    # Fonts shipped with a packaged engine (e.g. docveta-ocr/fonts) come first.
    *([os.environ["DOCVETA_FONTS_DIR"]] if os.environ.get("DOCVETA_FONTS_DIR") else []),
    "/usr/share/fonts", "/usr/local/share/fonts", os.path.expanduser("~/.fonts"),
    "C:/Windows/Fonts", "/System/Library/Fonts", "/Library/Fonts",
]
_CANDIDATES = {
    # family name -> file names, first found wins
    "latin": ["NotoSans-Regular.ttf", "DejaVuSans.ttf", "LiberationSans-Regular.ttf", "arial.ttf"],
    "devanagari": ["NotoSansDevanagari-Regular.ttf", "Lohit-Devanagari.ttf", "mangal.ttf", "Nirmala.ttf"],
    "bengali": ["NotoSansBengali-Regular.ttf", "Lohit-Bengali.ttf"],
    "tamil": ["NotoSansTamil-Regular.ttf", "Lohit-Tamil.ttf"],
    "telugu": ["NotoSansTelugu-Regular.ttf", "Lohit-Telugu.ttf"],
    "kannada": ["NotoSansKannada-Regular.ttf", "Lohit-Kannada.ttf"],
    "malayalam": ["NotoSansMalayalam-Regular.ttf", "Lohit-Malayalam.ttf"],
    "gujarati": ["NotoSansGujarati-Regular.ttf", "Lohit-Gujarati.ttf"],
    "gurmukhi": ["NotoSansGurmukhi-Regular.ttf", "Lohit-Gurmukhi.ttf"],
    "arabic": ["NotoSansArabic-Regular.ttf", "NotoNaskhArabic-Regular.ttf"],
}
_registered: dict[str, Optional[str]] = {}


def _find_file(names: list[str]) -> Optional[str]:
    for d in _FONT_DIRS:
        if not os.path.isdir(d):
            continue
        for root, _dirs, files in os.walk(d):
            lower = {f.lower(): f for f in files}
            for n in names:
                if n.lower() in lower:
                    return os.path.join(root, lower[n.lower()])
    return None


def _font_for_script(script: str) -> Optional[str]:
    if script in _registered:
        return _registered[script]
    name = None
    path = _find_file(_CANDIDATES.get(script, []))
    if path:
        name = f"kz-{script}"
        try:
            pdfmetrics.registerFont(TTFont(name, path))
        except Exception as e:  # pragma: no cover - bad font file
            log.warning("cannot load font %s: %s", path, e)
            name = None
    _registered[script] = name
    return name


def _script_of(text: str) -> str:
    for ch in text:
        if ch.isspace() or ch.isdigit() or ord(ch) < 0x250:
            continue
        n = unicodedata.name(ch, "")
        for script in ("DEVANAGARI", "BENGALI", "TAMIL", "TELUGU", "KANNADA", "MALAYALAM", "GUJARATI", "GURMUKHI", "ARABIC"):
            if n.startswith(script):
                return script.lower()
        return "latin"  # other scripts: try the general font
    return "ascii"


def _font_for(text: str) -> Optional[str]:
    script = _script_of(text)
    if script == "ascii":
        return _font_for_script("latin") or "Helvetica"
    return _font_for_script(script)


def _draw_words(c: canvas.Canvas, page: dict[str, Any], page_w_pt: float, page_h_pt: float) -> int:
    """Draws invisible words; returns how many were placed."""
    src_w = float(page.get("width") or 0)
    src_h = float(page.get("height") or 0)
    if src_w <= 0 or src_h <= 0:
        return 0
    sx, sy = page_w_pt / src_w, page_h_pt / src_h
    placed = 0
    for block in page.get("blocks", []):
        for line in block.get("lines", []):
            words = line.get("words") or []
            if not words and line.get("bbox") and line.get("text"):
                words = [{"text": line["text"], "bbox": line["bbox"]}]
            for w in words:
                text = (w.get("text") or "").strip()
                bbox = w.get("bbox")
                if not text or not bbox or len(bbox) != 4:
                    continue
                font = _font_for(text)
                if not font:
                    continue
                x0, y0, x1, y1 = bbox
                width = max(1.0, (x1 - x0) * sx)
                height = max(1.0, (y1 - y0) * sy)
                size = max(1.0, height * 0.85)
                try:
                    natural = pdfmetrics.stringWidth(text, font, size)
                except Exception:
                    continue
                t = c.beginText()
                t.setTextRenderMode(3)  # invisible
                t.setFont(font, size)
                if natural > 0:
                    t.setHorizScale(max(1.0, min(1000.0, width / natural * 100)))
                # PDF origin is bottom-left; baseline slightly above the box bottom.
                t.setTextOrigin(x0 * sx, page_h_pt - y1 * sy + height * 0.15)
                t.textOut(text + " ")
                c.drawText(t)
                placed += 1
    return placed


def _overlay(page: dict[str, Any], w_pt: float, h_pt: float) -> Optional[bytes]:
    buf = io.BytesIO()
    c = canvas.Canvas(buf, pagesize=(w_pt, h_pt))
    n = _draw_words(c, page, w_pt, h_pt)
    c.showPage()
    c.save()
    return buf.getvalue() if n else None


def build_searchable_pdf(src: str, mime: str, ocr: dict[str, Any], out: str) -> None:
    pages = {int(p["page"]): p for p in ocr.get("pages", [])}
    if mime == "application/pdf":
        _from_pdf(src, pages, out)
    elif mime.startswith("image/"):
        _from_image(src, pages, out)
    else:
        raise EngineError("unsupported_input", f"cannot build a PDF from {mime}")


def _from_pdf(src: str, pages: dict[int, dict[str, Any]], out: str) -> None:
    try:
        reader = PdfReader(src)
    except Exception as e:
        raise EngineError("corrupt_input", f"cannot read PDF: {e}") from e
    if reader.is_encrypted:
        raise EngineError("unsupported_input", "PDF is encrypted")
    writer = PdfWriter(clone_from=reader)
    for i, page in enumerate(writer.pages, start=1):
        ocr_page = pages.get(i)
        if not ocr_page:
            continue
        # Make rotation part of the content so overlay coordinates match the rendered view.
        if page.get("/Rotate", 0):
            page.transfer_rotation_to_content()
        box = page.mediabox
        w_pt, h_pt = float(box.width), float(box.height)
        data = _overlay(ocr_page, w_pt, h_pt)
        if data:
            overlay = PdfReader(io.BytesIO(data)).pages[0]
            if float(box.left) or float(box.bottom):
                from pypdf import Transformation

                overlay.add_transformation(Transformation().translate(float(box.left), float(box.bottom)))
            page.merge_page(overlay)
    writer.compress_identical_objects()
    with open(out, "wb") as f:
        writer.write(f)


def _from_image(src: str, pages: dict[int, dict[str, Any]], out: str) -> None:
    from PIL import ImageOps

    with Image.open(src) as img:
        _images_to_pdf(img, pages, out)


def _images_to_pdf(img: Image.Image, pages: dict[int, dict[str, Any]], out: str) -> None:
    from PIL import ImageOps

    frames = getattr(img, "n_frames", 1)
    c = canvas.Canvas(out)
    for i in range(frames):
        img.seek(i)
        frame = ImageOps.exif_transpose(img.copy()) if i == 0 else img.copy()
        frame = frame.convert("RGB")
        page = pages.get(i + 1, {})
        dpi = float(page.get("dpi") or (img.info.get("dpi") or (300, 300))[0] or 300)
        w_pt, h_pt = frame.width * 72.0 / dpi, frame.height * 72.0 / dpi
        c.setPageSize((w_pt, h_pt))
        bio = io.BytesIO()
        frame.save(bio, format="JPEG", quality=85, optimize=True)
        bio.seek(0)
        c.drawImage(ImageReader(bio), 0, 0, width=w_pt, height=h_pt)
        if page:
            # OCR coordinates refer to the same (EXIF-corrected) image.
            page = dict(page)
            page.setdefault("width", frame.width)
            page.setdefault("height", frame.height)
            _draw_words(c, page, w_pt, h_pt)
        c.showPage()
    c.save()
