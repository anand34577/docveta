# docveta-worker (Python SDK)

Build OCR / processing workers for [Docveta](../../README.md). The SDK implements the
Docveta worker protocol v1 (see `docs/workers.md`), so an engine only has to turn **one page
image into text lines**.

```python
from docveta_worker import Engine, PageResult, Line, Word, run

class MyEngine(Engine):
    name = "my-ocr"
    version = "1.0"
    languages = ["en"]
    tags = ["gpu"]
    concurrency = 2          # parallel pages (e.g. NPU cores)

    def open_session(self, slot):   # optional per-thread state
        return load_model(slot)

    def recognize(self, session, image, page_no, languages):
        lines = []
        for text, box, conf in session.run(image):
            lines.append(Line(text=text, bbox=box, confidence=conf))
        return PageResult(lines=lines)

run(MyEngine())
```

Configuration (environment):

| Variable | Meaning |
|---|---|
| `DOCVETA_URL` | Docveta base URL, e.g. `https://docs.example.com` |
| `DOCVETA_WORKER_TOKEN` | Token from *Administration → Processing → Add worker* |
| `DOCVETA_WORKER_NAME` | Optional display name (default: hostname) |
| `DOCVETA_ARCHIVE` | `false` to not build searchable PDFs on this worker |
| `DOCVETA_TLS_VERIFY` | `false`, or path to a CA bundle |

What the SDK handles for you: capability announcement, long-poll leasing, heartbeats
(and aborting when a lease is lost), downloads, PDF rasterisation (pypdfium2), multi-page
TIFF, EXIF orientation, HEIC (with `pip install docveta-worker[heic]`), the canonical result
format, error reporting with retry/fallback semantics, graceful shutdown, and building
searchable PDFs from word boxes.

Try it without real OCR: `docveta-worker-mock`.
