"""Docveta CPU OCR worker using Tesseract 5. Works on any machine (x86 or ARM) and supports
100+ languages, including Hindi, Marathi, Bengali, Tamil, Telugu, Kannada, Malayalam,
Gujarati and Punjabi. Use it as the universal fallback next to an NPU worker.
"""

from __future__ import annotations

import os
import subprocess
from typing import Optional

import pytesseract
from PIL import Image
from pytesseract import Output

from docveta_worker import Engine, EngineError, Line, PageResult, Word, run

# ISO 639-1 (what Docveta uses) -> Tesseract traineddata names.
ISO_TO_TESS = {
    "en": "eng", "hi": "hin", "mr": "mar", "bn": "ben", "gu": "guj", "ta": "tam", "te": "tel", "kn": "kan",
    "ml": "mal", "pa": "pan", "ur": "urd", "or": "ori", "as": "asm", "ne": "nep", "sa": "san",
    "de": "deu", "fr": "fra", "es": "spa", "it": "ita", "pt": "por", "nl": "nld", "sv": "swe", "da": "dan",
    "no": "nor", "fi": "fin", "pl": "pol", "cs": "ces", "ru": "rus", "uk": "ukr", "tr": "tur", "ar": "ara",
    "fa": "fas", "he": "heb", "ja": "jpn", "ko": "kor", "zh": "chi_sim", "th": "tha", "vi": "vie", "id": "ind",
}
TESS_TO_ISO = {v: k for k, v in ISO_TO_TESS.items()}


def installed_languages() -> list[str]:
    try:
        return list(pytesseract.get_languages(config=""))
    except Exception:
        return ["eng"]


class TesseractEngine(Engine):
    name = "tesseract"
    tags = ("cpu",)
    max_pages_per_task = 20

    def __init__(self) -> None:
        self.version = str(pytesseract.get_tesseract_version())
        all_langs = installed_languages()
        self.has_osd = "osd" in all_langs
        self.tess_langs = [l for l in all_langs if l != "osd"]
        self.languages = sorted({TESS_TO_ISO[l] for l in self.tess_langs if l in TESS_TO_ISO}) or ["en"]
        cpus = os.cpu_count() or 2
        # Tesseract is single-threaded per page with OMP_THREAD_LIMIT=1; run pages in parallel.
        self.concurrency = int(os.environ.get("DOCVETA_CONCURRENCY", max(1, cpus // 2)))
        self.psm = os.environ.get("DOCVETA_TESSERACT_PSM", "3")

    def _lang_arg(self, languages: list[str]) -> str:
        codes = [ISO_TO_TESS.get(l, l) for l in languages]
        codes = [c for c in codes if c in self.tess_langs]
        # Always include English: Indian documents mix English with the local script.
        if "eng" in self.tess_langs and "eng" not in codes:
            codes.append("eng")
        if not codes:
            raise EngineError("language_unavailable", f"none of {languages} installed (have {self.tess_langs})")
        return "+".join(codes)

    def _orientation(self, image: Image.Image) -> int:
        """Detects pages scanned sideways/upside down (needs the 'osd' traineddata)."""
        if not self.has_osd:
            return 0
        try:
            osd = pytesseract.image_to_osd(image, output_type=Output.DICT)
            if float(osd.get("orientation_conf", 0)) > 2:
                return int(osd.get("rotate", 0)) % 360
        except pytesseract.TesseractError:
            pass
        return 0

    def recognize(self, session, image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        lang = self._lang_arg(languages)
        rotate = self._orientation(image)
        work = image.rotate(-rotate, expand=True) if rotate else image
        try:
            data = pytesseract.image_to_data(work, lang=lang, config=f"--psm {self.psm}", output_type=Output.DICT, timeout=600)
        except RuntimeError as e:  # timeout
            raise EngineError("timeout", str(e), retryable=True) from e
        except pytesseract.TesseractError as e:
            raise EngineError("engine_error", str(e)) from e

        W, H = work.size
        lines: dict[tuple[int, int, int], Line] = {}
        order: list[tuple[int, int, int]] = []
        block_of: dict[tuple[int, int, int], int] = {}
        for i, text in enumerate(data["text"]):
            text = (text or "").strip()
            conf = float(data["conf"][i])
            if not text or conf < 0:
                continue
            x, y, w, h = data["left"][i], data["top"][i], data["width"][i], data["height"][i]
            box = _unrotate((x, y, x + w, y + h), rotate, W, H)
            key = (data["block_num"][i], data["par_num"][i], data["line_num"][i])
            if key not in lines:
                lines[key] = Line(text="", words=[])
                order.append(key)
                block_of[key] = data["block_num"][i]
            lines[key].words.append(Word(text, box, conf / 100.0))

        out: list[Line] = []
        blocks: dict[int, list[int]] = {}
        for key in order:
            l = lines[key]
            l.text = " ".join(w.text for w in l.words)
            xs = [c for w in l.words for c in (w.bbox[0], w.bbox[2])]
            ys = [c for w in l.words for c in (w.bbox[1], w.bbox[3])]
            l.bbox = (min(xs), min(ys), max(xs), max(ys))
            confs = [w.confidence for w in l.words if w.confidence is not None]
            l.confidence = sum(confs) / len(confs) if confs else None
            blocks.setdefault(block_of[key], []).append(len(out))
            out.append(l)
        return PageResult(lines=out, rotation=rotate, language=languages[0] if languages else None, blocks=list(blocks.values()))


def _unrotate(box: tuple[float, float, float, float], rotate: int, W: int, H: int) -> tuple[float, float, float, float]:
    """Maps a box from the rotated work image back to the original image."""
    x0, y0, x1, y1 = box
    if rotate == 0:
        return box
    if rotate == 180:
        return (W - x1, H - y1, W - x0, H - y0)
    if rotate == 90:  # work = original rotated 90° clockwise
        return (y0, W - x1, y1, W - x0)
    if rotate == 270:
        return (H - y1, x0, H - y0, x1)
    return box


def _check_binary() -> Optional[str]:
    try:
        subprocess.run(["tesseract", "--version"], capture_output=True, check=True)
        return None
    except Exception as e:
        return str(e)


if __name__ == "__main__":
    os.environ.setdefault("OMP_THREAD_LIMIT", "1")
    err = _check_binary()
    if err:
        raise SystemExit(f"tesseract binary not found: {err}")
    run(TesseractEngine())
