#!/bin/sh
# Installs or upgrades Docveta on Linux in one step: the server as a service, plus text
# recognition for this machine (Allwinner A733 NPU, or the processor).
#
#   curl -fsSLO https://github.com/anand34577/docveta/releases/latest/download/get-docveta.sh
#   sudo sh get-docveta.sh
#
# Then open the address it prints and click "Save and start" (built-in database).
#
# On a Proxmox host, first give a container the NPU and the fast cores:
#   sudo sh get-docveta.sh --proxmox <CTID>
#
# Options: --port N (default 8080) --version X.Y.Z (default: latest) --no-ocr
#          --uninstall (removes the program; your documents and settings are kept)
# Every download is checked against the release's SHA256SUMS. Running it again upgrades:
# everything is downloaded first, Docveta is stopped only while its files are swapped, and
# it is started again however the script ends.
set -eu

REPO=anand34577/docveta
PORT=8080
VERSION=
OCR=1
PROXMOX=
UNINSTALL=0
STOPPED=0
# Allwinner's VIPLite 2.0 libraries (not redistributable): fetched from Radxa's ai-sdk,
# pinned to a commit and checked against these SHA-256 sums.
VIPLITE_URL=https://raw.githubusercontent.com/ZIFENG278/ai-sdk/fc90006d0f6569da2f6726c2d8395877686f5aca/viplite-tina/lib/aarch64-none-linux-gnu/v2.0
VIPLITE_SUMS="82f049b0ed0065dd4d443e37eeb1edfcbef24457c9c24e36170d64d5b748ca66  libNBGlinker.so
3ed5357b26bd6c4fb68fbdc0b21d637227dda711d1fe67b9e28accdc48bb11f2  libVIPhal.so"

say() { printf '\033[1m%s\033[0m\n' "$*"; }
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --version) VERSION="${2#v}"; shift 2 ;;
    --no-ocr) OCR=0; shift ;;
    --proxmox) PROXMOX="$2"; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done
[ "$(id -u)" -eq 0 ] || die "please run with sudo"

# ------------------------------------------------------------------ Proxmox host mode
if [ -n "$PROXMOX" ]; then
  command -v pct >/dev/null || die "--proxmox runs on the Proxmox host (pct not found)"
  conf=/etc/pve/lxc/$PROXMOX.conf
  [ -f "$conf" ] || die "container $PROXMOX not found"
  if [ -e /dev/vipcore ]; then
    if grep -q vipcore "$conf"; then
      echo "Container $PROXMOX already has /dev/vipcore."
    else
      i=0; while grep -q "^dev$i:" "$conf"; do i=$((i + 1)); done
      pct set "$PROXMOX" -dev$i /dev/vipcore,mode=0666
      echo "Gave container $PROXMOX the NPU (/dev/vipcore)."
    fi
  else
    echo "No /dev/vipcore on this host: no Allwinner NPU driver (text recognition will use the processor)."
  fi
  # Big cores: the CPUs with the highest maximum clock (on the A733: the two Cortex-A76).
  max=0; big=
  for c in /sys/devices/system/cpu/cpu[0-9]*; do
    f=$(cat "$c/cpufreq/cpuinfo_max_freq" 2>/dev/null || echo 0)
    n=${c##*cpu}
    if [ "$f" -gt "$max" ]; then max=$f; big=$n; elif [ "$f" -eq "$max" ]; then big="$big,$n"; fi
  done
  total=$(ls -d /sys/devices/system/cpu/cpu[0-9]* | wc -l)
  if [ -n "$big" ] && [ "$(echo "$big" | tr ',' '\n' | wc -l)" -lt "$total" ] && ! grep -q '^lxc.cgroup2.cpuset.cpus' "$conf"; then
    echo "lxc.cgroup2.cpuset.cpus: $big" >> "$conf"
    echo "Pinned container $PROXMOX to the fast cores ($big)."
  fi
  if pct status "$PROXMOX" | grep -q running; then pct reboot "$PROXMOX"; echo "Restarted container $PROXMOX."; fi
  say "Done. Now run this script inside container $PROXMOX."
  exit 0
fi

# ------------------------------------------------------------------ install
if command -v systemctl >/dev/null && [ -d /run/systemd/system ]; then
  start() { systemctl start docveta; }
  stop() { systemctl stop docveta 2>/dev/null || true; }
  remove_service() { systemctl disable --now docveta 2>/dev/null || true; rm -f /etc/systemd/system/docveta.service; systemctl daemon-reload; }
  logs() { journalctl -u docveta -n 40 --no-pager; }
elif command -v rc-service >/dev/null; then # Alpine (OpenRC)
  start() { rc-service docveta start >/dev/null; }
  stop() { rc-service docveta stop >/dev/null 2>&1 || true; }
  remove_service() { stop; rc-update del docveta default >/dev/null 2>&1 || true; rm -f /etc/init.d/docveta; }
  logs() { tail -n 40 /var/log/docveta.log; }
else die "no systemd or OpenRC: download the release yourself and run ./docveta serve (see the README)"; fi
W=/opt/docveta-worker-allwinner

if [ "$UNINSTALL" = 1 ]; then
  say "Removing Docveta"
  remove_service
  pkill -x docveta-worker-allwinner 2>/dev/null || true
  rm -rf /opt/docveta "$W" "$W.old"
  echo "Docveta removed. Your documents and settings are still in /var/lib/docveta (and in your own PostgreSQL, if you used one)."
  echo "Installing again picks them up. To delete them too: sudo rm -rf /var/lib/docveta"
  exit 0
fi

command -v curl >/dev/null || die "curl is required (apt install curl / apk add curl)"
command -v sha256sum >/dev/null || die "sha256sum is required (coreutils)"
MUSL=0; ls /lib/ld-musl-*.so.1 >/dev/null 2>&1 && MUSL=1
case "$(uname -m)" in
  x86_64) ARCH=x64 ;; aarch64|arm64) ARCH=arm64 ;; armv7l|armv7*) ARCH=armv7 ;; i?86) ARCH=x86 ;;
  *) die "unsupported processor: $(uname -m)" ;;
