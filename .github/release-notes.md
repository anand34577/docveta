## Which file do I need?

| You have | Download |
|---|---|
| **Windows PC or server** (most people) | `docveta-setup-…-windows-x64.exe`: installer with text recognition on your GPU or CPU |
| Windows on ARM (Snapdragon) | `docveta-setup-…-windows-arm64.exe` |
| Windows, no installation (USB stick, testing) | `docveta-…-windows-x64.zip` + `docveta-ocr-…-windows-x64.zip` (unzip both into one folder) |
| **Linux server** (Ubuntu, Debian, Fedora, …) | `docveta-…-linux-x64.tar.gz` (or `-arm64` for Raspberry Pi 4/5, Rockchip, Ampere) + optionally `docveta-ocr-…-linux-…zip` |
| **Mac** (Apple silicon) | `docveta-…-darwin-arm64.tar.gz` + `docveta-ocr-…-darwin-arm64.zip` |
| Mac (Intel) | `docveta-…-darwin-x64.tar.gz` |
| Docker | `docker compose` with `ghcr.io/anand34577/docveta` (see the wiki) |
| OCR on an Allwinner A733 board's NPU (Radxa Cubie A7A) | `docveta-worker-allwinner-…-linux-arm64.tar.gz`, set up as described in [its guide](https://github.com/anand34577/docveta/blob/main/workers/allwinner/README.md) |

Docveta needs **PostgreSQL 16 or newer**. On first start a page in your browser asks for the
database details.

Step-by-step guides: **[Docveta wiki](https://github.com/anand34577/docveta/wiki)** ·
check downloads with `SHA256SUMS` · changes: [CHANGELOG.md](https://github.com/anand34577/docveta/blob/main/CHANGELOG.md)
