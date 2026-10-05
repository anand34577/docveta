# Changelog

All notable changes to Docveta. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.7.0] - 2026-10-05

### Added
- **Android app: everything the web app does.** Previously the app covered scanning, the Inbox,
  reading, filing and Ask; the rest needed a browser. New in the app:
  - Documents: filters for sender, type, date range (incl. financial year), untagged and status;
    sorting; **saved views** (save, pin, open, change, rename, reorder, delete); hold to
    **select several** and mark reviewed, change tags, move to another space, merge into one PDF,
    share, reprocess or move to Trash (restore / delete forever in Trash); **Upload files** from the
    phone (not only scans and the share sheet); *All reviewed* now covers the whole Inbox.
  - A document: custom fields, space, language, where the paper is, archive number; **Text**,
    **Versions** (upload a new version, restore, share an old one), **Similar** and **History**
    tabs; **Arrange pages** (turn, reorder, delete, copy pages into a new document); share the
    searchable PDF; re-read text (force OCR); ask AI for suggestions; *Ask about this document*;
    @mentions in notes.
  - Spaces: create; name, colour, description, language; members and roles; leave or delete; tags,
    correspondents and document types with automatic matching and merging; custom fields;
    workflows (editor and run history); AI policy and statistics; separator sheets and ASN labels.
  - Settings: profile (name, date format, time zone, light/dark theme), password, two-step
    sign-in (turn on with *Add to authenticator app*, recovery codes, turn off), single sign-on
    accounts, signed-in browsers, API tokens, notification channels and quiet hours.
  - Administration: users and invitations, workers and the task queue, processing settings, AI
    providers, watched folders, Office conversion, single sign-on, email, alert channels, export,
    server address and health, audit log.
  - First start of a new server (create the administrator) and accepting an invitation link.

### Changed
- Android: administrators' phones get a token with the admin permission at sign-in. Phones signed
  in before can turn it on in Administration by confirming the password.
- Android: changes the server only allows from a fresh sign-in (password, two-step sign-in, new API
  tokens) ask for the password and use a short-lived session; the phone's token can't make them.

### Fixed
- **New sign-in alerts** weren't sent when someone signed in with another browser on the same
  computer: only a new IP address or operating system counted as a new device. The browser now
  counts too, and the alert says which one ("Firefox on Windows").
- Notification channels: an email channel now warns when the server can't send email yet
  (Administration → Email isn't set up), instead of saving a channel that never delivers.
  People who aren't administrators no longer see administrator-only events (worker offline, low
  disk space) in the list; *Low disk space* says when it fires (under 5 GB free).
- Web on phones: rows in Users, notification channels, API tokens, AI providers, watched folders,
  workflows and members squeezed the name into a narrow column; their buttons now move to their
  own line. Tab rows that scroll sideways fade at the edge that has more, and the current section
  is scrolled into view.
- Web: a mistyped settings, administration or space settings address showed an empty page; it
  now shows *Page not found*. "1 documents" and "1 members" read correctly.
- Web: the document's tab row (Details, Notes, Text…) showed scrollbars, including a vertical
  one. Tab rows, the settings section links, the shared-link list and the bulk action bar now
  scroll sideways only, without a drawn scrollbar, and a chosen tab scrolls into view.
- Android: a document's file was downloaded again after every title or tag change; it is now kept
  until the file itself changes, and old copies are removed.
- Android: opening a document while it was processing could start several refresh loops.

## [0.6.1] - 2026-10-05

### Fixed
- Signing in with single sign-on from the login page returned people to the login page, and the
  address grew with every attempt (`/login?redirect=/login?redirect=…`). The app no longer
  redirects to the login page from the login page, and sign-in pages are never used as the place
  to return to (old nested links are unwrapped).
- Hindi in PDFs typeset with pre-Unicode fonts (Walkman-Chanakya, DV-TT, Kruti Dev: common in exam
  papers and government documents) came out as symbols like "¬⁄UËˇÊÊ", because Docveta trusted the
  PDF's text layer. Such pages are now recognised with OCR instead. Run **Process again** on
  documents added before.
- Docker: the Rockchip NPU worker didn't start on current RK3576/RK3588 kernels, which have no
  `/dev/rknpu` (the NPU is reached through `/dev/dri`).

