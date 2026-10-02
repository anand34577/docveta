# Docveta OCR worker for Rockchip NPUs (RK3588/RK3588S, RK3576, RK3566/RK3568). linux/arm64 only.
# Build from the workers/ directory:
#   docker build -t docveta-worker-rknn -f rknn/Dockerfile .
FROM python:3.11-slim-bookworm

ARG RKNN_VERSION=2.3.2
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates libgomp1 fonts-noto-core fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*

# NPU user-space runtime. Its version must match (or be compatible with) the RKNPU kernel
# driver on the board: check with `cat /sys/kernel/debug/rknpu/version` on the host.
RUN curl -fsSL -o /usr/lib/librknnrt.so \
      "https://github.com/airockchip/rknn-toolkit2/raw/v${RKNN_VERSION}/rknpu2/runtime/Linux/librknn_api/aarch64/librknnrt.so"

WORKDIR /app
COPY sdk-python /tmp/sdk
RUN pip install --no-cache-dir "/tmp/sdk[heic,ppocr]" "rknn-toolkit-lite2==${RKNN_VERSION}" \
 && rm -rf /tmp/sdk
COPY rknn/worker.py rknn/soc.py ./

# Converted models: mount a volume with models/<soc>/... (see convert/convert.py)
VOLUME ["/app/models"]
ENV PYTHONUNBUFFERED=1 DOCVETA_MODELS_DIR=/app/models
CMD ["python", "worker.py"]
