"""Worker main loop: hello, lease, process, heartbeat, complete/fail, graceful shutdown."""

from __future__ import annotations

import json
import logging
import os
import platform
import signal
import socket
import tempfile
import threading
import time
from typing import Any, Optional

from ._version import __version__
from .client import Client, LeaseLost, ProtocolError
from .engine import Engine, EngineError, PageResult
from .pages import iter_pages

log = logging.getLogger("docveta_worker")


class _Stop:
    def __init__(self) -> None:
        self.event = threading.Event()

    def __call__(self, *_: Any) -> None:
        if not self.event.is_set():
            log.info("shutting down: finishing current tasks (send again to force)")
            self.event.set()
        else:
            os._exit(1)


def page_json(page_no: int, width: int, height: int, dpi: float, r: PageResult) -> dict[str, Any]:
    lines = [l.to_json() for l in r.lines]
    if r.blocks:
        blocks = [{"lines": [lines[i] for i in idx if 0 <= i < len(lines)]} for idx in r.blocks]
    else:
        blocks = [{"lines": lines}] if lines else []
    for b in blocks:
        boxes = [l["bbox"] for l in b["lines"] if "bbox" in l]
        if boxes:
            b["bbox"] = [min(x[0] for x in boxes), min(x[1] for x in boxes), max(x[2] for x in boxes), max(x[3] for x in boxes)]
        b["type"] = "text"
    out: dict[str, Any] = {
        "page": page_no,
        "width": width,
        "height": height,
        "unit": "px",
        "dpi": round(dpi, 2),
        "rotation": r.rotation if r.rotation in (0, 90, 180, 270) else 0,
        "text": r.text(),
        "blocks": blocks,
    }
    conf = r.mean_confidence()
    if conf is not None:
        out["confidence"] = round(float(conf), 4)
    if r.language:
        out["language"] = r.language
    return out