esac
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSI "https://github.com/$REPO/releases/latest" | tr -d '\r' | sed -n 's#^[Ll]ocation: .*/tag/v\(.*\)$#\1#p')
  [ -n "$VERSION" ] || die "couldn't find the latest version; pass --version"
fi
BASE="https://github.com/$REPO/releases/download/v$VERSION"
TMP=$(mktemp -d)
# However the script ends (an error, Ctrl-C), Docveta isn't left stopped.
trap 'if [ "$STOPPED" = 1 ]; then echo "Starting Docveta again"; start || true; fi; rm -rf "$TMP"' EXIT
trap 'exit 1' INT TERM HUP
curl -fsSL -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS" || die "can't download version $VERSION"

# fetch FILE: downloads a release file into $TMP and checks it against SHA256SUMS.
fetch() {
  echo "  downloading $1"
  curl -fL --retry 3 -s -o "$TMP/$1" "$BASE/$1" || die "download failed: $1"
  (cd "$TMP" && grep " \*\?$1\$" SHA256SUMS | sed 's/ \*/  /' | sha256sum -c --status) || die "checksum mismatch: $1"
}
unzip_tool() {
  command -v unzip >/dev/null && return
  if command -v apt-get >/dev/null; then apt-get install -y -qq unzip >/dev/null
  elif command -v dnf >/dev/null; then dnf install -y -q unzip
  elif command -v zypper >/dev/null; then zypper -q install -y unzip
  elif command -v pacman >/dev/null; then pacman -S --noconfirm --needed unzip >/dev/null
  else die "please install unzip"; fi
}

say "Docveta $VERSION for linux-$ARCH"
# Everything is downloaded and checked before anything here changes: a failed download leaves
# the running Docveta as it was.
fetch "docveta-$VERSION-linux-$ARCH.tar.gz"
tar -C "$TMP" -xzf "$TMP/docveta-$VERSION-linux-$ARCH.tar.gz"
SRC="$TMP/docveta-$VERSION-linux-$ARCH"

ENGINE=none
if [ "$OCR" = 1 ]; then
  if [ "$MUSL" = 1 ]; then echo "  (the text recognition packages need glibc, not available on Alpine: use Docker for OCR, or born-digital PDFs only)"
  elif [ -e /dev/vipcore ] && [ "$ARCH" = arm64 ]; then ENGINE=allwinner
  elif [ "$ARCH" = x64 ] || [ "$ARCH" = arm64 ]; then ENGINE=cpu
  else echo "  (no text recognition package for linux-$ARCH: scans won't be searchable)"; fi
fi

if [ "$ENGINE" = cpu ]; then
  unzip_tool
  fetch "docveta-ocr-$VERSION-linux-$ARCH.zip"
  unzip -q "$TMP/docveta-ocr-$VERSION-linux-$ARCH.zip" -d "$SRC" # -> $SRC/ocr, installed next to docveta
