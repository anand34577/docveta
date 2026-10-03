## Which file do I need?

| You have | Download |
|---|---|
| **Windows PC or server** (most people) | `docveta-setup-…-windows-x64.exe`: installer with text recognition on your GPU or CPU |
| Windows on ARM (Snapdragon) | `docveta-setup-…-windows-arm64.exe` |
| Windows, no installation (USB stick, testing) | `docveta-…-windows-x64.zip` + `docveta-ocr-…-windows-x64.zip` (unzip both into one folder) |
| **Linux** in one step (servers, VMs, containers, boards, incl. Allwinner A733 NPU) | `get-docveta.sh`: `curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh && sudo sh get-docveta.sh` |
| **Mac** (Apple silicon) | `docveta-…-darwin-arm64.tar.gz` + `docveta-ocr-…-darwin-arm64.zip` |
| Mac (Intel) | `docveta-…-darwin-x64.tar.gz` |
| Docker, NAS (Synology, Unraid, TrueNAS) | `docker compose up -d` with [docker-compose.yml](https://github.com/anand34577/docveta/blob/main/deploy/docker-compose.yml): nothing to edit |

Nothing else to install: on first start, click **Save and start** in your browser to use the
built-in database (or connect your own PostgreSQL 16+).

Step-by-step guides: **[Docveta wiki](https://github.com/anand34577/docveta/wiki)** ·
check downloads with `SHA256SUMS` · changes: [CHANGELOG.md](https://github.com/anand34577/docveta/blob/main/CHANGELOG.md)
