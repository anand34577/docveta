# Docveta

**Your documents, organised and searchable.** Docveta keeps bills, IDs,
contracts, certificates and records in one place. It reads the text in your scans (on your
graphics card, an AI accelerator or the processor), sorts documents into spaces with tags,
and finds anything in seconds, in English, Hindi and other Indian scripts.

Docveta is **one program** with the web app built in. It runs on Windows, Linux and macOS, on
ordinary PCs, servers and small boards like the Raspberry Pi. You use it in a web browser,
from your computer or phone.

## Get started in three steps

1. **Install PostgreSQL**, the database Docveta keeps its records in. → [Database](Database)
2. **Install Docveta** for your system:
   - [Windows](Install-on-Windows): installer or portable ZIP
   - [Linux](Install-on-Linux): servers, Raspberry Pi, Rockchip boards
   - [macOS](Install-on-macOS)
   - [Docker](Install-with-Docker)
3. **Open Docveta in your browser**, connect the database on the page that appears, and
   create your account. → [First steps](First-steps)

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
