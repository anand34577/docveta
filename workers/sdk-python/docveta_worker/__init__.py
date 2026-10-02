"""Docveta worker SDK.

Implement an OCR engine in a few lines::

    from docveta_worker import Engine, PageResult, Line, run

    class MyEngine(Engine):
        name = "my-ocr"
        version = "1.0"
        languages = ["en"]

        def recognize(self, session, image, page_no, languages):
            ...
            return PageResult(lines=[Line(text="hello", bbox=(10, 10, 80, 30))])

    if __name__ == "__main__":
        run(MyEngine())

The SDK speaks the Docveta worker protocol (hello / lease / heartbeat / complete / fail),
rasterises PDFs, handles multi-page TIFFs and HEIC, and builds searchable PDFs, so an
engine only turns one page image into text lines.
"""

from ._version import __version__
from .engine import Engine, Line, PageResult, Word, EngineError
from .runner import run

__all__ = ["Engine", "Line", "PageResult", "Word", "EngineError", "run", "__version__"]
