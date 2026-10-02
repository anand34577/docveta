# Troubleshooting

Start with the **log** and the **health check**:

| Installed with | Log | Health check |
|---|---|---|
| Windows installer | `C:\ProgramData\Docveta\logs\docveta.log` (Start menu → *Docveta log*) | admin prompt: `"C:\Program Files\Docveta\docveta.exe" doctor` |
| Windows portable | the Docveta window | `docveta.exe doctor` in the folder |
| Linux service | `journalctl -u docveta -f` | `sudo -u docveta DOCVETA_CONFIG=/opt/docveta/docveta.conf /opt/docveta/docveta doctor` |
| macOS | `~/Library/Logs/Docveta/docveta.log` | `~/Applications/Docveta/docveta doctor` |
| Docker | `docker compose logs -f docveta` | `docker compose exec docveta /docveta doctor` |

`docveta doctor` checks the settings, database, schema, clock, disk space, the secret key, OCR
engines and stored files, and says what to fix.

## Docveta doesn't open in the browser

- **Is it running?** Windows: *Services* → "Docveta Document Manager" → *Running*. Linux:
  `systemctl status docveta`.
- **Right address and port?** It's `DOCVETA_LISTEN` in `docveta.conf` (default 8080).
- **Port already in use?** The log says `address already in use`. Choose another port:
  `DOCVETA_LISTEN=:9000` and `DOCVETA_BASE_URL=http://localhost:9000`.
- **From another device?** Allow the port in the firewall (Windows: see
  [Install on Windows](Install-on-Windows#using-docveta-from-other-devices); Linux:
  `sudo ufw allow 8080/tcp`), and check `DOCVETA_LISTEN` isn't `127.0.0.1:…`.

## The setup page

| Message | Fix |
|---|---|
| *Nothing is answering at that server and port* | PostgreSQL isn't running or uses another port. Windows: *Services* → `postgresql-x64-17` → Start |
| *The user name or password is wrong* | Use the password you set when installing PostgreSQL, for user `postgres` |
| *The server refused this connection* | PostgreSQL doesn't allow connections from this computer: edit `pg_hba.conf` (see [Database](Database#a-database-on-another-computer)) |
| *…isn't allowed to set up the database* | Make the user the database owner: `ALTER DATABASE docveta OWNER TO docveta;` |
| *extensions pg_trgm and citext aren't installed* | Install the PostgreSQL contrib package (`postgresql-contrib`) |
| *Docveta needs version 16 or newer* | Upgrade PostgreSQL ([Database](Database)) |
| *The setup code is wrong* | It's in the log (`setup_code_for_other_computers`) and in `setup-code.txt` in the data folder; or open the page on the Docveta computer itself (`http://localhost:8080`), which needs no code |

## "waiting for the database" in the log

Docveta started but can't reach PostgreSQL. It keeps retrying, so start PostgreSQL and Docveta
continues by itself. If the database moved or the password changed, fix
`DOCVETA_DATABASE_URL` in `docveta.conf` **in the data folder**, or delete that line and restart
Docveta to get the setup page again.

## Text isn't recognised (documents stay "Processing")

1. *Administration → Processing*: is an engine listed and **online**?
   - No engine: unzip the `docveta-ocr` package next to Docveta (an `ocr` folder) and restart, or
     add an [engine on another computer](OCR-engines#an-engine-on-another-computer).
   - `local-ocr` offline: look for `component=local-ocr` lines in the log.
2. Run the engine's self-test: `ocr/docveta-ocr --self-test` (Windows: `ocr\docveta-ocr.exe --self-test`).
3. *Failed* tasks have a **Retry** button; the error is shown next to them.

## The GPU isn't used

- `ocr/docveta-ocr --list-devices` shows what ONNX Runtime can use.
- **Windows**: update the graphics driver (Windows Update or the vendor). Check
  `DOCVETA_OCR_DEVICE` isn't `cpu`. With several GPUs, set `DOCVETA_OCR_GPU_ID`.
- **Linux**: the standard download runs on the CPU; GPUs need the setup in
  [OCR engines](OCR-engines#linux-nvidia-intel-and-amd).
- The log line `OCR on …` at start-up names the device actually used; a warning explains
  why it fell back to the CPU.

## Windows: SmartScreen or antivirus blocks Docveta

Docveta isn't code-signed yet. SmartScreen: *More info* → *Run anyway*. Antivirus: allow
`C:\Program Files\Docveta`. Verify the download with `Get-FileHash` against `SHA256SUMS` on the
release page.

## macOS: "cannot be opened because Apple cannot check it"

See [Install on macOS](Install-on-macOS#docveta-cant-be-opened-because-apple-cannot-check-it).

## Forgot the administrator password

Set a new one from the command line (it also signs out all sessions):

```bash
# Linux service
sudo -u docveta DOCVETA_CONFIG=/opt/docveta/docveta.conf DOCVETA_PASSWORD='new-long-password' /opt/docveta/docveta user reset-password --email you@example.com
# Docker
docker compose exec -e DOCVETA_PASSWORD='new-long-password' docveta /docveta user reset-password --email you@example.com
```

```bat
:: Windows (administrator prompt)
set DOCVETA_PASSWORD=new-long-password
"C:\Program Files\Docveta\docveta.exe" user reset-password --email you@example.com
```

## Still stuck?

Open an [issue](https://github.com/anand34577/docveta/issues) with the output of
`docveta doctor`, your system and Docveta version (`docveta version`), and the relevant log lines.
Remove passwords from what you paste.
