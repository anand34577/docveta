# Docveta core: one static Go binary with the web UI embedded.
# Multi-arch: docker buildx build --platform linux/amd64,linux/arm64 -t docveta .

FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN mkdir -p ../internal/webui && npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/docveta ./cmd/docveta

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/docveta /docveta
ENV DOCVETA_DATA_DIR=/data DOCVETA_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
USER nonroot
HEALTHCHECK NONE
ENTRYPOINT ["/docveta"]
CMD ["serve"]
