#!/bin/sh
# Installs (or upgrades) Docveta for your macOS user: it starts when you log in.
#
#   ./install.sh                # port 8080
#   ./install.sh --port 9000
#   ./install.sh --uninstall
#
# Program: ~/Applications/Docveta   Data: ~/Library/Application Support/Docveta
# Logs: ~/Library/Logs/Docveta/docveta.log
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

HERE=$(cd "$(dirname "$0")" && pwd)
APP="$HOME/Applications/Docveta"
DATA="$HOME/Library/Application Support/Docveta"
LOGS="$HOME/Library/Logs/Docveta"
PLIST="$HOME/Library/LaunchAgents/in.docveta.server.plist"

launchctl bootout "gui/$(id -u)" "$PLIST" 2>/dev/null || true
if [ "$UNINSTALL" -eq 1 ]; then
  rm -f "$PLIST"
  rm -rf "$APP"
  echo "Docveta removed. Your data is still in \"$DATA\" and in PostgreSQL."
  exit 0
fi

mkdir -p "$APP" "$DATA" "$LOGS" "$HOME/Library/LaunchAgents"
cp "$HERE/docveta" "$APP/docveta"
chmod 755 "$APP/docveta"
[ -d "$HERE/ocr" ] && { rm -rf "$APP/ocr"; cp -R "$HERE/ocr" "$APP/ocr"; }
# Downloaded files are quarantined by Gatekeeper; Docveta isn't notarised yet.
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true

if [ ! -f "$APP/docveta.conf" ]; then
  cat > "$APP/docveta.conf" <<EOF
# Docveta settings. After changes run: ./install.sh again (or log out and in)
DOCVETA_DATA_DIR=$DATA
DOCVETA_LISTEN=:$PORT
DOCVETA_BASE_URL=http://localhost:$PORT
DOCVETA_OCR_DEVICE=auto
DOCVETA_LOG_FORMAT=text
EOF
fi

cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>in.docveta.server</string>
  <key>ProgramArguments</key><array><string>$APP/docveta</string><string>serve</string></array>
  <key>EnvironmentVariables</key><dict><key>DOCVETA_CONFIG</key><string>$APP/docveta.conf</string></dict>
  <key>WorkingDirectory</key><string>$DATA</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardOutPath</key><string>$LOGS/docveta.log</string>
  <key>StandardErrorPath</key><string>$LOGS/docveta.log</string>
</dict>
</plist>
EOF
launchctl bootstrap "gui/$(id -u)" "$PLIST"

PORT_NOW=$(sed -n 's/^DOCVETA_LISTEN=.*:\([0-9]*\)$/\1/p' "$APP/docveta.conf")
URL="http://localhost:${PORT_NOW:-$PORT}"
echo "Docveta is running and starts when you log in. Opening $URL ..."
sleep 2
open "$URL"