### Changed
- Android: the app stays in portrait, and screens move like other Android apps: opening a screen
  slides it in from the side (Back reverses it and follows the back gesture), bottom-bar tabs fade
  through, and the scanner slides up from the bottom.

## [0.6.0] - 2026-10-04

### Added
- **Ask about one document**: ⋯ → *Ask about this document* ("Summarise this", "What are the important dates?").
- Ask: Markdown answers (lists, tables, bold, code) on the web and in the Android app, a **Stop** button,
  *Try again*, *Copy*, a "Searching…/Writing…" status, cited sources first ("Also searched N more"),
  rename and delete conversations, *Delete all conversations*, older conversations load page by page,
  and the conversation list on phones (**History**).
- **pgvector** support for meaning-based search: used automatically when the extension is installed
  (HNSW index per vector size, built in the background; pgvector 0.8 iterative scans for filtered
  searches); existing vectors are copied over by the maintenance job. Without pgvector the in-app
  scan now keeps a bounded top-k list.
- **Single sign-on in the Android app** (browser sign-in with PKCE), and **Settings → Security →
  Single sign-on** to connect or disconnect an account.
- Viewer: **Fit page** and **Fit width**, pinch-to-zoom and Ctrl/⌘ + wheel zoom, a picture viewer
  with the same controls, and a page indicator in the Android viewer.
- Administration → System: **Server address** (used in emailed and pushed links) and a switch to let
  everyone's notification channels reach the local network.
- `DOCVETA_METRICS_TOKEN` for scraping `/metrics` from outside the local network.
- Wiki: [AI and Ask](docs/wiki/AI-and-Ask.md) and [Single sign-on](docs/wiki/Single-sign-on.md).

### Fixed
- **Single sign-on** didn't work in common setups: issuers with a trailing `/` (Authentik, Zitadel,
  Auth0) failed discovery because Docveta removed the slash, and the redirect URI sent to the provider
  was `http://localhost:8080/…` unless `DOCVETA_BASE_URL` was set, so providers rejected it. The
  redirect URI now follows the address in use (and Administration shows the exact one to register);
  userinfo is read when email or groups are missing from the ID token; `email_verified: "true"`
  (string) is accepted; error messages say what to fix.
- **Hindi documents weren't recognised at all**: the OCR engines used only the space's language
  (English by default), so every Devanagari line was dropped. The PaddleOCR engines (ONNX, Rockchip,
  Allwinner) now detect each page's script themselves and read mixed English/Hindi lines line by
  line; Tesseract also reads Hindi when installed (`DOCVETA_OCR_EXTRA_LANGUAGES`). The document's
  language follows the text unless someone set it.
- Document viewer: tapping zoom/turn on a touch screen selected document text (the text layer sat
  above the controls), on shared links and in the Inbox. Shared links showed recognised pictures as a
  broken image.
- Ask: follow-up questions ("and last year?") found nothing; long pages crowded out other sources;
  a failed first question left an empty conversation; stopping an answer lost it; reasoning models'
  `<think>` text appeared in answers; AI server errors showed as "Something went wrong"; a stuck model
  kept the answer spinning forever; slow-loading models were cut off by reverse proxies (keep-alives
  are now sent); stopping in the Android app didn't cancel the request.
- Meaning-based search: chunks were sized in characters, so Hindi and other Indian-language pages
  overflowed embedding models' token limits.
- Links in emails, Gotify, ntfy and invitations pointed at `localhost` when `DOCVETA_BASE_URL` wasn't set.
- Notifications to Gotify/ntfy/Apprise on the local network were blocked even for administrators;
  ntfy titles in Hindi; Microsoft 365 / Outlook.com SMTP (AUTH LOGIN only) couldn't sign in.
- Inbox: **All reviewed** only marked the documents loaded on screen; keyboard shortcuts fired while a
  dialog was open; the three columns squeezed the preview on laptop screens. Esc in a bulk-action
  dialog also cleared the selection.
- Home page overflowed sideways on phones with long titles; list view squeezed titles to a few letters.
- Android: no way out when the server address stopped working (now *Sign out and change server*);
  tab colours made the active tab hard to see.

