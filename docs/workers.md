# Docveta worker protocol v1

Processing workers (OCR engines and the searchable-PDF builder) run outside the Docveta
server, on any machine, in any language. They **pull** work over HTTPS, so they can sit
behind NAT and scale by simply starting more of them. The Python SDK
(`workers/sdk-python`) implements everything below; read this if you write a worker in
another language. Design rationale: [DESIGN.md §11](DESIGN.md#11-processing-engines-ocr--pluggable-worker-architecture).

## Authentication

Create a worker in *Administration → Processing → Add worker*. Every request carries
`Authorization: Bearer dvt_wrk_…`. Disabled workers get `403`.

## Lifecycle

```
POST /worker/v1/hello                 announce capabilities (on start and when they change)
loop:
  POST /worker/v1/lease               long-poll for tasks (≤30 s); 204 = nothing to do
  GET  <input.url>?lease_id=…         download the original file
  POST /worker/v1/tasks/{id}/heartbeat   every heartbeat_interval_seconds while working
  POST <complete_url>                 multipart: lease_id, metrics, result | archive
  or POST /worker/v1/tasks/{id}/fail  {lease_id, code, message, retryable}
  or POST /worker/v1/tasks/{id}/release  give a task back (e.g. shutting down)
```

A **lease** gives one worker exclusive ownership of a task for `lease_ttl_seconds`;
heartbeats extend it. If a worker disappears, the lease expires and the task is retried
elsewhere. Any call made with an expired or replaced lease returns `409` with
`code: "lease_lost"` — stop working on that task.

## hello

```json
POST /worker/v1/hello
{
  "protocol": [1],
  "worker": {"name": "rk3588-npu-1", "version": "0.1.0", "host": "rock5b"},
  "capabilities": [
    {"task_type": "ocr", "engine": "ppocr-rknn", "engine_version": "rk3588/ppocr-v4",
     "languages": ["en", "hi"], "input_mime": ["application/pdf", "image/*"],
     "max_pages_per_task": 30, "concurrency": 3, "tags": ["npu", "rk3588"]},
    {"task_type": "archive", "engine": "docveta-textlayer", "concurrency": 1}
  ]
}
→ 200 {"worker_id": "…", "protocol": 1, "heartbeat_interval_seconds": 22,
       "lease_ttl_seconds": 90, "max_lease_wait_seconds": 30, "server_version": "…"}
→ 426 if no common protocol version
```

* `languages` are ISO 639-1 codes. Tasks are routed only to workers that support the
  document's language (until the fallback time passes).
* `tags` are matched against *Prefer workers tagged* in the processing settings.
* `concurrency` caps how many tasks of that type the worker holds at once.

## lease

```json
POST /worker/v1/lease
{"capacity": 1, "wait_seconds": 30, "types": ["ocr"]}
→ 204 (no work)
→ 200 {"tasks": [{
   "task_id": "…", "lease_id": "…", "type": "ocr", "engine": "ppocr-rknn", "attempt": 1,
   "lease_expires_at": "2026-10-02T10:01:30Z", "document_id": "…",
   "page_range": [11, 20],                       // 1-based inclusive; null = whole file
   "hints": {"languages": ["hi", "en"], "dpi": 300, "deskew": true, "detect_rotation": true},
   "input": {"url": "/worker/v1/tasks/…/input", "mime": "application/pdf", "sha256": "…", "size": 1830021},
   "ocr_result_url": "/worker/v1/tasks/…/ocr-result",   // archive tasks only
   "complete_url": "/worker/v1/tasks/…/complete",
   "max_result_bytes": 536870912
}]}
```

## complete

`multipart/form-data`, parts **in this order**: `lease_id`, `metrics` (JSON, optional),
then `result` (OCR tasks, `application/json`) or `archive` (archive tasks, PDF).
`204` on success.

### Canonical OCR result (`ocr-result/v1`)

```json
{
  "schema": "ocr-result/v1",
  "engine": {"name": "ppocr-rknn", "version": "rk3588/ppocr-v4", "models": {"det": "det.rknn"}},
  "pages": [{
    "page": 11, "width": 2480, "height": 3508, "unit": "px", "dpi": 300,
    "rotation": 0, "language": "hi", "confidence": 0.94,
    "text": "full page text in reading order",
    "blocks": [{"bbox": [120,210,2300,380], "type": "text", "lines": [
      {"bbox": [120,210,2300,260], "text": "BESCOM Electricity Bill", "confidence": 0.98,
       "words": [{"bbox": [120,210,520,260], "text": "BESCOM", "confidence": 0.99}]}
    ]}]
  }]
}
```

Rules (validated by the server; violations count as a non-retryable `engine_error`):

* `page` numbers are 1-based and must lie inside the task's `page_range`; no duplicates.
* `rotation` ∈ {0, 90, 180, 270}.
* Coordinates are pixels of the page image **as rendered at `dpi`**, origin top-left —
  even if the engine rotated the image internally, boxes are mapped back.
* `words`, `bbox` and `confidence` are optional. Without word boxes, text is still
  searchable in Docveta, but no searchable-PDF (archive) task is created.
* `text` may be omitted; it is then derived from the lines.

## fail

```json
POST /worker/v1/tasks/{id}/fail
{"lease_id": "…", "code": "language_unavailable", "message": "no Tamil model", "retryable": false}
```

Codes: `unsupported_input`, `corrupt_input`, `language_unavailable`, `engine_error`,
`resource_exhausted`, `timeout`.

* `retryable: true` → retried with exponential backoff, up to *Attempts per task*.
* `retryable: false` (or attempts exhausted) → if another engine is available, this
  engine is excluded and the task is offered to the others immediately; otherwise the
  task fails and the document shows "couldn't read text" with a Retry button for admins.

## Routing summary

A queued task is offered to a worker when all of these hold:

1. the worker advertises the task type and the file's MIME type;
2. its engine is not in the task's excluded engines;
3. it has all *preferred tags* and the document's language — **or** the task's fallback
   time has passed (*Fall back to any worker after N minutes*; immediately if no worker
   with the preferred tags has been seen in a day);
4. the page batch fits `max_pages_per_task`.

Ordering: priority (interactive uploads before email/folder imports before bulk
imports/reprocessing), then age.
