"""The worker loop's task handling, against a fake Docveta client (no server needed)."""

import unittest

from docveta_worker.engine import EngineError
from docveta_worker.mock import MockEngine
from docveta_worker.runner import Worker

from test_sdk import scanned_pdf


class FakeClient:
    def __init__(self, pdf_path: str):
        self.pdf_path = pdf_path
        self.failed = []
        self.completed = []

    def download(self, url, lease_id, dest):
        with open(self.pdf_path, "rb") as f:
            dest.write(f.read())

    def heartbeat(self, *a, **kw):
        pass

    def complete(self, task, metrics, result=None, archive_path=None):
        self.completed.append(task["task_id"])

    def fail(self, task, code, message, retryable):
        self.failed.append((code, retryable, message))


class NoNPU(MockEngine):
    def open_session(self, slot):
        raise SystemExit("cannot initialise the NPU runtime")


class NoLanguage(MockEngine):
    def recognize(self, session, image, page_no, languages):
        if page_no == 2:
            raise EngineError("language_unavailable", "no Tamil model")
        return super().recognize(session, image, page_no, languages)


def task(**kw):
    t = {"task_id": "t1", "lease_id": "l1", "type": "ocr", "complete_url": "/c",
         "input": {"url": "/in", "mime": "application/pdf"}, "page_range": None, "hints": {"languages": ["ta"]}}
    t.update(kw)
    return t


class HandleTest(unittest.TestCase):
    def setUp(self):
        import tempfile

        self.tmp = tempfile.TemporaryDirectory()
        self.pdf = self.tmp.name + "/scan.pdf"
        scanned_pdf(self.pdf, 3)

    def tearDown(self):
        self.tmp.cleanup()

    def worker(self, engine):
        w = Worker(engine, "http://docveta.invalid", "dvt_wrk_x")
        w.client = FakeClient(self.pdf)
        return w

    def test_engine_error_mid_document_is_reported_with_its_code(self):
        w = self.worker(NoLanguage())
        w._handle(task(), None)
        self.assertEqual([(c, r) for c, r, _ in w.client.failed], [("language_unavailable", False)])
        self.assertEqual(w.client.completed, [])

    def test_null_language_hint(self):
        w = self.worker(MockEngine())
        w._handle(task(hints={"languages": None}), None)
        self.assertEqual(w.client.failed, [])
        self.assertEqual(w.client.completed, ["t1"])

    def test_engine_that_cannot_start_stops_the_worker(self):
        # Before, the failing slot thread ended silently and the worker kept running
        # (and stayed online in Docveta) without reading anything.
        import signal

        w = Worker(NoNPU(), "http://docveta.invalid", "dvt_wrk_x", archive=False)
        w.hello = lambda: None
        old = signal.getsignal(signal.SIGINT)
        try:
            with self.assertRaises(SystemExit):
                w.run()
        finally:
            signal.signal(signal.SIGINT, old)


if __name__ == "__main__":
    unittest.main()
