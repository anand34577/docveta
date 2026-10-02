#!/bin/sh
# Installs (or upgrades) Docveta as a systemd service. Run from the unpacked release folder:
#
#   sudo ./install.sh              # port 8080
#   sudo ./install.sh --port 9000
#
# Program: /opt/docveta (docveta, ocr/ if present)   Data: /var/lib/docveta   Settings: /opt/docveta/docveta.conf
# Upgrades keep the data and settings. Remove with: sudo ./install.sh --uninstall
set -eu

PORT=8080
UNINSTALL=0
while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "Please run with sudo." >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd not found: run ./docveta serve yourself (see README.txt)." >&2; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)

if [ "$UNINSTALL" -eq 1 ]; then
  systemctl disable --now docveta 2>/dev/null || true
  rm -f /etc/systemd/system/docveta.service
  systemctl daemon-reload
  rm -rf /opt/docveta
  echo "Docveta removed. Your data is still in /var/lib/docveta and in PostgreSQL."
  exit 0
fi

id docveta >/dev/null 2>&1 || useradd --system --home-dir /var/lib/docveta --shell /usr/sbin/nologin docveta
for g in video render; do getent group "$g" >/dev/null && usermod -aG "$g" docveta || true; done
install -d -o docveta -g docveta -m 750 /var/lib/docveta

systemctl stop docveta 2>/dev/null || true
install -d /opt/docveta
install -m 755 "$HERE/docveta" /opt/docveta/docveta
if [ -d "$HERE/ocr" ]; then
  rm -rf /opt/docveta/ocr
  cp -r "$HERE/ocr" /opt/docveta/ocr
  chmod 755 /opt/docveta/ocr/docveta-ocr
fi
if [ ! -f /opt/docveta/docveta.conf ]; then
  cat > /opt/docveta/docveta.conf <<EOF
# Docveta settings. After changes: sudo systemctl restart docveta
# All options: https://github.com/anand34577/docveta/wiki/Configuration
DOCVETA_DATA_DIR=/var/lib/docveta
DOCVETA_LISTEN=:$PORT
DOCVETA_BASE_URL=http://$(hostname -f 2>/dev/null || hostname):$PORT
DOCVETA_OCR_DEVICE=auto
EOF
fi
install -m 644 "$HERE/docveta.service" /etc/systemd/system/docveta.service
systemctl daemon-reload
systemctl enable --now docveta

PORT_NOW=$(sed -n 's/^DOCVETA_LISTEN=.*:\([0-9]*\)$/\1/p' /opt/docveta/docveta.conf)
echo
echo "Docveta is running. Open http://$(hostname -I 2>/dev/null | awk '{print $1}'):${PORT_NOW:-$PORT} in a browser to connect the database."
echo "From another computer the setup page asks for a code; show it with:"
echo "    sudo cat /var/lib/docveta/setup-code.txt"
echo "Logs: journalctl -u docveta -f"
