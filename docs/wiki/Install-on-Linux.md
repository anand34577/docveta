# Install on Linux

Docveta is a single program with no dependencies: it runs on any Linux distribution, on PCs,
servers and boards (Raspberry Pi 4/5, Rockchip RK3588/RK3576/RK3566, Orange Pi, Ampere).
First [install PostgreSQL](Database#ubuntu-debian-raspberry-pi-os).

| Your processor | Download |
|---|---|
| Intel / AMD 64-bit | `docveta-<version>-linux-x64.tar.gz` |
| ARM 64-bit (Raspberry Pi 4/5 with 64-bit OS, Rockchip, Ampere, AWS Graviton) | `…-linux-arm64.tar.gz` |
| ARM 32-bit (Raspberry Pi 2/3 with 32-bit OS) | `…-linux-armv7.tar.gz` |
| Old 32-bit PCs | `…-linux-x86.tar.gz` |

Not sure? Run `uname -m`: `x86_64` → x64, `aarch64` → arm64, `armv7l` → armv7.

## Install as a service (recommended)

```bash
VERSION=0.2.0          # the latest release
ARCH=x64               # or arm64, armv7, x86
curl -LO https://github.com/anand34577/docveta/releases/download/v$VERSION/docveta-$VERSION-linux-$ARCH.tar.gz
tar xzf docveta-$VERSION-linux-$ARCH.tar.gz
cd docveta-$VERSION-linux-$ARCH

# optional: text recognition on this machine (x64 and arm64)
curl -LO https://github.com/anand34577/docveta/releases/download/v$VERSION/docveta-ocr-$VERSION-linux-$ARCH.zip
unzip docveta-ocr-$VERSION-linux-$ARCH.zip      # creates ocr/

sudo ./install.sh                  # or: sudo ./install.sh --port 9000
```

`install.sh`:

- creates a system user `docveta`
- copies the program to `/opt/docveta` (with `ocr/` if present)
- keeps data in `/var/lib/docveta`
- writes settings to `/opt/docveta/docveta.conf`
- installs and starts the systemd service `docveta`

Running it again upgrades Docveta and keeps your data and settings.

Then open `http://<server>:8080` in a browser. Because you're on another computer, the setup
page asks for a code:

```bash
sudo cat /var/lib/docveta/setup-code.txt
```

Everyday commands:

```bash
sudo systemctl status docveta
sudo systemctl restart docveta       # after editing /opt/docveta/docveta.conf
journalctl -u docveta -f             # live log
sudo ./install.sh --uninstall      # remove (keeps /var/lib/docveta and the database)
```

## Just run it

```bash
./docveta
```

Docveta starts on port 8080 and keeps its data in `./data`. Stop it with Ctrl+C. Put settings in
`docveta.conf` next to the program or pass them as environment variables:

```bash
DOCVETA_LISTEN=:9000 DOCVETA_DATA_DIR=/srv/docveta ./docveta
```

> Don't run Docveta as `root`. Use a normal user or the service above.

## Set up systemd by hand

Use the `docveta.service` file from the download as a template: it runs `/opt/docveta/docveta` as
user `docveta`, reads `/opt/docveta/docveta.conf` (`DOCVETA_CONFIG`), may only write
`/var/lib/docveta`, and joins the `video` and `render` groups so the OCR engine can use the GPU.

## Boards and small servers

- Put the PostgreSQL data and Docveta's data folder on an **SSD, NVMe or eMMC**, not an SD
  card: SD cards are slow for databases and wear out.
- **Rockchip NPU** (RK3588/RK3576/RK3566): use the dedicated NPU worker, which is much faster
  there than the CPU. See [OCR engines](OCR-engines#rockchip-npu).
- 2 GB of RAM is enough for Docveta and PostgreSQL; the OCR engine needs about 1 GB more
  while it works.
