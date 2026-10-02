"""Turning input files into page images: PDFs (pdfium), images, multi-page TIFF, HEIC."""

from __future__ import annotations

from typing import Iterator, Optional

from PIL import Image, ImageOps

from .engine import EngineError

Image.MAX_IMAGE_PIXELS = 150_000_000  # refuse decompression bombs

try:  # optional HEIC/AVIF support
    import pillow_heif  # type: ignore

    pillow_heif.register_heif_opener()
    HEIF = True
except Exception:  # pragma: no cover - optional dependency
    HEIF = False


class PageImage:
    def __init__(self, page_no: int, image: Image.Image, dpi: float):
        self.page_no = page_no
        self.image = image
        self.dpi = dpi


def pdf_pages(path: str, page_from: Optional[int], page_to: Optional[int], dpi: int) -> Iterator[PageImage]:
    import pypdfium2 as pdfium  # imported lazily: heavy

    try:
        pdf = pdfium.PdfDocument(path)
    except pdfium.PdfiumError as e:  # type: ignore[attr-defined]
        msg = str(e).lower()
        if "password" in msg:
            raise EngineError("unsupported_input", "PDF is password protected") from e
        raise EngineError("corrupt_input", f"cannot open PDF: {e}") from e
    try:
        n = len(pdf)
        first = max(1, page_from or 1)
        last = min(n, page_to or n)
        for no in range(first, last + 1):
            page = pdf[no - 1]
            try:
                # Cap the longest side so huge-format pages don't exhaust memory.
                w_pt, h_pt = page.get_size()
                scale = dpi / 72.0
                longest = max(w_pt, h_pt) * scale
                if longest > 7000:
                    scale *= 7000 / longest
                bitmap = page.render(scale=scale, rotation=0, fill_color=(255, 255, 255, 255))
                img = bitmap.to_pil().convert("RGB")
            finally:
                page.close()
            yield PageImage(no, img, scale * 72.0)
    finally:
        pdf.close()


def image_pages(path: str, page_from: Optional[int], page_to: Optional[int]) -> Iterator[PageImage]:
    try:
        img = Image.open(path)
    except Exception as e:
        if not HEIF and path.lower().endswith((".heic", ".heif", ".avif")):
            raise EngineError("unsupported_input", "HEIC/AVIF needs pillow-heif installed") from e
        raise EngineError("unsupported_input" if "cannot identify" in str(e) else "corrupt_input", f"cannot open image: {e}") from e
    with img:
        frames = getattr(img, "n_frames", 1)
        first = max(1, page_from or 1)
        last = min(frames, page_to or frames)
        for no in range(first, last + 1):
            try:
                img.seek(no - 1)
            except EOFError:
                break
            frame = ImageOps.exif_transpose(img.copy()) if no == 1 else img.copy()
            dpi = float((img.info.get("dpi") or (300, 300))[0] or 300)
            if dpi < 50 or dpi > 1200:
                dpi = 300.0
            yield PageImage(no, frame.convert("RGB"), dpi)


def iter_pages(path: str, mime: str, page_from: Optional[int], page_to: Optional[int], dpi: int) -> Iterator[PageImage]:
    if mime == "application/pdf":
        return pdf_pages(path, page_from, page_to, dpi)
    if mime.startswith("image/"):
        return image_pages(path, page_from, page_to)
    raise EngineError("unsupported_input", f"unsupported type {mime}")
