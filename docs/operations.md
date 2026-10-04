> **The user guide now lives in the [wiki](https://github.com/anand34577/docveta/wiki)** (source: [docs/wiki](wiki/)).
> This page keeps the short operator reference.

# Operating Docveta

## Configuration

Bootstrap settings come from environment variables; everything else is configured in the
web UI (*Administration*).

| Variable | Default | Meaning |
|---|---|---|
| `DOCVETA_DATABASE_URL` | — (required) | `postgres://user:pass@host:5432/db` |
| `DOCVETA_SECRET_KEY` | — (required, ≥32 chars) | Encrypts stored secrets (SMTP, OIDC). **Back it up**; losing it means re-entering those secrets. |
| `DOCVETA_BASE_URL` | `http://localhost:8080` | Public URL; used in links, cookies (`https` ⇒ Secure cookies + HSTS) and OIDC redirects |
| `DOCVETA_LISTEN` | `:8080` | Listen address |
| `DOCVETA_DATA_DIR` | `./data` | Files (`blobs/`) and temporary uploads (`tmp/`) |
| `DOCVETA_TRUSTED_PROXIES` | — | Comma-separated IPs/CIDRs of reverse proxies (for real client IPs) |
| `DOCVETA_MAX_UPLOAD_MB` | `500` | Largest accepted file |
| `DOCVETA_TRASH_RETENTION` | `720h` | How long deleted documents stay in Trash |
| `DOCVETA_GOTENBERG_URL` | — | Gotenberg address; turns on Word/Excel/PowerPoint documents (`docker compose --profile office up`) |
| `DOCVETA_WATCH_ROOTS` | — | Comma-separated folders administrators may watch for new files |
| `DOCVETA_STORAGE` | `fs` | `fs` (data folder) or `s3` |
| `DOCVETA_S3_ENDPOINT` / `DOCVETA_S3_BUCKET` / `DOCVETA_S3_ACCESS_KEY` / `DOCVETA_S3_SECRET_KEY` | — | S3-compatible storage (MinIO, R2, AWS); keys can also come from `*_FILE` |
| `DOCVETA_SESSION_IDLE` / `DOCVETA_SESSION_MAX` | `720h` / `2160h` | Session lifetimes |
| `DOCVETA_PDF_WORKERS` | `2` | Parallel PDF inspections (memory: ~50–100 MB each) |
| `DOCVETA_JOB_WORKERS` | `4` | Parallel background jobs |
| `DOCVETA_LOG_LEVEL` / `DOCVETA_LOG_FORMAT` | `info` / `json` | `debug`…`error` / `json` or `text` |
| `DOCVETA_ALLOW_LOCAL_TARGETS` | `false` | Let Gotify/ntfy/webhook channels point at private network addresses (`192.168.x.x`, `10.x`, `localhost`, Tailscale `100.64/10`…). Off by default so users can't make the server probe your network; turn it on if your Gotify/ntfy runs on the LAN and you trust your users. |

Migrations run automatically at start (guarded by a database lock). Use
`docveta serve --no-migrate` to control upgrades manually and `docveta migrate` to apply them.

## Reverse proxy (TLS)

Docveta serves plain HTTP; put it behind Caddy, Traefik or Nginx for HTTPS. Example Caddyfile:

```
docs.example.com {
    reverse_proxy docveta:8080 {
        flush_interval -1          # live notifications (Server-Sent Events)
    }
    request_body {
        max_size 600MB
    }
}
```

Nginx: set `client_max_body_size 600m;`, `proxy_buffering off;` for `/api/v1/events`,
and `proxy_read_timeout 1h;`. Set `DOCVETA_TRUSTED_PROXIES` to the proxy's address.

## Single sign-on

*Administration → Single sign-on*. In your identity provider create an OpenID Connect
client (confidential, authorization code flow) with redirect URI
`https://<your-host>/api/v1/auth/oidc/callback`. Group-based access and admin mapping use
the `groups` claim (configurable). Keep at least one local admin password, or know the
recovery command:

```bash
docker compose exec -e DOCVETA_PASSWORD='new-long-password' docveta /docveta user reset-password --email you@example.com
```

## Backups

Two pieces of data matter: **the database** and **the data directory** (`blobs/`).
Files are content-addressed and never modified, and unreferenced files are only removed
seven days after they stop being used, so the safe order is: database first, then files.

```bash
# 1. database
docker compose exec -T db pg_dump -U docveta -Fc docveta > backup/docveta-$(date +%F).dump
# 2. files (incremental; restic, borg or rsync all work)
restic -r /mnt/backup/docveta backup ./data/blobs
# 3. keep .env (DOCVETA_SECRET_KEY!) with the backup, encrypted
```

Restore: create an empty database, `pg_restore -d docveta docveta-YYYY-MM-DD.dump`, restore
`data/blobs`, start Docveta with the same `DOCVETA_SECRET_KEY`, then verify with
`docveta doctor --verify-blobs` (see below).

## Checking an installation

`docveta doctor` checks the configuration, database (version, `pg_trgm`, schema version,
clock skew), data directory (writable, free space), that `DOCVETA_SECRET_KEY` decrypts
stored secrets, OCR workers, and that every stored file exists. `--verify-blobs` also
re-hashes every file to detect corruption (slow on large libraries). It changes nothing
and exits non-zero if a check fails.

```bash
docker compose exec docveta /docveta doctor
```

## Monitoring

* `GET /healthz` — process is up; `GET /readyz` — database reachable.
* `GET /api/v1/openapi.yaml` — the API specification (OpenAPI 3), for API clients.
* `GET /metrics` — Prometheus: request latency per route, processing queue depth,
  documents, workers online, free disk space. (Expose it only on a private network.)
* *Administration → System* shows the same at a glance; *Processing* shows workers and
  failed tasks with retry buttons. Admins get alerts when a worker goes offline or disk
  space runs low (in-app, plus any channels under *Administration → Alerts*).

## Hardware notes (Rockchip boards)

* Put the database and data on NVMe/SSD/eMMC, not an SD card.
* 4 GB RAM is enough for core + PostgreSQL + NPU worker on RK3566; 8 GB+ recommended on
  RK3588/RK3576 for large imports.
* The NPU worker can run on the same board or on other boards; it only needs to reach
  Docveta over HTTP(S).
