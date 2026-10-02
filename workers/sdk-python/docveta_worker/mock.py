"""A fake OCR engine for development and end-to-end tests. It doesn't read the image;
it returns a fixed sentence with plausible word boxes so the whole pipeline (routing,
leasing, merging, search, searchable PDF) can be exercised without real OCR.

    DOCVETA_URL=http://localhost:8080 DOCVETA_WORKER_TOKEN=dvt_wrk_... docveta-worker-mock
"""

from __future__ import annotations

import time

from PIL import Image

from .engine import Engine, Line, PageResult, Word
from .runner import run


class MockEngine(Engine):
    name = "mock"
    version = "1.0"
    languages = ("en", "hi")
    tags = ("cpu", "mock")
    concurrency = 2

    def recognize(self, session, image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        time.sleep(0.2)
        w, h = image.size
        lines = []
        texts = [f"Mock page {page_no}", "Electricity bill dated 05/08/2026", "Amount due 1842.00 INR"]
        for i, text in enumerate(texts):
            y0 = h * (0.1 + i * 0.06)
            y1 = y0 + h * 0.035
            words, x = [], w * 0.1
            for t in text.split():
                ww = w * 0.012 * len(t)
                words.append(Word(t, (x, y0, x + ww, y1), 0.99))
                x += ww + w * 0.01
            lines.append(Line(text, (w * 0.1, y0, x, y1), 0.99, words))
        return PageResult(lines=lines, language=languages[0] if languages else "en")


def main() -> None:
    run(MockEngine())


if __name__ == "__main__":
    main()
