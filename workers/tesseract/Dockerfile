# Docveta CPU OCR worker (Tesseract 5). Multi-arch: linux/amd64 and linux/arm64.
FROM python:3.12-slim

ARG TESS_LANGS="eng hin mar ben guj tam tel kan mal pan urd osd"
RUN apt-get update \
 && apt-get install -y --no-install-recommends tesseract-ocr \
      $(for l in $TESS_LANGS; do echo tesseract-ocr-$l; done) \
      fonts-noto-core fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY sdk-python /tmp/sdk
RUN pip install --no-cache-dir "/tmp/sdk[heic]" pytesseract==0.3.13 && rm -rf /tmp/sdk
COPY tesseract/worker.py .

ENV OMP_THREAD_LIMIT=1 PYTHONUNBUFFERED=1
USER nobody
CMD ["python", "worker.py"]
