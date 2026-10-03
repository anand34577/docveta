# Install on Linux

Docveta runs on any Linux distribution with systemd, on PCs, servers and boards (Raspberry Pi
4/5, Rockchip, Radxa Cubie A7A, Orange Pi, Ampere). You don't need to install a database first.

## Install (one command)

```bash
curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh
sudo sh get-docveta.sh
```

The script:

- downloads the latest Docveta for your processor and checks every file against the
  release's checksums
- installs it as the service `docveta` (program in `/opt/docveta`, data in `/var/lib/docveta`)
- adds text recognition for this machine: the **NPU** on Allwinner A733 boards (Radxa Cubie
  A7A), otherwise the **processor** (x64 and ARM 64-bit). Docveta starts and connects the
  engine itself; there are no tokens to copy.

Then open the address it prints, click **Save and start** to use the built-in database, and
create your account. From another computer the page asks for a setup code:

```bash
sudo cat /var/lib/docveta/setup-code.txt
```

Running the script again upgrades Docveta and keeps your data and settings.

| Option | |
|---|---|
| `--port 9000` | Use another port (default 8080) |
| `--version 0.4.0` | Install a specific version |
| `--no-ocr` | Skip text recognition (born-digital PDFs are still searchable) |

Everyday commands:

```bash
sudo systemctl status docveta
sudo systemctl restart docveta       # after editing /opt/docveta/docveta.conf
journalctl -u docveta -f             # live log
```

## Proxmox containers (LXC)

On the Proxmox **host**, give the container the NPU (if the board has one) and the fast cores,
then run the normal install inside the container:

```bash
curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh
sudo sh get-docveta.sh --proxmox <CTID>
```

## By hand

Download `docveta-<version>-linux-<arch>.tar.gz` (`uname -m`: `x86_64` → x64, `aarch64` →
arm64, `armv7l` → armv7), unpack it, optionally unpack `docveta-ocr-<version>-linux-<arch>.zip`
into the same folder (it creates `ocr/`), and run `sudo ./install.sh`. Or just run `./docveta`:
it starts on port 8080 and keeps its data in `./data`.

> Don't run Docveta as `root`. Use a normal user or the service; the built-in database refuses
> to run as root.

## Boards and small servers

- Keep Docveta's data folder on an **SSD, NVMe or eMMC**, not an SD card: SD cards are slow for
  databases and wear out.
- 2 GB of RAM is enough for Docveta and its database; text recognition needs about 1 GB more
  while it works.