### Changed
- Large libraries: indexes behind foreign keys (purging documents no longer scans every page of every
  document), taxonomy counts in one grouped query, result totals counted up to 100,000 ("100,000+"),
  filter counts skipped above 50,000 matches.
- `/metrics` answers only local-network addresses unless `DOCVETA_METRICS_TOKEN` is set.

## [0.5.2] - 2026-10-04

### Fixed
- Android 17: the app could not connect to a server at a local address (192.168.x.x, 10.x.x.x, .local); it now declares and asks for the new local-network permission.
- Android: app bars no longer leave a gap under the status bar on some screens or sit under it on others. Every screen now handles the status and navigation bars exactly once (the shell used to pad all screens, and screens with their own app bar added the inset again).

## [0.5.1] - 2026-10-04

### Added
- Android: scanning with Google's ML Kit document scanner (live edge detection, crop, clean-up,
  gallery import), then the app's review: reorder, one combined PDF or separate pictures, upload
  to the server. Phones without Google Play services keep the built-in scanner.

### Fixed
- Dropdowns were white in the dark theme (the select lost its background colour when its classes were merged), and had no arrow.
- Ask: "l is not a function" crash in current browsers, where `scrollIntoView` returns a Promise that React tried to call as a cleanup function.
- Email: "SMTP auth: unencrypted connection" when the mail server (or a relay/proxy) is set to Security "None"; the login is now sent as the administrator chose.
- Inbox: the keyboard shortcut hints at the bottom lost their spaces and wrapped one word per line.
- The release workflow never built the Android app, so no APK was published. It now builds a
  signed `docveta-android-<version>.apk` and attaches it to the release; the app's version
  follows the tag.

## [0.5.0] - 2026-10-04

### Added
- **Android app** (`android/`): Kotlin + Jetpack Compose. Sign in with password (and two-step
  code) or an access token; Inbox with swipe-to-review and Undo; search, filters and
  meaning-based search; PDF/image viewer with pinch zoom; edit title, date, sender, type and
  tags; notes; AI suggestions; share links; Ask your documents with cited pages; notifications;
  Trash; app lock; "Share to Docveta" from other apps.
- **Document scanner** in the app, without Google services: live page detection with an outline,
  automatic capture when the page is held still, draggable corner crop with a magnifier,
  perspective correction that recovers the page's true proportions, Enhanced / Grey / Black &
  white looks, multi-page scans, one PDF or separate pictures. Uploads are resumable (tus) and
  survive restarts and dropped connections.
- Invitations by link, two-step sign-in (TOTP + recovery codes), share links, custom fields,
  workflows, AI suggestions / meaning-based search / similar documents / Ask, MCP server,
  Office documents (Gotenberg), watched folders, S3 storage, barcode batch scanning, PDF page
  tools (rotate, reorder, delete, split, merge), versions, resumable uploads, quiet hours and
  Apprise notifications, export/import, facet counts in filters.
- Web UI for all of the above, plus: create-space dialog, upload space chooser, date inputs in
  the account's date format, "Reviewed & next" with Undo, Empty Trash, saved-views manager,
  keyboard shortcut help (`?`), notifications page, PDF thumbnails and turn, mobile
  Preview/Details switch, space colours, bulk tags across spaces.

### Changed
- Every page loads on demand; the first screen is about 175 KB gzipped.
- The change feed reports documents moved out of a space; live notifications are replayed after
  a reconnect; HEIC/AVIF and multi-page TIFF are handled.

### Fixed
- Web resumable uploads treated a duplicate-document answer (409) as an offset mismatch.

## [0.4.1] - 2026-10-03

### Fixed
- Linux: the service didn't start on systems without `video` and `render` groups (minimal
  Debian/Ubuntu, many container templates and cloud VMs): the systemd unit required them.
  `install.sh` already adds Docveta to those groups where they exist, so the unit no longer
  names them. CI now installs the package on Debian (systemd) and Alpine (OpenRC) and checks
  the service runs.

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
- Windows: a silent uninstall (`/VERYSILENT`) hung on a message box, and `docveta service stop`
  / uninstall didn't wait for Docveta to finish shutting down.
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
