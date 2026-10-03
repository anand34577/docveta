# Changelog

All notable changes to Docveta. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.4.0] - 2026-10-03

Installing Docveta no longer needs a database server, worker tokens or edited config files.

### Added
- **Built-in database.** The setup page's default is now "Built-in database": one click and
  Docveta runs its own PostgreSQL 17 inside its data folder, listening on this computer only
  with a generated password. Works on Windows (x64, ARM), macOS, and Linux including Alpine.
  The Windows downloads include it, so setup works offline; elsewhere it's downloaded once and
  checked against a pinned SHA-256. Connecting your own PostgreSQL still works as before.
- **One-command Linux install:** `get-docveta.sh` installs Docveta as a service (systemd, or
  OpenRC on Alpine) with text recognition for the machine: the NPU on Allwinner A733 boards,
  otherwise the processor. All downloads are checked against the release's checksums.
  `--proxmox <CTID>` on a Proxmox host gives a container the NPU and the fast cores.
- **Docker without editing anything:** `docker compose up -d` generates the database password,
  and text recognition engines enroll themselves for a token (`POST /worker/v1/enroll` with a
  key only the engine containers can read). PaddleOCR on the processor now runs by default.
  Data lives in Docker volumes, so no `chown` either. Older `.env` files keep working.
- Allwinner A733: the release package now includes a prebuilt detection model (calibrated on
  generated pages, see `workers/allwinner/prebuilt`), and the installer fetches Allwinner's
  VIPLite libraries pinned by checksum. Docveta starts the engine itself; no worker service or
  token is needed.

### Changed
- The Windows installer no longer asks you to install PostgreSQL first.
- Docker profiles: `tesseract` and `npu-rockchip` (the old `cpu-ocr` and `npu` still work).

### Fixed
- Windows installer: the service started before its settings file was written, so a fresh
  install kept its data in `C:\Program Files\Docveta\data` until the next restart, and then
  asked for the database again. The settings are now written before the service starts.
- Search results: with only a few results, a document's thumbnail filled half the screen. The
  grid measured its width before the results list existed.

## [0.3.0] - 2026-10-03

### Changed
- Allwinner worker: text is still found on the NPU, but each line is now read on the CPU with
  the same ONNX models as the GPU/CPU engine. Tested on a Cubie A7A, the NPU can't run the
  PaddleOCR reading network accurately: int16 output drifts to nothing, int8 gets about one
  character in five wrong, and float16 is 20 times slower and still wrong. Reading on the CPU
  takes about 0.3 s per line on a Cortex-A76 core.
- The release package now includes the reading models for English, Devanagari, Tamil, Telugu
  and Kannada. You only convert the detection model (`det.nb`), and the converter now builds
  only that.
- The setup guide shows how to pin the Proxmox container to the A733's two fast cores.

## [0.2.2] - 2026-10-03

### Fixed
- Allwinner worker: text recognition on the NPU returned garbage, because the compiled models
  expect normalised input in BGR order and the worker sent raw RGB pixels. The worker now
  normalises each tile with a lookup table, matching the ONNX engine. Models built with 0.2.1
  keep working; no reconversion needed.
- `docveta-worker-allwinner --probe` now also reads a rendered test image and fails if no text
  comes back, so a run that's fast but wrong no longer passes.
- Model converters (Allwinner and Rockchip): onnxsim 0.7 corrupts the PP-OCR recognition
  models when fixing their input size. The install instructions now pin `onnxsim==0.4.36`, and
  the converter checks each model and stops with a clear message instead of failing later in
  the NPU toolkit. They also list `setuptools`, which Paddle needs on Python 3.12+.

## [0.2.1] - 2026-10-03

### Added
- `docveta-worker-allwinner-<version>-linux-arm64.tar.gz`: the Allwinner NPU worker as a ready
  program, so the board needs no git checkout or Python. Add Allwinner's two VIPLite
  libraries and your converted models next to it (see `workers/allwinner/README.md`).

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
