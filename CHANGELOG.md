# Changelog

All notable changes to Docveta. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed
- Allwinner worker: the setup guide now says to use VIPLite 2.0 on every A733, Armbian's
  vendor kernel included (its driver reports 1.13.0 in sysfs, but the 1.13 libraries don't
  work with it). The worker no longer tries to set the NPU clock unless
  `DOCVETA_NPU_CLOCK_PERCENT` is set, which avoids a warning at every start.

## [0.2.0] - 2026-10-03

### Added
- Text recognition on the NPU of Allwinner A733 boards such as the Radxa Cubie A7A
  (`workers/allwinner`). Finding and reading text both run on the NPU, so the processor stays
  free, which helps when the board also runs Proxmox containers. Includes a converter for
  Allwinner's ACUITY Toolkit, `worker.py --probe` to check the NPU and models, and a setup
  guide for Proxmox LXC.

## [0.1.0] - 2026-10-03

### Added
- **Zero-configuration start.** Without a database, Docveta shows a setup page in the browser
  that tests the PostgreSQL connection, can create the database, and saves the settings.
  Requests from other computers need a one-time setup code from the log. The secret key is
  generated on first start. Settings can live in `docveta.conf` files (next to the program and
  in the data folder); environment variables still work and take precedence.
- **GPU/NPU/CPU text recognition** with the new `docveta-ocr` engine (PaddleOCR on ONNX
  Runtime): any DirectX 12 GPU on Windows (integrated or external, via DirectML), NVIDIA CUDA,
  Apple GPU/Neural Engine, Intel OpenVINO, Qualcomm QNN, with automatic CPU fallback.
  `DOCVETA_OCR_DEVICE=auto|gpu|igpu|npu|cpu`, `--list-devices`, `--self-test`.
- Docveta starts the bundled OCR engine (`ocr/` next to the program) by itself, registers it
  as worker `local-ocr`, restarts it if it stops, and ends it with Docveta.
- Windows service support (`docveta service install|uninstall|start|stop`) and an Inno Setup
  installer (x64, arm64, x86) with port, OCR device and firewall choices.
- Linux `install.sh` + systemd unit, macOS `install.sh` + launchd agent.
- Release workflow: binaries for Windows, Linux and macOS on x64, ARM64, ARMv7 and x86, OCR
  engine packages, Windows installers, Docker images (including `docveta-ocr`), checksums.
- User guide in the GitHub wiki (`docs/wiki`, published automatically).
- OpenAPI 3 specification of the whole API, served at `/api/v1/openapi.yaml`. Tests fail
  when routes and the spec disagree, and the integration test validates real responses
  against it.
- `docveta doctor [--verify-blobs]`: checks configuration, database, schema version, clock,
  storage, the secret key, OCR workers and every stored file (documents and OCR results).
- `DOCVETA_ALLOW_LOCAL_TARGETS` setting (see Security).
- CI (Go with race detector and the PostgreSQL integration test, web build, worker tests,
  govulncheck, image scan) and a release workflow publishing multi-arch images to ghcr.io.
- `SECURITY.md`, `CONTRIBUTING.md`.

### Changed
- "New sign-in" notifications are only sent for a new device or IP, not for every login.
- Renaming a saved view uses an in-app dialog instead of the browser prompt.
- The login page sends people who are already signed in straight to the app.

### Fixed
- 32-bit builds (x86, ARMv7) didn't compile, and the sync cursor would have overflowed on them.
- Retrying a failed OCR task from *Administration → Processing* within a minute of the
  failure left the document stuck in "processing" forever (the finalize step was
  de-duplicated away).
- Docker builds no longer send `node_modules`, local data or build output to the build
  context (`.dockerignore` for the core and worker images).
- API tokens with only the `upload` scope could edit, trash and permanently delete
  documents. They can now only upload.
- Administration → Single sign-on crashed on a fresh install (group lists were `null`).
- Document history entries without details returned `null` instead of an empty object.

### Security
- Notification channels (Gotify, ntfy, webhooks) can no longer reach loopback, private,
  link-local (including cloud metadata) or CGNAT addresses unless an administrator sets
  `DOCVETA_ALLOW_LOCAL_TARGETS=true`. Checked at connect time, so DNS tricks and redirects
  are covered.
- Email notification addresses are validated as real addresses.
