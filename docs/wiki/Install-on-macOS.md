# Install on macOS

macOS 12 or newer, Apple silicon (M1–M4) or Intel. Nothing else to install: Docveta brings its
own database.

| Your Mac | Download |
|---|---|
| Apple silicon (M1, M2, M3, M4) | `docveta-<version>-darwin-arm64.tar.gz` + `docveta-ocr-<version>-darwin-arm64.zip` |
| Intel | `docveta-<version>-darwin-x64.tar.gz` (text recognition: use the [Docker OCR engine](Install-with-Docker#text-recognition) or a worker on another machine) |

Not sure? Apple menu → *About This Mac*: "Chip: Apple M…" means Apple silicon.

## Install (starts when you log in)

In Terminal:

```bash
cd ~/Downloads
tar xzf docveta-*-darwin-arm64.tar.gz
cd docveta-*-darwin-arm64
unzip ../docveta-ocr-*-darwin-arm64.zip     # optional: text recognition, creates ocr/
./install.sh                              # or: ./install.sh --port 9000
```

Docveta is copied to `~/Applications/Docveta` and starts now and whenever you log in. Your
browser opens <http://localhost:8080>: click **Save and start** to use the built-in database (or
[connect your own PostgreSQL](Database#connect-docveta-the-setup-page)) and create your account.

| What | Where |
|---|---|
| Program and settings (`docveta.conf`) | `~/Applications/Docveta` |
| Your documents and data | `~/Library/Application Support/Docveta` |
| Log | `~/Library/Logs/Docveta/docveta.log` |

After changing `docveta.conf`, run `./install.sh` again to restart Docveta. Remove Docveta with
`./install.sh --uninstall` (your data is kept).

Text recognition uses the Apple GPU through Core ML; set `DOCVETA_OCR_DEVICE=npu` in
`docveta.conf` to use the Neural Engine instead. See [OCR engines](OCR-engines).

## Just run it

```bash
./docveta
```

## "Docveta can't be opened because Apple cannot check it"

Docveta isn't notarised by Apple yet. `install.sh` removes the download quarantine for you.
If you start `./docveta` directly and macOS blocks it:

```bash
xattr -dr com.apple.quarantine .
```

or allow it in *System Settings → Privacy & Security* → *Open Anyway*.