elif [ "$ENGINE" = allwinner ]; then
  fetch "docveta-worker-allwinner-$VERSION-linux-arm64.tar.gz"
  tar -C "$TMP" -xzf "$TMP/docveta-worker-allwinner-$VERSION-linux-arm64.tar.gz" # -> $TMP/docveta-worker-allwinner
  for lib in libNBGlinker.so libVIPhal.so; do
    curl -fsSL --retry 3 -o "$TMP/$lib" "$VIPLITE_URL/$lib" || die "download failed: $lib"
  done
  (cd "$TMP" && echo "$VIPLITE_SUMS" | sha256sum -c --status) || die "checksum mismatch: VIPLite libraries"
  install -d "$TMP/docveta-worker-allwinner/viplite"
  install -m 644 "$TMP/libNBGlinker.so" "$TMP/libVIPhal.so" "$TMP/docveta-worker-allwinner/viplite/"
fi

# Stopped from here to the end: a running engine reads its own files while it works (they
# mustn't change under it), and the NPU check needs the NPU to itself.
say "Installing"
STOPPED=1
stop
pkill -x docveta-worker-allwinner 2>/dev/null || true
if grep -q -- --no-start "$SRC/install.sh"; then "$SRC/install.sh" --port "$PORT" --no-start >/dev/null
else "$SRC/install.sh" --port "$PORT" >/dev/null; stop; fi # releases before 0.17.0 start it themselves
CONF=/opt/docveta/docveta.conf
setconf() { # setconf KEY VALUE
  if grep -q "^$1=" "$CONF"; then sed -i "s|^$1=.*|$1=$2|" "$CONF"; else echo "$1=$2" >> "$CONF"; fi
}

if [ "$ENGINE" = allwinner ]; then
  say "Text recognition on the Allwinner NPU"
  # Swapped as a whole, so nothing of an older version is left; the previous one is kept until
  # the new one passes its check.
  rm -rf "$W.old"
  if [ -d "$W" ]; then mv "$W" "$W.old"; fi
  mv "$TMP/docveta-worker-allwinner" "$W"
  if timeout -s KILL 180 "$W/docveta-worker-allwinner" --probe >"$TMP/probe.log" 2>&1; then
    grep -E "Detection|Test image|^  " "$TMP/probe.log" | sed 's/^/  /' || true
    rm -rf "$W.old"
  else
    sed 's/^/  /' "$TMP/probe.log"
    if [ -d "$W.old" ]; then
      rm -rf "$W"
      mv "$W.old" "$W"
      echo "  The new NPU engine failed its check (above), so the previous one is kept."
    else
      echo "  The NPU engine failed its check (above): scans won't be read until it's fixed (see the OCR engines wiki page)."
    fi
  fi
  # Docveta starts the engine itself and gives it a token: no worker service or token needed.
  setconf DOCVETA_LOCAL_OCR "$W/docveta-worker-allwinner"
  # Older manual setups ran it as a separate service.
  if command -v systemctl >/dev/null && systemctl is-enabled docveta-worker >/dev/null 2>&1; then systemctl disable --now docveta-worker >/dev/null 2>&1; fi
  su -s /bin/sh docveta -c 'test -r /dev/vipcore -a -w /dev/vipcore' 2>/dev/null \
    || echo "  Note: the docveta user can't open /dev/vipcore; allow it (e.g. chmod 666 /dev/vipcore or a udev rule)."
elif [ "$ENGINE" = cpu ]; then
  setconf DOCVETA_LOCAL_OCR auto
fi

say "Starting Docveta"
start
STOPPED=0
# A new version updates its database on its first start: wait until it answers.
LISTEN_PORT=$(sed -n 's/^DOCVETA_LISTEN=.*:\([0-9]*\)$/\1/p' "$CONF")
LISTEN_PORT=${LISTEN_PORT:-$PORT}
i=0
until curl -fs -o /dev/null "http://127.0.0.1:$LISTEN_PORT/healthz"; do
  i=$((i + 1))
  [ "$i" -lt 180 ] || { logs; die "Docveta didn't answer within 3 minutes (its log is above)"; }
  sleep 1
done

IP=$(hostname -I 2>/dev/null | awk '{print $1}')
say "Docveta $VERSION is running: http://${IP:-localhost}:$LISTEN_PORT"
if grep -q '^DOCVETA_DATABASE_URL=' "$CONF" /var/lib/docveta/docveta.conf 2>/dev/null; then
  echo "Upgrade complete; your settings and data are unchanged."
else
  echo "Open that address and click \"Save and start\" to use the built-in database, then create your account."
  echo "From another computer the page asks for a setup code: sudo cat /var/lib/docveta/setup-code.txt"
fi
if command -v journalctl >/dev/null; then echo "Logs: journalctl -u docveta -f"; else echo "Logs: tail -f /var/log/docveta.log"; fi
echo "Upgrade: run this script again. Remove: sudo sh get-docveta.sh --uninstall (your data is kept)."
