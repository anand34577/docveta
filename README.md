# Docveta

**Your documents, organised and searchable.** Docveta is a lightweight,
self-hosted document management system for households, small organisations and teams.
Capture bills, IDs, contracts, certificates and records from any device; Docveta reads the
text (on your graphics card, an NPU or the CPU), organises them, and finds them in seconds.

- **Simple to use, full-featured underneath** — Inbox-first workflow, search-as-you-type,
  saved views, bulk edit, notes with @mentions, trash, history.
- **Spaces** — Personal, Family, Accounts, Client X… one easy permission model (owner /
  editor / viewer).
- **OCR on GPU, NPU or CPU** — the bundled PaddleOCR engine uses any Windows GPU
  (integrated or external, via DirectML), NVIDIA CUDA, Apple GPU/Neural Engine, NPUs, and
  falls back to the CPU. Also **Rockchip RK3588/RK3576/RK3566 NPUs** and **Tesseract** (100+
  languages). Engines are pluggable: add your own in ~100 lines.
- **Works without OCR too** — born-digital PDFs are searchable immediately.
- **Search that understands Indic scripts and IDs** — Unicode-aware indexing, typo
  tolerance, `tag:` / `from:` / `date:` filters, highlighted snippets with page numbers.
- **Single sign-on (OIDC)**, API tokens, audit log.
- **Notifications** — in-app (live), Gotify, ntfy, email (SMTP), signed webhooks.
- **One binary, every platform** — the server and web app in a single file for Windows,
  Linux and macOS (x64, ARM64, ARMv7, x86), plus PostgreSQL. Windows installer with a
  service, portable ZIP, systemd/launchd scripts, Docker images.
- **Phone-ready** — installable web app with Android "Share to Docveta"; API designed for a
  future native app (delta sync, token auth).

> Status: **early development (v0.1)**. See [docs/DESIGN.md](docs/DESIGN.md) for the full
> design, the *Implementation status* section there for what's done, and
> [docs/ROADMAP.md](docs/ROADMAP.md) for everything that remains.

## Install

**Step-by-step guides for every system are in the [wiki](https://github.com/anand34577/docveta/wiki).**
Downloads are on the [Releases page](https://github.com/anand34577/docveta/releases).

1. Install **PostgreSQL 16+** ([how](https://github.com/anand34577/docveta/wiki/Database)).
2. Install Docveta:
   - **Windows**: run `docveta-setup-…-windows-x64.exe` (installs a service, includes GPU/CPU
     text recognition), or unzip the portable ZIP and double-click `docveta.exe`.
   - **Linux**: `tar xzf docveta-…-linux-x64.tar.gz && cd docveta-* && sudo ./install.sh`
   - **macOS**: `tar xzf docveta-…-darwin-arm64.tar.gz && cd docveta-* && ./install.sh`
   - **Docker**: `docker compose up -d` with [deploy/docker-compose.yml](deploy/docker-compose.yml)
3. Open <http://localhost:8080>. A setup page asks for the database (Docveta can create it),
   then you create your account. No configuration files needed.

For text recognition, unzip `docveta-ocr-…zip` next to Docveta: Docveta starts it automatically
and picks your GPU, NPU or CPU (`DOCVETA_OCR_DEVICE=auto|gpu|igpu|npu|cpu`).
See [OCR engines](https://github.com/anand34577/docveta/wiki/OCR-engines).

More: [configuration](https://github.com/anand34577/docveta/wiki/Configuration) ·
[backups](https://github.com/anand34577/docveta/wiki/Backup-and-restore) ·
[worker protocol](docs/workers.md) · [API spec](internal/api/openapi.yaml) · [design](docs/DESIGN.md).

## Development

Requirements: Go 1.26+, Node 22+, PostgreSQL 16+ (with `pg_trgm`), Python 3.10+ for workers.

```bash
# backend (http://localhost:8080): first start shows the database setup page,
# or set DOCVETA_DATABASE_URL=postgres://docveta:docveta@localhost:5432/docveta to skip it
DOCVETA_DEV=true go run ./cmd/docveta serve

# frontend with hot reload (http://localhost:5173, proxies /api to :8080)
cd web && npm install && npm run dev

# a fake OCR worker to exercise the pipeline without real OCR
pip install -e workers/sdk-python
DOCVETA_URL=http://localhost:8080 DOCVETA_WORKER_TOKEN=dvt_wrk_... docveta-worker-mock
```

Tests: `make test` (Go unit tests, TypeScript type-check, worker tests). The end-to-end
test runs when a database is available:
`DOCVETA_TEST_DATABASE_URL=postgres://... go test ./internal/app -run Integration -v`
(it uses a throwaway schema).

## Project layout

```
cmd/docveta/            server + admin CLI (serve, migrate, doctor, user, service)
internal/             Go packages (api, identity, spaces, documents, search, pipeline, notify, …)
web/                  React + TypeScript app (built into internal/webui/dist and embedded)
workers/sdk-python/   SDK for OCR workers (protocol client, rasterisation, searchable PDFs, PP-OCR)
workers/onnx/         GPU/NPU/CPU OCR engine (PaddleOCR on ONNX Runtime), bundled as docveta-ocr
workers/rknn/         Rockchip NPU worker (RK3588/RK3576/RK3566) + model conversion
workers/tesseract/    CPU worker (Tesseract 5)
deploy/               Docker Compose, Windows installer (Inno Setup), Linux/macOS install scripts
docs/wiki/            user guide, published to the GitHub wiki
docs/                 design, operations, worker protocol
```

## License

- Docveta server, web app and reference workers: [GNU AGPL-3.0](LICENSE).
- Worker SDK (`workers/sdk-python`): [Apache-2.0](workers/sdk-python/LICENSE), so anyone can
  build OCR engines on top of it, including proprietary ones.

Source: <https://github.com/anand34577/docveta>
