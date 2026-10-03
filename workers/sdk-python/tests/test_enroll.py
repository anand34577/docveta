"""Enrollment: the worker waits for the key file and the server, then gets its token."""

import json
import os
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

from docveta_worker.client import ProtocolError, enroll


class Server(BaseHTTPRequestHandler):
    calls = 0

    def do_POST(self):
        Server.calls += 1
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if Server.calls == 1:  # still starting
            self.send_response(503)
            self.end_headers()
            return
        ok = body == {"name": "ocr-1", "key": "secret-key"}
        self.send_response(200 if ok else 401)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"token": "dvt_wrk_x"} if ok else {"title": "no"}).encode())

    def log_message(self, *a):
        pass


class EnrollTest(unittest.TestCase):
    def setUp(self):
        self.srv = HTTPServer(("127.0.0.1", 0), Server)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.srv.server_port}"
        self.dir = tempfile.mkdtemp()
        Server.calls = 0

    def tearDown(self):
        self.srv.shutdown()

    def test_waits_for_key_file_and_server(self):
        key = os.path.join(self.dir, "enroll.key")
        threading.Timer(0.3, lambda: open(key, "w").write("secret-key\n")).start()
        t = time.time()
        self.assertEqual(enroll(self.url, "ocr-1", key, wait=0.2), "dvt_wrk_x")
        self.assertGreaterEqual(Server.calls, 2)  # retried past the 503
        self.assertLess(time.time() - t, 10)

    def test_wrong_key_stops(self):
        key = os.path.join(self.dir, "enroll.key")
        open(key, "w").write("other")
        Server.calls = 1  # skip the "starting" answer
        with self.assertRaises(ProtocolError):
            enroll(self.url, "ocr-1", key, wait=0.2)


if __name__ == "__main__":
    unittest.main()
