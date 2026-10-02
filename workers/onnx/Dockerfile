# Docveta OCR engine (PaddleOCR on ONNX Runtime, CPU) for Docker. linux/amd64 and linux/arm64.
# Build from the workers/ directory, after putting the models in onnx/models
# (python onnx/convert.py, or unzip docveta-ocr-models-*.zip from a release):
#   docker build -t docveta-ocr -f onnx/Dockerfile .
# For GPU OCR run the docveta-ocr package directly on the host (see the wiki: OCR engines).
FROM python:3.12-slim-bookworm

RUN apt-get update \
 && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 fonts-noto-core fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY sdk-python /tmp/sdk
RUN pip install --no-cache-dir "/tmp/sdk[heic,ppocr]" onnxruntime && rm -rf /tmp/sdk
COPY onnx/worker.py ./
COPY onnx/models ./models

ENV PYTHONUNBUFFERED=1 DOCVETA_OCR_DEVICE=cpu
USER nobody
CMD ["python", "worker.py"]
