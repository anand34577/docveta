# Docveta

**Your documents, organised and searchable.** Docveta keeps bills, IDs,
contracts, certificates and records in one place. It reads the text in your scans (on your
graphics card, an AI accelerator or the processor), sorts documents into spaces with tags,
and finds anything in seconds, in English, Hindi and other Indian scripts.

Docveta is **one program** with the web app built in. It runs on Windows, Linux and macOS, on
ordinary PCs, servers and small boards like the Raspberry Pi. You use it in a web browser,
from your computer or phone.

## Get started

| Where | How |
|---|---|
| **Windows 10/11 PC or server** | Run `docveta-setup-…-windows-x64.exe` (`-arm64` for Windows on ARM): installs a service with text recognition and the database. Or unzip the portable ZIP and double-click `docveta.exe`. |
| **Linux** server, PC, VM, LXC container, Raspberry Pi | `curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh && sudo sh get-docveta.sh` |
| **Proxmox** container on a board with an NPU | First `sudo sh get-docveta.sh --proxmox <CTID>` on the host, then the Linux line inside the container |
| **macOS** (Apple silicon or Intel) | `tar xzf docveta-…-darwin-arm64.tar.gz && cd docveta-* && ./install.sh` |
| **Docker**, NAS (Synology, Unraid, TrueNAS), Portainer | `docker compose up -d` with [deploy/docker-compose.yml](https://github.com/anand34577/docveta/blob/main/deploy/docker-compose.yml), nothing to edit |
| Old 32-bit Windows 10 | The x86 installer runs, but without text recognition or the built-in database: it needs a [PostgreSQL server](https://github.com/anand34577/docveta/wiki/Database) |
| Windows 7 / 8.1, very old PCs | Not supported (too old for Docveta's toolchain). Install Docveta on another machine and use it from that PC's browser. |

Then open the address shown (usually <http://localhost:8080>), click **Save and start** to use
the built-in database, and create your account. No PostgreSQL to install, nothing to configure.

Step-by-step guides: [Windows](Install-on-Windows) · [Linux](Install-on-Linux) ·
[macOS](Install-on-macOS) · [Docker](Install-with-Docker) · then [First steps](First-steps).

## Make it yours

| I want to… | Read |
|---|---|
| Read text from scans faster, on my GPU or NPU | [OCR engines](OCR-engines) |
| Change the port, data folder or other settings | [Configuration](Configuration) |
| Use Docveta from other devices, with HTTPS | [Remote access and HTTPS](Remote-access-and-HTTPS) |
| Keep my documents safe | [Backup and restore](Backup-and-restore) |
| Install a new version | [Upgrading](Upgrading) |
| Fix a problem | [Troubleshooting](Troubleshooting) |
| Build Docveta myself or help develop it | [Building from source](Building-from-source) |

## Downloads

Every release on the [Releases page](https://github.com/anand34577/docveta/releases) has:

| File | What it is |
|---|---|
| `docveta-setup-<version>-windows-x64.exe` | Windows installer (runs Docveta as a service, includes text recognition) |
| `docveta-<version>-<system>-<cpu>.zip / .tar.gz` | Docveta itself, for every system: unzip and run |
| `docveta-ocr-<version>-<system>-<cpu>.zip` | The text recognition engine: unzip next to Docveta |
| `SHA256SUMS` | Checksums to verify downloads |

Systems: `windows`, `linux`, `darwin` (macOS). Processors: `x64` (Intel/AMD 64-bit), `arm64`
(Apple silicon, Snapdragon, Raspberry Pi 4/5, Rockchip, Ampere), `armv7` (older 32-bit ARM
boards), `x86` (old 32-bit PCs).
