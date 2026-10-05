"""Engine interface and canonical result types (ocr-result/v1)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Optional, Sequence

from PIL import Image

BBox = tuple[float, float, float, float]  # x0, y0, x1, y1 in image pixels, origin top-left


@dataclass
class Word:
    text: str
    bbox: BBox
    confidence: Optional[float] = None

    def to_json(self) -> dict[str, Any]:
        d: dict[str, Any] = {"text": self.text, "bbox": [round(v, 1) for v in self.bbox]}
        if self.confidence is not None:
            d["confidence"] = round(float(self.confidence), 4)
        return d


@dataclass
class Line:
    text: str
    bbox: Optional[BBox] = None
    confidence: Optional[float] = None
    words: list[Word] = field(default_factory=list)

    def to_json(self) -> dict[str, Any]:
        d: dict[str, Any] = {"text": self.text}
        if self.bbox is not None:
            d["bbox"] = [round(v, 1) for v in self.bbox]
        if self.confidence is not None:
            d["confidence"] = round(float(self.confidence), 4)
        if self.words:
            d["words"] = [w.to_json() for w in self.words]
        return d


@dataclass
class PageResult:
    """What an engine returns for one page image.

    Coordinates are in the pixel space of the image passed to ``recognize`` — even if
    the engine internally rotates the image, it must map boxes back. ``rotation`` reports
    how the content is rotated (0/90/180/270) for display purposes.
    """

    lines: list[Line] = field(default_factory=list)
    rotation: int = 0
    language: Optional[str] = None
    confidence: Optional[float] = None
    # Optional block grouping: list of lists of line indexes. Default: one block.
    blocks: Optional[list[list[int]]] = None

    def text(self) -> str:
        return "\n".join(l.text for l in self.lines if l.text)

    def mean_confidence(self) -> Optional[float]:
        if self.confidence is not None:
            return self.confidence
        confs = [l.confidence for l in self.lines if l.confidence is not None]
        return sum(confs) / len(confs) if confs else None


class EngineError(Exception):
    """Raise from an engine to report a failure with a protocol error code.

    Codes: unsupported_input, corrupt_input, language_unavailable, engine_error,
    resource_exhausted, timeout.
    """

    def __init__(self, code: str, message: str, retryable: bool = False):
        super().__init__(message)
        self.code = code
        self.retryable = retryable


class Engine:
    """Base class for OCR engines.

    Subclasses set the class attributes and implement :meth:`recognize`. If the engine
    needs per-thread state (for example one NPU context per core), override
    :meth:`open_session`; the runner creates ``concurrency`` sessions and never shares
    one between threads.
    """

    name: str = "engine"
    version: str = "0"
    languages: Sequence[str] = ("en",)
    tags: Sequence[str] = ()
    concurrency: int = 1
    max_pages_per_task: int = 50
    # Input types the worker accepts. PDFs are rasterised by the SDK; images are decoded
    # by Pillow (HEIC/AVIF need the optional pillow-heif package).
    input_mime: Sequence[str] = ("application/pdf", "image/*")
    dpi: int = 300
    models: dict[str, str] = {}

    def open_session(self, slot: int) -> Any:  # noqa: ARG002 - default ignores slot
        return None

    def close_session(self, session: Any) -> None:
        pass

    def recognize(self, session: Any, image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        raise NotImplementedError

    def describe(self) -> str:
        return f"{self.name} {self.version}"
