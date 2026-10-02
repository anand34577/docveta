# Docveta — Remaining Work

| | |
|---|---|
| **Updated** | 2026-10-02 |
| **Repository** | github.com/anand34577/docveta |
| **License** | AGPL-3.0 (server, web, reference workers) · Apache-2.0 (worker SDK) |
| **Companion docs** | [DESIGN.md](DESIGN.md) (architecture & decisions) · [workers.md](workers.md) · [operations.md](operations.md) |

This document lists everything that is **not done yet**, in the order it should be done.
Each item has: *why*, *scope* (what to build, where), *acceptance criteria* (how we know
it's done) and *dependencies*. Status of what *is* built is in DESIGN.md → "Implementation
status".

Priority legend: **P0** must happen before anyone uses Docveta for real documents ·
**P1** completes milestone M1/M2 · **P2** M3 · **P3** M4 / later.

---

## Contents

0. [Verification debt (do first)](#0-verification-debt-do-first)
1. [Finish M1 — "Capture, find, view"](#1-finish-m1--capture-find-view)
2. [M2 — "Organise automatically"](#2-m2--organise-automatically)
3. [M3 — "Understand"](#3-m3--understand)
4. [M4 — "Ask" and mobile](#4-m4--ask-and-mobile)
5. [Cross-cutting work](#5-cross-cutting-work)
6. [Known limitations & technical debt](#6-known-limitations--technical-debt)
7. [Release checklist for v0.1.0](#7-release-checklist-for-v010)
8. [Suggested order of work](#8-suggested-order-of-work)

---

## 0. Verification debt (do first)

Large parts of the backend were written and reviewed but **never executed against
PostgreSQL**, and the OCR workers were never run on their target hardware. Nothing
below should be trusted until this section is green.

### 0.1 Run the end-to-end integration test — P0 ✅ done (2026-10-02)
> Passes 10/10 with `-race` against PostgreSQL 17; it now also runs in CI (§5.1).

- **Why:** login, upload, preprocessing, search, the whole worker protocol (lease →
  heartbeat → complete → finalize → archive), permissions and CSRF are only covered by
  `internal/app/integration_test.go`, which hasn't run.
- **Scope:** provide a PostgreSQL 16+ database (local PG 17 or a throwaway cluster), run
  `DOCVETA_TEST_DATABASE_URL=postgres://… go test ./internal/app -run Integration -v`,
  fix every failure. The test uses a throwaway schema and drops it afterwards.
- **Acceptance:** the test passes 10 times in a row (`-count=10`) with `-race`.
- **Likely problem areas** (found by review, worth checking first): pgx encoding of
  `google/uuid` arrays, `jsonb` ↔ `json.RawMessage`, `make_interval(secs => int)`,
  the goose migration's trigger functions, River migrations in a non-public schema.

### 0.2 Extend integration coverage — P0 (partly done)
> Done: API-token scopes, new-sign-in dedupe, SSRF guard, saved views, bulk update, JSON
> search, every read endpoint, and lease expiry → reaper → re-queue → attempts exhausted
> → document failed → admin Retry → ready (found and fixed a stuck-document bug), with
> **all responses validated against `openapi.yaml`**. Still open: OIDC with a mock IdP,
> engine exclusion/fallback routing, moving between spaces, notification delivery,
> session expiry, last-admin/owner protections, trash purge + blob GC, search edge cases.

Add test cases for the paths the current test doesn't touch:
- OIDC login against a mock IdP (e.g. `github.com/oauth2-proxy/mockoidc`): new-user
  provisioning, verified-email linking on/off, allowed/admin groups, nonce/state errors.
- Lease expiry → reaper → re-queue → attempts exhausted → task failed → document
  `failed`; admin *Retry* re-opens the run.
- Non-retryable failure with a second engine available → engine excluded, fallback.
- Fallback routing: task requiring `npu` tag goes to a CPU worker after the fallback time.
- Moving a document between spaces maps tags/correspondent/type by name.
- Notification delivery to a fake Gotify/webhook HTTP server; idempotency on job retry.
- Session expiry/revocation, API-token scopes (read-only token can't upload).
- Last-admin and last-owner protections; user deletion with non-empty personal space.
- Trash purge job and blob GC (with an artificially short grace period).
- Search: Hindi query, identifier query (`ABCDE-1234-F`), negation, phrases, sort orders,
  keyset pagination across pages without duplicates or gaps.
- **Acceptance:** every API endpoint is hit at least once by a test.

### 0.3 Manual UI test pass — P0
- **Scope:** run the full stack (`go run ./cmd/docveta serve` + `npm run dev` + mock
  worker) and walk through every screen on desktop (Chrome, Firefox) and phone (Android
  Chrome, iOS Safari). Checklist: setup wizard, login/logout, upload (drag & drop,
  picker, paste), duplicate dialog, inbox triage with keyboard, document viewer
  (PDF, image, text), autosave + conflict toast, filters/sort/saved views, bulk
  actions, trash, notes + mentions, all settings and admin pages, dark mode, PWA install
  and Android share target.
- **Acceptance:** issues filed and fixed; no console errors; no layout breakage at 360 px.

### 0.4 Tesseract worker on a real machine — P0
- **Scope:** build `workers/tesseract/Dockerfile`, connect to Docveta, process English,
  Hindi and mixed documents, rotated scans (OSD), multi-page TIFF, HEIC.
- **Acceptance:** text is correct and searchable; searchable PDF opens in a normal PDF
  reader with selectable text aligned to the image; Devanagari text layer uses the Noto
  font.

### 0.5 Rockchip NPU worker on real boards — P0
- **Scope:**
  1. Run `workers/rknn/convert/convert.py` on x86 for `rk3588`, `rk3576`, `rk3566`
     with a calibration set of ≥ 50 real scans. Verify the Paddle model URLs still
     resolve (they are pinned to PaddleOCR 2.7-era paths) and that RKNN-Toolkit2
     accepts the dynamic recognition widths (`dynamic_input`).
  2. On each board: build the image, confirm `librknnrt.so` matches the kernel driver
     (`/sys/kernel/debug/rknpu/version`), run the worker, process a test corpus.
  3. Add `docveta-worker-rknn bench`: pages/minute and character error rate (CER) against
     a ground-truth fixture set; compare with Tesseract.
- **Acceptance:** all three SoCs process the corpus; RK3588 uses 3 contexts in parallel
  (check `active_tasks` in Admin → Processing); CER within a few points of PaddleOCR on
  CPU; numbers published in `workers/rknn/README.md`.
- **Possible follow-ups:** direction classifier (cls) model for upside-down text lines;
  ONNX-Runtime backend of the same pipeline for x86/GPU machines (shares `ppocr.py`).

---

## 1. Finish M1 — "Capture, find, view"

### 1.1 OpenAPI specification + spec tests — P1 (ADR-023) (mostly done)
> Done: `internal/api/openapi.yaml` (embedded so it can be served; `api/` would be outside
> the Go package) covering all 95 operations, served at `/api/v1/openapi.yaml`;
> `TestOpenAPIRoutes` (route ↔ spec parity + spec validity); the integration test
> validates every response with `openapi3filter`. Still open: the `/api/docs` page and
> generating `web/src/lib/api-types.ts` with `openapi-typescript`.

- **Why:** contract for the Android app and third parties; TypeScript types are
  currently hand-written in `web/src/lib/types.ts` and can drift.
- **Scope:**
  - `api/openapi.yaml` (OpenAPI 3.0.3) covering every route in `internal/api`, with
    schemas, RFC 9457 problem responses, security schemes (cookie, bearer token,
    worker token), examples.
  - Serve it at `/api/v1/openapi.yaml` and a docs page at `/api/docs` (static Redoc or
    Scalar bundle, CSP-compatible).
  - Go test: (a) every registered mux pattern exists in the spec and vice versa;
    (b) the integration test validates responses with `kin-openapi/openapi3filter`.
  - Generate `web/src/lib/api-types.ts` with `openapi-typescript`; replace hand-written
    types.
- **Acceptance:** CI fails when a route or response shape changes without the spec.

### 1.2 Resumable uploads (tus) — P1
- **Why:** big scans and phone uploads on flaky networks (DESIGN §16, ADR-017).
- **Scope:** mount `tusd` handler at `/api/v1/uploads/` with a filestore in
  `data/tmp/tus`, auth via the existing middleware, metadata (space, filename, tags);
  on completion call `documents.Ingest` with the finished file. Size limit,
  expiry of incomplete uploads (24 h, already cleaned by maintenance). Web: use tus
  for files > 5 MB (`tus-js-client`), keep multipart for small ones.
- **Acceptance:** a 300 MB upload survives a network drop and resumes.

### 1.3 Password-protected PDFs — P1 (FR-D15)
- **Scope:** `POST /api/v1/documents/{id}/unlock {password, store_unlocked}`; pass the
  password to pdfium (`Inspect` already accepts it); if `store_unlocked`, write a
  decrypted copy (needs a PDF writer: run it on a worker as a new `decrypt` task type,
  or pdfium `FPDF_SaveAsCopy` via go-pdfium) as a new version; never store the password.
  UI: "Unlock" field on documents with status `needs_password`.
- **Acceptance:** an encrypted bank statement becomes searchable after unlocking; wrong
  password shows a clear error.

### 1.4 Error/empty-state polish & accessibility pass — P1
- Keyboard focus order, focus return after dialogs, screen-reader labels on icon
  buttons, live-region announcements for upload/processing, axe-core clean.
- ~~Login page: redirect to `/` when already signed in.~~ ✅
- **Acceptance:** axe reports no serious issues on every page (see §5.3).

### 1.5 Admin user invitations — P1 (FR-A5)
- **Scope:** `invites` table (in DESIGN §9.4), `POST /api/v1/admin/invites` → link
  `/invite/{token}` (expires 7 days), optional email via SMTP; invite can pre-assign
  space + role; accept page sets name/password or uses SSO.
- **Acceptance:** admins no longer need to type passwords for other people.

---

## 2. M2 — "Organise automatically"

### 2.1 AI classification (OpenAI-compatible) — P1 (DESIGN §13)
- **Scope:**
  - Migration: `ai_providers`, `suggestions` tables (DESIGN §9.4).
  - `internal/ai`: provider client for `/v1/chat/completions`, `/v1/models`,
    `/v1/embeddings`; structured outputs (`json_schema`) with fallback to
    `json_object` and prompt-only + one repair retry; per-provider concurrency, rate
    limit, timeout, circuit breaker; secrets encrypted.
  - Prompt builder: space vocabulary (top-K pre-selection when > 150 tags), first ~3k
    tokens + first/last page, locale/date order, few-shot from recent user corrections.
  - Response validation: IDs must exist in the space; `new_name` only if allowed;
    plausible dates; custom-field types.
  - Classify stage: rules → AI (only if the space's `ai_policy` permits the provider:
    `off` / `local_only` / `any`) → suggestions or auto-apply above a confidence
    threshold; never overwrite `field_sources = user`.
  - API: providers CRUD + "test connection"; `GET/POST /documents/{id}/suggestions`
    accept/reject (per field and "accept all").
  - UI: Admin → AI providers; Space settings → AI policy; Inbox shows ✨ suggestions
    with one-click accept; accuracy stats per space.
- **Acceptance:** with Ollama + a small instruct model, ≥ 80 % of suggestions on a
  test set are accepted unchanged; AI outage never blocks documents becoming `ready`.

### 2.2 Custom fields UI + extraction — P1 (FR-D6)
- Schema exists (`custom_fields`, `custom_field_values`). Build: CRUD API, document
  API read/write (typed validation, monetary with currency), filters in search
  (`amount:>1500`, field-specific date ranges), sorting, metadata panel editors, AI
  and rule-based extraction (amount, due date, policy/invoice numbers).
- **Acceptance:** "Amount" and "Due date" fields can be filtered and sorted.

### 2.3 Reminders + ICS feed — P1 (FR-N4)
- Migration `reminders`; auto-create from "date with reminder" custom fields (expiry,
  due, renewal); manual reminders; lead times; RRULE subset (yearly/monthly); snooze /
  done; scheduler job every 15 min with idempotency key per (reminder, lead time);
  `reminder.due` notifications; Home "Upcoming" card; per-user secret ICS feed URL
  (`/ical/{token}.ics`).
- **Acceptance:** a passport expiry triggers notifications at 180/90/30 days exactly
  once each, even across restarts.

### 2.4 Email import (IMAP) — P1 (FR-I1)
- Migration `mail_accounts`, `mail_rules`; poller (IDLE when supported) per account;
  rules on from/to/subject/body/attachment type/size; actions after import (mark read,
  move, flag, delete); Message-ID de-duplication; optionally import the mail body as a
  PDF (via Gotenberg, 2.6). Errors → `mail.import_failed` notification.
- **Acceptance:** "scan to email" from a scanner lands in the right space with tags.

### 2.5 Watched folder — P1 (FR-I2)
- Polling (source of truth) + fsnotify hints; stable-file detection (size/mtime
  unchanged N seconds); sub-folder → space/tag mapping; move to `done/` / `failed/`
  with `.error.txt`; config in Admin UI; path allow-list.
- **Acceptance:** scanner SMB drop folder works, including files copied slowly.

### 2.6 Office documents via Gotenberg — P1 (FR-D2)
- Optional `gotenberg` Compose profile; `DOCVETA_GOTENBERG_URL`; convert DOCX/XLSX/PPTX/
  ODF/EML to PDF in preprocess as a `derived` file; original kept for download;
  `documents.OfficeEnabled` returns true when configured.
- **Acceptance:** a .docx uploads, previews as PDF and is searchable.

### 2.7 Share links — P1 (FR-A4)
- Migration `shares`; create for a document or saved view; expiry, optional password
  (Argon2id), view-only vs download, revoke, access count; public page `/s/{token}`
  with rate limiting, `noindex`, no app shell; audit entries.
- **Acceptance:** an accountant can open a FY2025-26 tax view without an account.

### 2.8 2FA: TOTP + passkeys — P1 (FR-A1)
- `totp_secrets`, `recovery_codes`, `webauthn_credentials` (go-webauthn); enrolment in
  Settings → Security; login step-up; admin can reset a user's 2FA; re-auth for
  sensitive actions.
- **Acceptance:** password login requires the second factor when enabled.

### 2.9 Import & export — P1 (FR-I3, FR-I4, §19.3)
- `docveta export --dir`: `manifest.json` (versioned schema: users, spaces, taxonomy,
  documents, notes, custom fields, history) + originals + archives with readable names.
- `docveta import --dir`: the inverse, idempotent.
- `docveta import-paperless --dir`: reads paperless-ngx `document_exporter` output; maps
  owners → personal spaces, tags/correspondents/types/custom fields/notes/ASN; keeps
  OCR text (option to re-OCR).
- `docveta backup --to`: `pg_dump` + blob sync orchestration; Admin → System shows "last
  backup".
- **Acceptance:** export → wipe → import round-trips without loss; a paperless sample
  export imports completely.

### 2.10 Workflows — P1 (FR-P7)
- Migration `workflows`; triggers (added/processed/updated/schedule/mail), conditions
  reuse the search query AST, actions (set fields, tags, move, reminder, notify,
  webhook, run AI, request review); loop protection; run log in document history; UI
  builder with plain-language sentences ("When a document from HDFC is added…").
- **Acceptance:** a workflow tags and moves matching documents and notifies a user.

### 2.11 Apprise bridge & notification preferences — P1
- `apprise` channel type (POST to an Apprise API server); quiet hours per user
  (delay non-urgent events); weekly digest email (P2).

### 2.12 S3-compatible storage — P1 (§19.1)
- `storage.S3` implementing `Store` (minio-go); staging via local temp then multipart
  upload; signed URLs for worker downloads (optional); GC via listing.
- **Acceptance:** all integration tests pass with `DOCVETA_STORAGE=s3` against MinIO.

---

## 3. M3 — "Understand"

### 3.1 Embeddings & semantic search — P2 (DESIGN §14)
- `CREATE EXTENSION vector` migration (optional; feature disabled if unavailable);
  `embedding_spaces`; one chunks table per embedding space (`halfvec`, HNSW,
  iterative scans); chunker (page-aligned, 400–800 tokens, 15 % overlap, context
  header); background embedding job at `background` priority; model switch = new
  space + back-fill + atomic switch.
- Hybrid retrieval (FTS + vector, Reciprocal Rank Fusion) behind the existing search
  API (`mode=hybrid`); "Similar documents" on the document page.
- **Acceptance:** "electricity" finds a bill that only says "power consumption";
  permission filter verified by test.

### 3.2 Document versions & page operations — P2 (FR-D11, FR-D12)
- Replace file / new version; rotate, delete, reorder pages, split, merge (PDF writer
  task on a worker or pdfium in core); version list with restore.

### 3.3 Barcode separator pages & ASN barcodes — P2 (FR-P8)
- Worker capability `barcode`; split a batch scan at separator pages; read ASN barcodes;
  printable ASN label sheets (P3).

### 3.4 Scale & performance hardening — P2 (NFR-1/2)
- Synthetic dataset generator (100k documents, 1M pages, Indic + English text).
- k6 scenarios: search mix, list/sort, upload bursts, worker leasing with 10 workers.
- Fix sort-key queries to use indexes (see §6.2); facet counts; `EXPLAIN` regression
  checks in CI.
- **Acceptance:** search p95 < 300 ms on RK3588 at 100k documents.

### 3.5 Digest emails — P2 (FR-N5)

---

## 4. M4 — "Ask" and mobile

### 4.1 RAG "Ask your documents" — P3 (DESIGN §14.5)
- `/api/v1/ai/ask` (streaming), hybrid retrieval with permission filter, answers with
  mandatory citations linking to document + page, "not found" when confidence is low,
  conversation history per user (deletable), respects space AI policy.

### 4.2 MCP server — P3 (FR-I5)
- Tools `search_documents`, `get_document_text`, `list_tags`, … authenticated by a
  personal access token with read scope.

### 4.3 Android app — P3 (DESIGN §21)
- Separate repo or `android/`: Kotlin + Jetpack Compose, Kotlin client generated from
  the OpenAPI spec (needs 1.1), OAuth 2.1 + PKCE first-party login (server side to be
  built: `/oauth/authorize`, `/oauth/token`, rotating refresh tokens), Room offline
  cache via `/api/v1/changes`, WorkManager tus uploads, CameraX document scanner
  (F-Droid flavour without Google ML Kit), UnifiedPush/ntfy notifications.

### 4.4 Ensemble OCR, ClamAV scanning, ASN label printing — P3

---

## 5. Cross-cutting work

### 5.1 CI/CD — P0 (written, not yet run on GitHub)
> `.github/workflows/ci.yml` (gofmt, vet, `go test -race` with a Postgres service so the
> integration test runs, govulncheck, web build, worker tests, Docker build + Trivy) and
> `release.yml` (multi-arch images to ghcr.io with SBOM + provenance, linux binaries,
> GitHub release). Still open: golangci-lint, OSV-scanner, cosign signing, changelog
> automation.

- GitHub Actions: `go vet`, `staticcheck`/`golangci-lint`, `go test -race` with a
  Postgres service container (runs the integration test), `npm ci && tsc -b && vite
  build`, Python worker tests, `govulncheck`, OSV-scanner, Trivy image scan.
- Release workflow: goreleaser binaries (linux amd64/arm64), multi-arch images to
  `ghcr.io/anand34577/docveta`, `…/docveta-worker-tesseract`, `…/docveta-worker-rknn`
  (arm64), SBOM (Syft), cosign signatures, changelog from Conventional Commits.

### 5.2 Security hardening — P1 (partly done)
> Done: SSRF guard (dial-time check, `DOCVETA_ALLOW_LOCAL_TARGETS`), new-sign-in dedupe,
> `SECURITY.md`; also fixed an upload-scope bypass (upload-only tokens could edit and
> purge documents). Still open: threat-model review, multipart field limits, rate limits
> on token-auth failures, ZAP baseline.

- Threat-model review against DESIGN §22; SSRF guard for user-defined webhook/Gotify/
  ntfy URLs (block private ranges unless the admin allows "local network targets");
  request body limits on every JSON endpoint (done: 1 MiB) and multipart field limits;
  rate limits on share links and token auth failures; security headers verified with
  ZAP baseline scan; `SECURITY.md` with disclosure process.
- New-device login notification currently fires on *every* login — dedupe per
  user-agent/IP (see §6.6).

### 5.3 Accessibility & i18n — P1
- axe-core in Playwright for every page; manual screen-reader pass (NVDA, TalkBack).
- Extract UI strings to i18next; first translations: Hindi (हिन्दी); locale-aware
  number/currency formatting (INR lakh/crore grouping option).

### 5.4 Frontend tests — P1
- Vitest + Testing Library for components (entity picker, filters, upload store);
  Playwright e2e for scenarios S1–S6 (DESIGN §4.2) against the real backend + mock
  worker.

### 5.5 Observability — P2
- OpenTelemetry traces (optional), Grafana dashboard JSON in `deploy/`, River job
  metrics, per-engine OCR throughput histograms.

### 5.6 Documentation — P1
- User guide (with screenshots), admin guide, API guide (from OpenAPI), worker
  author guide (exists: `docs/workers.md`), `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  issue/PR templates, ADR files split out of DESIGN §28 into `docs/adr/`.

---

## 6. Known limitations & technical debt

Things found during the first implementation that need follow-up.

| # | Area | Issue | Planned fix | Pri |
|---|---|---|---|---|
| 6.1 | Sync | When a document moves to another space, members of the old space don't receive a tombstone in `/api/v1/changes`, so an offline client would keep a stale copy. | Record per-space removal events (e.g. `space_removals` table) and emit them in the change feed. | P2 (before Android) |
| 6.2 | Search | Non-relevance sorts use text expressions (`to_char(...)`) as keyset keys, which can't use the existing B-tree indexes; fine at 10k, slow near 100k+. | Typed keyset cursors per sort (timestamp/date/text) matching `documents_space_idx` / `documents_space_date_idx`. | P2 |
| 6.3 | Search | Facet counts (tags/types/years) for filter chips are not implemented; filter menus show global counts. | Facet query on the filtered set, cached briefly. | P2 |
| 6.4 | Frontend | Initial JS ≈ 212 KB gzipped vs. 200 KB budget (NFR-3). | Lazy-load inbox/document pages, trim Radix imports, measure with bundle analyser. | P2 |
| 6.5 | Frontend | ~~Saved-view rename uses `window.prompt`.~~ ✅ in-app `prompt()` dialog | — | done |
| 6.6 | Security | ~~"New sign-in" notification is sent for every login.~~ ✅ only for an unseen IP + device kind (remembered while old sessions are kept: session max + 7 days) | — | done |
| 6.7 | Notifications | SSE drops messages for slow clients (by design) and the UI only refetches on the next event; no `Last-Event-ID` replay. | Event IDs + replay from the `notifications` table on reconnect. | P2 |
| 6.8 | Processing | Thumbnails are JPEG, not WebP as the design says (Go has no pure-Go WebP encoder). | Keep JPEG (document the deviation) or generate WebP on a worker. | P3 |
| 6.9 | Processing | Multi-page TIFF thumbnails show only page 1 and `page_count` is unknown until OCR finishes. | Count frames in the core (x/image/tiff can't; parse IFD chain). | P3 |
| 6.10 | Processing | HEIC/AVIF have no thumbnail until a worker converts them; the `convert` task type is in the protocol but no worker implements it yet. | `convert` capability in the SDK (pillow-heif → JPEG/PDF) + core handling of the derived file. | P1 |
| 6.11 | Processing | The PP-OCR worker has no text-direction classifier; upside-down pages read poorly on the NPU path. | Add cls model or page-orientation detection before detection. | P2 |
| 6.12 | Database | `asn_seq` sequence is unused (ASN uses max+1 under an advisory lock). | Drop in a later migration. | P3 |
| 6.13 | API | Token-authenticated clients can't change their password or create tokens (intentional); the Android app will need its own OAuth flow (4.3). | — | P3 |
| 6.14 | Ops | The core container runs as UID 65532; host data directories must be chowned. | Optional entrypoint that fixes ownership, or document per platform (done in README). | P3 |
| 6.15 | Ops | ~~No `docveta doctor` command yet.~~ ✅ `docveta doctor [--verify-blobs]` | — | done |
| 6.16 | Config | `DOCVETA_STORAGE` / S3 and Gotenberg variables mentioned in DESIGN are not implemented. | See 2.12 and 2.6. | P1 |

---

## 7. Release checklist for v0.1.0

- [ ] §0.1–0.5 verification complete — 0.1 ✅; 0.2 partly; 0.3 partly (setup, login, SSO
      settings, saved views, and every main page at 360 px without horizontal scrolling;
      full pipeline run with the mock worker); PWA install/share target still to check in
      a real Chrome/Android; 0.4/0.5 need Tesseract and Rockchip boards
- [x] 1.1 OpenAPI spec with route/spec parity test
- [ ] 5.1 CI green on every push; images published to ghcr.io/anand34577 — workflows written; first push pending
- [x] 5.2 SSRF guard and login-notification dedupe
- [x] 6.5, 6.6, 6.15 fixed
- [ ] Upgrade test: data created by v0.1.0-rc migrates cleanly to v0.1.0
- [ ] Backup & restore rehearsal following `docs/operations.md`
- [ ] README screenshots — CHANGELOG, `SECURITY.md`, `CONTRIBUTING.md` ✅
- [ ] Tag `v0.1.0`

---

## 8. Suggested order of work

1. **Week 1:** §0.1 + §0.2 (needs a database) → §5.1 CI so it stays green.
2. **Week 2:** §0.3 manual UI pass, §0.4 Tesseract, §0.5 RKNN on your boards.
3. **Week 3:** §1.1 OpenAPI, §1.2 tus, §6.5/6.6/6.15, §5.2 → **v0.1.0**.
4. **Next:** §2.1 AI classification, §2.2 custom fields, §2.3 reminders (highest user value),
   then §2.4–2.12 in any order.
5. **Then:** §3 (semantic search, scale), §4 (RAG, Android).