class Worker:
    def __init__(self, engine: Engine, base_url: str, token: str, name: Optional[str] = None, archive: bool = True, verify: bool | str = True):
        self.engine = engine
        self.client = Client(base_url, token, verify=verify)
        self.name = name or socket.gethostname()
        self.archive = archive
        self.stop = _Stop()
        self.heartbeat_interval = 20
        self.lease_wait = 30

    def capabilities(self) -> list[dict[str, Any]]:
        e = self.engine
        caps = [{
            "task_type": "ocr",
            "engine": e.name,
            "engine_version": e.version,
            "languages": list(e.languages),
            "input_mime": list(e.input_mime),
            "outputs": ["lines", "words", "bbox", "confidence"],
            "max_pages_per_task": e.max_pages_per_task,
            "concurrency": max(1, int(e.concurrency)),
            "tags": list(e.tags),
        }]
        if self.archive:
            caps.append({"task_type": "archive", "engine": "docveta-textlayer", "engine_version": __version__,
                         "input_mime": ["application/pdf", "image/*"], "outputs": ["pdf"], "concurrency": 1})
        return caps

    def hello(self) -> None:
        delay = 2.0
        while not self.stop.event.is_set():
            try:
                res = self.client.hello({"name": self.name, "version": __version__, "host": platform.node()}, self.capabilities())
                self.heartbeat_interval = int(res.get("heartbeat_interval_seconds", 20))
                self.lease_wait = int(res.get("max_lease_wait_seconds", 30))
                log.info("connected to Docveta %s as %s (engine %s, protocol %s)", res.get("server_version"), self.name, self.engine.describe(), res.get("protocol"))
                return
            except ProtocolError:
                raise
            except Exception as e:  # network errors: retry with backoff
                log.warning("cannot reach Docveta (%s); retrying in %.0fs", e, delay)
                self.stop.event.wait(delay)
                delay = min(delay * 2, 60)

    # ------------------------------------------------------------------ loop

    def run(self) -> None:
        signal.signal(signal.SIGINT, self.stop)
        try:
            signal.signal(signal.SIGTERM, self.stop)
        except (AttributeError, ValueError):  # pragma: no cover - Windows
            pass
        self.hello()
        threads = []
        n = max(1, int(self.engine.concurrency))
        for slot in range(n):
            t = threading.Thread(target=self._slot_loop, args=(slot,), name=f"slot-{slot}", daemon=True)
            t.start()
            threads.append(t)
        if self.archive:
            t = threading.Thread(target=self._slot_loop, args=(-1,), name="archive", daemon=True)
            t.start()
            threads.append(t)
        while any(t.is_alive() for t in threads):
            for t in threads:
                t.join(timeout=1)

    def _slot_loop(self, slot: int) -> None:
        session = None
        if slot >= 0:
            session = self.engine.open_session(slot)
        backoff = 2.0
        try:
            while not self.stop.event.is_set():
                try:
                    tasks = self.client.lease(capacity=1, wait=self.lease_wait, types=["archive"] if slot < 0 else ["ocr"])
                    backoff = 2.0
                except ProtocolError as e:
                    log.error("%s", e)
                    if "hello" in str(e).lower():
                        self.hello()
                    self.stop.event.wait(30)
                    continue
                except Exception as e:
                    log.warning("lease failed (%s); retrying in %.0fs", e, backoff)
                    self.stop.event.wait(backoff)
                    backoff = min(backoff * 2, 60)
                    continue
                for task in tasks:
                    self._handle(task, session)
        finally:
            if session is not None:
                self.engine.close_session(session)

    # ------------------------------------------------------------------ tasks

    def _handle(self, task: dict[str, Any], session: Any) -> None:
        lost = threading.Event()
        progress: dict[str, Any] = {}
        done = threading.Event()

        def beat() -> None:
            while not done.wait(self.heartbeat_interval):
                try:
                    self.client.heartbeat(task["task_id"], task["lease_id"], progress)
                except LeaseLost:
                    log.warning("task %s: lease lost, aborting", task["task_id"])
                    lost.set()
                    return
                except Exception as e:
                    log.warning("heartbeat failed: %s", e)

        hb = threading.Thread(target=beat, daemon=True)
        hb.start()
        started = time.monotonic()
        try:
            with tempfile.TemporaryDirectory(prefix="docveta-") as tmp:
                src = os.path.join(tmp, "input")
                with open(src, "wb") as f:
                    self.client.download(task["input"]["url"], task["lease_id"], f)
                if task["type"] == "ocr":
                    result = self._ocr(task, session, src, progress, lost)
                    if lost.is_set():
                        return
                    metrics = {"ms_total": int((time.monotonic() - started) * 1000), "pages": len(result["pages"]), "engine": self.engine.name}
                    if result["pages"]:
                        metrics["ms_per_page"] = metrics["ms_total"] // len(result["pages"])
                    self.client.complete(task, metrics, result=json.dumps(result, ensure_ascii=False).encode("utf-8"))
                    log.info("task %s: OCR done (%d pages, %.1fs)", task["task_id"], len(result["pages"]), time.monotonic() - started)
                elif task["type"] == "archive":
                    from .textlayer import build_searchable_pdf

                    ocr = self.client.get_json(task["ocr_result_url"], task["lease_id"])
                    out = os.path.join(tmp, "archive.pdf")
                    build_searchable_pdf(src, task["input"]["mime"], ocr, out)
                    if lost.is_set():
                        return
                    self.client.complete(task, {"ms_total": int((time.monotonic() - started) * 1000)}, archive_path=out)
                    log.info("task %s: searchable PDF done", task["task_id"])
                else:
                    raise EngineError("unsupported_input", f"unknown task type {task['type']}")
        except LeaseLost:
            log.warning("task %s: lease lost", task["task_id"])
        except EngineError as e:
            log.warning("task %s failed: %s: %s", task["task_id"], e.code, e)
            self._fail(task, e.code, str(e), e.retryable)
        except MemoryError:
            self._fail(task, "resource_exhausted", "out of memory", True)
        except Exception as e:  # unexpected: retryable so a transient problem doesn't lose work
            log.exception("task %s crashed", task["task_id"])
            self._fail(task, "engine_error", f"{type(e).__name__}: {e}", True)
        finally:
            done.set()

    def _fail(self, task: dict[str, Any], code: str, msg: str, retryable: bool) -> None:
        try:
            self.client.fail(task, code, msg, retryable)
        except Exception as e:
            log.error("could not report failure: %s", e)

    def _ocr(self, task: dict[str, Any], session: Any, path: str, progress: dict[str, Any], lost: threading.Event) -> dict[str, Any]:
        mime = task["input"]["mime"]
        rng = task.get("page_range") or [None, None]
        hints = task.get("hints") or {}
        languages = [l for l in hints.get("languages", []) if l] or list(self.engine.languages[:1])
        dpi = int(hints.get("dpi") or self.engine.dpi)
        pages = []
        for p in iter_pages(path, mime, rng[0], rng[1], dpi):
            if lost.is_set():
                break
            r = self.engine.recognize(session, p.image, p.page_no, languages)
            pages.append(page_json(p.page_no, p.image.width, p.image.height, p.dpi, r))
            progress["pages_done"] = len(pages)
            p.image.close()
        models = dict(self.engine.models)
        return {"schema": "ocr-result/v1", "engine": {"name": self.engine.name, "version": self.engine.version, "models": models}, "pages": pages}


def run(engine: Engine, archive: Optional[bool] = None) -> None:
    """Start a worker using environment configuration:

    DOCVETA_URL            base URL of the Docveta server (required)
    DOCVETA_WORKER_TOKEN   worker token from Admin → Processing (required)
    DOCVETA_WORKER_NAME    display name (default: hostname)
    DOCVETA_ARCHIVE        "false" to not build searchable PDFs on this worker
    DOCVETA_TLS_VERIFY     "false" to skip TLS verification, or a CA bundle path
    DOCVETA_LOG_LEVEL      DEBUG, INFO (default), WARNING
    """
    logging.basicConfig(level=os.environ.get("DOCVETA_LOG_LEVEL", "INFO").upper(), format="%(asctime)s %(levelname)s %(threadName)s %(message)s")
    url = os.environ.get("DOCVETA_URL")
    token = os.environ.get("DOCVETA_WORKER_TOKEN")
    if not url or not token:
        raise SystemExit("DOCVETA_URL and DOCVETA_WORKER_TOKEN must be set")
    if archive is None:
        archive = os.environ.get("DOCVETA_ARCHIVE", "true").lower() not in ("0", "false", "no")
    verify: bool | str = True
    v = os.environ.get("DOCVETA_TLS_VERIFY")
    if v:
        verify = False if v.lower() in ("0", "false", "no") else v
    Worker(engine, url, token, os.environ.get("DOCVETA_WORKER_NAME"), archive=archive, verify=verify).run()
