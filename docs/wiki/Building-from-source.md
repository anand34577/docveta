# Building from source

You need Go 1.26+, Node 22+, and PostgreSQL 16+ to run it. Python 3.10+ for the OCR engines.

```bash
git clone https://github.com/anand34577/docveta && cd docveta
cd web && npm ci && npm run build && cd ..     # builds the web app into internal/webui/dist
go build -tags nodynamic -o docveta ./cmd/docveta                  # one binary with the web app inside
./docveta                                        # setup page on http://localhost:8080
```

The build is pure Go (`CGO_ENABLED=0` works). `-tags nodynamic` keeps the HEIC/AVIF decoders from linking to the system loader (glibc), so the binary also runs on Alpine and in minimal containers. Cross-compiling is one command:

```bash
GOOS=windows GOARCH=amd64 go build -tags nodynamic -o docveta.exe ./cmd/docveta
GOOS=linux   GOARCH=arm64 go build -tags nodynamic -o docveta     ./cmd/docveta
GOOS=darwin  GOARCH=arm64 go build -tags nodynamic -o docveta     ./cmd/docveta
```

## Development

```bash
# backend with readable logs (http://localhost:8080)
DOCVETA_DEV=true go run ./cmd/docveta serve
# frontend with hot reload (http://localhost:5173, proxies the API to :8080)
cd web && npm run dev
```

Tests:

```bash
make test                       # Go, TypeScript, worker tests
DOCVETA_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/docveta_test go test -race ./internal/app -run Integration
```

The integration test uses a throwaway schema and validates every API response against
`internal/api/openapi.yaml`.

## The OCR engine

```bash
cd workers/onnx
pip install "../sdk-python[heic,ppocr]" onnxruntime          # onnxruntime-directml on Windows
pip install paddlepaddle "paddle2onnx==1.3.1" onnx && python convert.py --out models   # once
python worker.py --list-devices
python worker.py --self-test
pip install pyinstaller && pyinstaller docveta-ocr.spec        # standalone build in dist/docveta-ocr
```

## Windows installer

With [Inno Setup 6](https://jrsoftware.org/isinfo.php):

```bat
iscc /DAppVersion=0.2.0 /DArch=x64 /DSourceDir=C:\path\to\folder-with-docveta.exe deploy\windows\docveta.iss
```

The folder needs `docveta.exe`, `LICENSE`, `README.txt` and optionally `ocr\`.

## Releases

Pushing a tag `v*` runs `.github/workflows/release.yml`: it builds Docveta for 9 platforms,
the OCR engine for Windows/Linux/macOS, the Windows installers and Docker images, and
publishes a GitHub release. *Actions → Release → Run workflow* makes a draft release for
testing.

More: [CONTRIBUTING.md](https://github.com/anand34577/docveta/blob/main/CONTRIBUTING.md),
[design](https://github.com/anand34577/docveta/blob/main/docs/DESIGN.md),
[worker protocol](https://github.com/anand34577/docveta/blob/main/docs/workers.md).
