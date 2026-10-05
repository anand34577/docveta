"""HTTP client for the Docveta worker protocol v1."""

from __future__ import annotations

import json
import logging
import time
from typing import Any, BinaryIO, Optional

import requests

log = logging.getLogger("docveta_worker")

PROTOCOL = 1


class LeaseLost(Exception):
    """The server no longer considers this worker the holder of the task's lease."""


class ProtocolError(Exception):
    pass


def enroll(base_url: str, name: str, key_file: str, verify: bool | str = True, wait: float = 5.0) -> str:
    """Gets a worker token with the enrollment key (Docker setups, POST /worker/v1/enroll).

    Waits for the key file and for the server, so containers can start in any order."""
    url = base_url.rstrip("/") + "/worker/v1/enroll"
    waited = 0.0
    while True:
        try:
            with open(key_file, encoding="utf-8") as f:
                key = f.read().strip()
            r = requests.post(url, json={"name": name, "key": key}, timeout=30, verify=verify)
            if r.status_code == 200:
                return r.json()["token"]
            if r.status_code in (400, 401, 404):
                raise ProtocolError(f"Docveta refused enrollment (HTTP {r.status_code}): {r.text[:200]}")
            reason = f"HTTP {r.status_code}"
        except (OSError, requests.RequestException) as e:  # key file not there yet, or server starting
            reason = str(e)
        if waited % 60 < wait:
            log.info("waiting to enroll with Docveta (%s)", reason)
        time.sleep(wait)
        waited += wait


class Client:
    def __init__(self, base_url: str, token: str, timeout: float = 60.0, verify: bool | str = True):
        self.base = base_url.rstrip("/")
        self.s = requests.Session()
        self.s.headers.update({"Authorization": f"Bearer {token}", "User-Agent": "docveta-worker-sdk/0.1"})
        self.s.verify = verify
        self.timeout = timeout

    def _url(self, path: str) -> str:
        return path if path.startswith("http") else self.base + path

    def _check(self, r: requests.Response) -> None:
        if r.status_code == 409:
            try:
                code = r.json().get("code")
            except ValueError:
                code = None
            if code == "lease_lost":
                raise LeaseLost(r.text)
        if r.status_code == 426:
            raise ProtocolError("Server requires a newer worker protocol; upgrade this worker")
        if r.status_code == 401:
            raise ProtocolError("Worker token rejected (401). Check DOCVETA_WORKER_TOKEN.")
        if r.status_code == 403:
            raise ProtocolError("Worker is disabled in Docveta (403)")
        if r.status_code >= 400:
            raise ProtocolError(f"HTTP {r.status_code}: {r.text[:300]}")

    def hello(self, worker: dict[str, Any], capabilities: list[dict[str, Any]]) -> dict[str, Any]:
        r = self.s.post(self._url("/worker/v1/hello"), json={"protocol": [PROTOCOL], "worker": worker, "capabilities": capabilities}, timeout=self.timeout)
        self._check(r)
        return r.json()

    def lease(self, capacity: int, wait: int, types: Optional[list[str]] = None) -> list[dict[str, Any]]:
        body: dict[str, Any] = {"capacity": capacity, "wait_seconds": wait}
        if types:
            body["types"] = types
        r = self.s.post(self._url("/worker/v1/lease"), json=body, timeout=wait + 30)
        if r.status_code == 204:
            return []
        self._check(r)
        return r.json().get("tasks", [])

    def heartbeat(self, task_id: str, lease_id: str, progress: Optional[dict[str, Any]] = None) -> None:
        r = self.s.post(self._url(f"/worker/v1/tasks/{task_id}/heartbeat"), json={"lease_id": lease_id, "progress": progress or {}}, timeout=30)
        self._check(r)

    def download(self, url: str, lease_id: str, dest: BinaryIO) -> None:
        with self.s.get(self._url(url), params={"lease_id": lease_id}, stream=True, timeout=self.timeout) as r:
            self._check(r)
            for chunk in r.iter_content(chunk_size=1 << 20):
                dest.write(chunk)

    def get_json(self, url: str, lease_id: str) -> Any:
        r = self.s.get(self._url(url), params={"lease_id": lease_id}, timeout=self.timeout)
        self._check(r)
        return r.json()

    def complete(self, task: dict[str, Any], metrics: dict[str, Any], result: Optional[bytes] = None, archive_path: Optional[str] = None) -> None:
        # Field order matters: the server streams parts and expects lease_id/metrics first.
        fields: list[tuple[str, Any]] = [
            ("lease_id", (None, task["lease_id"])),
            ("metrics", (None, json.dumps(metrics), "application/json")),
        ]
        fh = None
        try:
            if result is not None:
                fields.append(("result", ("result.json", result, "application/json")))
            if archive_path is not None:
                fh = open(archive_path, "rb")
                fields.append(("archive", ("archive.pdf", fh, "application/pdf")))
            r = self.s.post(self._url(task["complete_url"]), files=fields, timeout=max(self.timeout, 300))
            self._check(r)
        finally:
            if fh:
                fh.close()

    def fail(self, task: dict[str, Any], code: str, message: str, retryable: bool) -> None:
        r = self.s.post(
            self._url(f"/worker/v1/tasks/{task['task_id']}/fail"),
            json={"lease_id": task["lease_id"], "code": code, "message": message[:450], "retryable": retryable},
            timeout=30,
        )
        if r.status_code not in (204, 409):
            self._check(r)

    def release(self, task: dict[str, Any]) -> None:
        try:
            self.s.post(self._url(f"/worker/v1/tasks/{task['task_id']}/release"), json={"lease_id": task["lease_id"]}, timeout=10)
        except requests.RequestException:
            pass
