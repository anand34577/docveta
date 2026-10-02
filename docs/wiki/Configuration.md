# Configuration

Docveta works without any configuration: it listens on port 8080, keeps data in a `data` folder
and asks for the database in the browser. Change settings in a file called **`docveta.conf`**,
one `NAME=value` per line:

```ini
# docveta.conf
DOCVETA_LISTEN=:9000
DOCVETA_BASE_URL=https://docs.example.com
DOCVETA_OCR_DEVICE=gpu
```

Restart Docveta after changing it (Windows service: `sc stop Docveta` / `sc start Docveta`; Linux:
`sudo systemctl restart docveta`; macOS: run `install.sh` again).

## Where Docveta looks

1. **Environment variables** win over everything (useful in Docker and systemd).
2. **`docveta.conf` next to the program** (or the file named in `DOCVETA_CONFIG`). The installers
   create it with the data folder, port and OCR device:
   - Windows installer: `C:\Program Files\Docveta\docveta.conf`
   - Linux `install.sh`: `/opt/docveta/docveta.conf`
   - macOS `install.sh`: `~/Applications/Docveta/docveta.conf`
3. **`docveta.conf` in the data folder**, which Docveta writes itself: the database connection
   from the setup page and the generated secret key. It contains passwords, so keep it
   private and include it in backups.

## Settings

### Basics

| Setting | Default | Meaning |
|---|---|---|
| `DOCVETA_LISTEN` | `:8080` | Address and port to listen on. `:8080` = all network interfaces; `127.0.0.1:8080` = this computer only |
| `DOCVETA_BASE_URL` | `http://localhost:8080` | The address people type to open Docveta. Used in email links, for single sign-on, and to decide on secure cookies (`https://…`) |
| `DOCVETA_DATA_DIR` | `./data` | Folder for documents (`blobs/`), temporary uploads, logs and the data `docveta.conf` |
| `DOCVETA_DATABASE_URL` | set by the setup page | PostgreSQL connection, `postgres://user:password@host:5432/docveta?sslmode=prefer`. See [Database](Database) |
| `DOCVETA_SECRET_KEY` | generated | Encrypts stored passwords (email, single sign-on). At least 32 characters. **If you lose it, those passwords must be entered again** |

### Text recognition

| Setting | Default | Meaning |
|---|---|---|
| `DOCVETA_OCR_DEVICE` | `auto` | `auto`, `gpu`, `igpu`, `npu` or `cpu`. See [OCR engines](OCR-engines) |
| `DOCVETA_OCR_GPU_ID` | — | Use a specific graphics adapter by number (`docveta-ocr --list-devices`) |
| `DOCVETA_LOCAL_OCR` | `auto` | `auto` starts the engine in the `ocr` folder next to Docveta; `off` doesn't; or a path to an engine program |
| `DOCVETA_MODELS_DIR` | `ocr/models` | Where the bundled engine finds its models |

### Network and security

| Setting | Default | Meaning |
|---|---|---|
| `DOCVETA_TRUSTED_PROXIES` | — | Comma-separated IPs/networks of your reverse proxy, so Docveta sees real client addresses (e.g. `127.0.0.1,172.16.0.0/12`) |
| `DOCVETA_ALLOW_LOCAL_TARGETS` | `false` | Allow notification channels (Gotify, ntfy, webhooks) to reach addresses on your local network. Off by default so users can't use Docveta to probe your network; turn on if your Gotify/ntfy runs at home |
| `DOCVETA_SESSION_IDLE` | `720h` | Sign out after this long without activity (30 days) |
| `DOCVETA_SESSION_MAX` | `2160h` | Sign out after this long at the latest (90 days) |

### Limits and performance

| Setting | Default | Meaning |
|---|---|---|
| `DOCVETA_MAX_UPLOAD_MB` | `500` | Largest file accepted |
| `DOCVETA_TRASH_RETENTION` | `720h` | How long deleted documents stay in the trash (30 days) |
| `DOCVETA_PDF_WORKERS` | `2` | PDFs inspected in parallel (each needs 50–100 MB of memory) |
| `DOCVETA_JOB_WORKERS` | `4` | Background jobs in parallel |

### Logging and starting

| Setting | Default | Meaning |
|---|---|---|
| `DOCVETA_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `DOCVETA_LOG_FORMAT` | `text` in a terminal, `json` otherwise | `text` is easier to read, `json` suits log collectors |
| `DOCVETA_OPEN_BROWSER` | `true` | On Windows and macOS, open the browser when Docveta is started by hand |
| `DOCVETA_CONFIG` | `docveta.conf` next to the program | Path to the main settings file |

Durations use Go's format: `30m`, `12h`, `720h`.

## Settings in the app

Everything else is changed in Docveta itself, by an administrator:

- *Administration → Single sign-on*: OpenID Connect (Authentik, Keycloak, Google, …)
- *Administration → Email*: SMTP server for email notifications
- *Administration → Processing*: OCR engines, routing, retries
- *Administration → Users*, *Settings → Notifications*, *Space settings*

## Check your setup

```bash
docveta doctor                  # configuration, database, disk, OCR engines, stored files
docveta doctor --verify-blobs   # also re-checks every stored file for damage (slow)
```

Run it from the same folder or environment Docveta uses (Windows service:
`"C:\Program Files\Docveta\docveta.exe" doctor` in an administrator prompt; Linux service:
`sudo -u docveta DOCVETA_CONFIG=/opt/docveta/docveta.conf /opt/docveta/docveta doctor`).
