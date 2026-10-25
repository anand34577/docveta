"""Protocol client: uploading a result survives a dropped connection or a restarting server."""

import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from docveta_worker.client import Client, LeaseLost, ProtocolError


class Server(BaseHTTPRequestHandler):
    answers: list = []
    bodies: list = []

    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        Server.bodies.append(body)
        status = Server.answers.pop(0)
        if status == "drop":  # connection closed without an answer
            self.close_connection = True
            return
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        payload = b'{"code":"lease_lost"}' if status == 409 else b"{}"
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *a):
        pass


TASK = {"task_id": "t1", "lease_id": "l1", "complete_url": "/worker/v1/tasks/t1/complete"}


class CompleteTest(unittest.TestCase):
    def setUp(self):
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), Server)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.client = Client(f"http://127.0.0.1:{self.srv.server_port}", "dvt_wrk_x")
        Server.bodies = []

    def tearDown(self):
        self.srv.shutdown()
        self.srv.server_close()

    def complete(self):
        self.client.complete(TASK, {"pages": 1}, result=b'{"schema":"ocr-result/v1","pages":[]}', retry_delay=0.01)

    def test_retries_after_dropped_connection(self):
        Server.answers = ["drop", 204]
        self.complete()
        self.assertEqual(len(Server.bodies), 2)
        self.assertIn(b"ocr-result/v1", Server.bodies[1])  # the same upload again

    def test_retries_while_server_restarts(self):
        Server.answers = [503, 502, 204]
        self.complete()
        self.assertEqual(len(Server.bodies), 3)

    def test_gives_up_after_the_attempts(self):
        Server.answers = [503, 503, 503]
        with self.assertRaises(ProtocolError):
            self.complete()

    def test_earlier_attempt_arrived(self):
        Server.answers = ["drop", 409]
        with self.assertRaises(LeaseLost):
            self.complete()

    def test_archive_file_is_sent_again(self):
        import os
        import tempfile

        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "archive.pdf")
            with open(path, "wb") as f:
                f.write(b"%PDF-1.7 test archive")
            Server.answers = ["drop", 204]
            self.client.complete(TASK, {}, archive_path=path, retry_delay=0.01)
        self.assertIn(b"test archive", Server.bodies[1])


if __name__ == "__main__":
    unittest.main()
