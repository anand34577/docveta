VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all web build test test-go test-web test-workers dev docker clean

all: build

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -tags nodynamic -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/docveta ./cmd/docveta

# Integration tests need a database: DOCVETA_TEST_DATABASE_URL=postgres://... make test
test: test-go test-web test-workers

test-go:
	go vet ./...
	go test ./...

test-web:
	cd web && npx tsc -b

test-workers:
	cd workers/sdk-python && python -m unittest discover -s tests

# Backend on :8080 (needs DOCVETA_* env), frontend with hot reload on :5173
dev:
	@echo "Run in two terminals:"
	@echo "  DOCVETA_DEV=true DOCVETA_LOG_FORMAT=text go run ./cmd/docveta serve"
	@echo "  cd web && npm run dev"

docker:
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) -t docveta:$(VERSION) .

clean:
	rm -rf bin internal/webui/dist/assets internal/webui/dist/*.html
