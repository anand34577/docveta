# Install with Docker

The Docker setup runs Docveta, PostgreSQL and (optionally) an OCR engine together. It works on
any Linux machine with Docker, x64 or ARM 64-bit.

## Start

```bash
mkdir docveta && cd docveta
curl -LO https://raw.githubusercontent.com/anand34577/docveta/main/deploy/docker-compose.yml
curl -Lo .env https://raw.githubusercontent.com/anand34577/docveta/main/deploy/.env.example
```

Edit `.env` and set at least `POSTGRES_PASSWORD` (any long random text). Then:

```bash
mkdir -p data db && sudo chown -R 65532:65532 data   # Docveta runs as an unprivileged user
docker compose up -d
```

Open `http://<server>:8080` and create your account. The database is already connected:
the compose file sets `DOCVETA_DATABASE_URL`, so there's no setup page.

The secret key that protects stored passwords is generated on first start and saved in
`data/docveta.conf`. **Back up the `data` and `db` folders together.**

## Text recognition

Choose one engine (or several, on different machines):

```bash
docker compose --profile ocr up -d       # PaddleOCR on the CPU: good for most documents
docker compose --profile cpu-ocr up -d   # Tesseract: 100+ languages
docker compose --profile npu up -d       # Rockchip NPU boards (RK3588/RK3576/RK3566)
```

Each engine needs a worker token. In Docveta: *Administration → Processing → Add worker*, copy
the token into `.env` (`DOCVETA_OCR_WORKER_TOKEN`, `DOCVETA_CPU_WORKER_TOKEN` or
`DOCVETA_NPU_WORKER_TOKEN`) and run the command again.

For **GPU** text recognition, run the `docveta-ocr` package on the host (or on a PC with a GPU)
and point it at Docveta. See [OCR engines](OCR-engines#an-engine-on-another-computer).

## Images

| Image | Contents |
|---|---|
| `ghcr.io/anand34577/docveta` | Docveta (amd64, arm64) |
| `ghcr.io/anand34577/docveta-ocr` | PaddleOCR engine, CPU (amd64, arm64) |
| `ghcr.io/anand34577/docveta-worker-tesseract` | Tesseract engine (amd64, arm64) |
| `ghcr.io/anand34577/docveta-worker-rknn` | Rockchip NPU engine (arm64) |

## Everyday commands

```bash
docker compose logs -f docveta       # log
docker compose pull && docker compose up -d     # upgrade
docker compose down                # stop (data stays in ./data and ./db)
docker compose exec docveta /docveta doctor         # health check
```

All settings from [Configuration](Configuration) can be added to the `environment:` section
of the `docveta` service.
