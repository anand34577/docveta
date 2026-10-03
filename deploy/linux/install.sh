#!/bin/sh
# Installs (or upgrades) Docveta as a service (systemd, or OpenRC on Alpine). Run from the
# unpacked release folder:
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
if command -v systemctl >/dev/null && [ -d /run/systemd/system ]; then INIT=systemd
elif command -v rc-service >/dev/null; then INIT=openrc
else echo "Neither systemd nor OpenRC found: run ./docveta serve yourself (see README.txt)." >&2; exit 1; fi
HERE=$(cd "$(dirname "$0")" && pwd)

svc() { # svc start|stop|restart|enable|disable
  if [ "$INIT" = systemd ]; then
    case $1 in enable) systemctl enable --now docveta ;; disable) systemctl disable --now docveta ;; *) systemctl "$1" docveta ;; esac
  else
    case $1 in enable) rc-update add docveta default >/dev/null; rc-service docveta start ;;
               disable) rc-service docveta stop; rc-update del docveta default ;; *) rc-service docveta "$1" ;; esac
  fi
}

if [ "$UNINSTALL" -eq 1 ]; then
  svc disable 2>/dev/null || true
  rm -f /etc/systemd/system/docveta.service /etc/init.d/docveta
  if [ "$INIT" = systemd ]; then systemctl daemon-reload; fi
  rm -rf /opt/docveta
  echo "Docveta removed. Your data is still in /var/lib/docveta (and in PostgreSQL, if you used your own)."
  exit 0
fi

if ! id docveta >/dev/null 2>&1; then
  if command -v useradd >/dev/null; then useradd --system --home-dir /var/lib/docveta --shell /usr/sbin/nologin docveta
  else addgroup -S docveta && adduser -S -D -H -h /var/lib/docveta -s /sbin/nologin -G docveta docveta; fi # BusyBox (Alpine)
fi
for g in video render; do
  if getent group "$g" >/dev/null; then usermod -aG "$g" docveta 2>/dev/null || addgroup docveta "$g" 2>/dev/null || true; fi
done
install -d -o docveta -g docveta -m 750 /var/lib/docveta

svc stop 2>/dev/null || true
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
if [ "$INIT" = systemd ]; then
  install -m 644 "$HERE/docveta.service" /etc/systemd/system/docveta.service
  systemctl daemon-reload
else
  cat > /etc/init.d/docveta <<'INIT'
#!/sbin/openrc-run
name="Docveta"
description="Docveta document manager"
command="/opt/docveta/docveta"
command_args="serve"
command_user="docveta:docveta"
directory="/var/lib/docveta"
supervisor="supervise-daemon"
output_log="/var/log/docveta.log"
error_log="/var/log/docveta.log"
export DOCVETA_CONFIG=/opt/docveta/docveta.conf
depend() { need net; after postgresql; }
INIT
  chmod 755 /etc/init.d/docveta
  touch /var/log/docveta.log && chown docveta:docveta /var/log/docveta.log
fi
svc enable

PORT_NOW=$(sed -n 's/^DOCVETA_LISTEN=.*:\([0-9]*\)$/\1/p' /opt/docveta/docveta.conf)
echo
echo "Docveta is running. Open http://$(hostname -I 2>/dev/null | awk '{print $1}'):${PORT_NOW:-$PORT} in a browser to connect the database."
echo "From another computer the setup page asks for a code; show it with:"
echo "    sudo cat /var/lib/docveta/setup-code.txt"
if [ "$INIT" = systemd ]; then echo "Logs: journalctl -u docveta -f"; else echo "Logs: tail -f /var/log/docveta.log"; fi
