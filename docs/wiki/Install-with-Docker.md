# Install with Docker

The Docker setup runs Docveta, its database and text recognition together, on any machine with
Docker (x64 or ARM 64-bit).

## Start

```bash
mkdir docveta && cd docveta
curl -LO https://raw.githubusercontent.com/anand34577/docveta/main/deploy/docker-compose.yml
docker compose up -d
```

Open `http://<server>:8080` and create your account. There is nothing to edit:

- the **database password** is generated on first start and kept in a Docker volume that only
  the database and Docveta can read
- the **text recognition engine** (PaddleOCR on the processor) fetches its own token from
  Docveta with an enrollment key that only the engine containers can see
- the **secret key** that protects stored passwords is generated and saved in Docveta's data

Your data lives in the Docker volumes `docveta_data` and `docveta_db`.

## More engines

```bash
docker compose --profile tesseract up -d      # Tesseract: 100+ languages
docker compose --profile npu-rockchip up -d   # Rockchip NPU boards (RK3588/RK3576/RK3566)
```

The Rockchip engine needs models converted for your board, see
[OCR engines](OCR-engines#rockchip-npu). For a **GPU**, run the `docveta-ocr` package on the
host or a PC with a GPU and point it at Docveta, see
[OCR engines](OCR-engines#an-engine-on-another-computer).

## Settings

Optional settings (port, the public address behind a reverse proxy, …) go in a `.env` file next
to `docker-compose.yml`; see
[.env.example](https://github.com/anand34577/docveta/blob/main/deploy/.env.example). Any setting
from [Configuration](Configuration) can also be added to the `environment:` section of the
`docveta` service.

## Everyday commands

```bash
docker compose logs -f docveta                     # log
docker compose pull && docker compose up -d        # upgrade
docker compose down                                # stop (data stays in the volumes)
docker compose exec docveta /docveta doctor        # health check
```

**Backups:** back up the `docveta_data` and `docveta_db` volumes together, for example with
`docker compose stop` and a copy of `/var/lib/docker/volumes/docveta_*`.

## Upgrading from an older compose file

Installs that set `POSTGRES_PASSWORD` and folder paths (`DOCVETA_DATA_DIR=./data`) in `.env`
keep working unchanged: the new compose file reuses that password and those folders. Worker
tokens in `.env` are still used; remove them to let the engines enroll themselves.

## Images

| Image | Contents |
|---|---|
| `ghcr.io/anand34577/docveta` | Docveta (amd64, arm64) |
| `ghcr.io/anand34577/docveta-ocr` | PaddleOCR engine, CPU (amd64, arm64) |
| `ghcr.io/anand34577/docveta-worker-tesseract` | Tesseract engine (amd64, arm64) |
| `ghcr.io/anand34577/docveta-worker-rknn` | Rockchip NPU engine (arm64) |
