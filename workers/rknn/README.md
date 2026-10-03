# Docveta OCR worker for Rockchip NPUs

Reads text from scans and photos on the NPU of **RK3588 / RK3588S**, **RK3576** and
**RK3566 / RK3568** boards, using PaddleOCR (PP-OCR) models converted to `.rknn`.

| SoC | NPU cores used | Detection input | Notes |
|---|---|---|---|
| RK3588 / RK3588S | 3 (one context per core) | 960 px tiles | Fastest; three pages in parallel |
| RK3576 | 2 | 960 px tiles | |
| RK3566 / RK3568 | 1 | 640 px tiles | Low power; pair with a CPU worker for big backlogs |

The SoC is detected from the device tree; override with `DOCVETA_RKNN_SOC=rk3588|rk3576|rk3566|rk3568`.
The worker advertises tags `npu` + the SoC name, so Docveta routes OCR to it first (see
*Administration → Processing → Prefer workers tagged*).

## 1. Convert models (once, on an x86_64 Linux PC)

`.rknn` files are specific to a SoC family. RKNN-Toolkit2 only runs on x86_64.

```bash
cd workers/rknn/convert
python -m venv .venv && . .venv/bin/activate
pip install rknn-toolkit2 paddle2onnx onnx "onnxsim==0.4.36" pillow
# 20–200 sample document images (scans/phone photos like yours) for INT8 calibration:
mkdir calib && cp ~/Scans/*.jpg calib/
python convert.py --soc rk3588 --scripts en devanagari --calib-dir calib --out ../models
```

Scripts available: `en` (English and Latin-alphabet languages), `devanagari` (Hindi,
Marathi, Nepali, Sanskrit — also reads English), `ta`, `te`, `ka`. Languages without a
converted model are routed by Docveta to another worker (e.g. Tesseract).

Copy `models/<soc>/` to the board.

## 2. Run on the board

Requirements: a Rockchip BSP / Armbian / Ubuntu-Rockchip kernel with the RKNPU driver
(`/dev/rknpu`, or the DRM render node on newer kernels). Check the driver version with
`sudo cat /sys/kernel/debug/rknpu/version`; the runtime (`RKNN_VERSION` in the
Dockerfile) must be compatible with it.

```bash
# from the repository's workers/ directory, on the board
docker build -t docveta-worker-rknn -f rknn/Dockerfile .
docker run -d --name docveta-worker-rknn --restart unless-stopped \
  --device /dev/rknpu --device /dev/dri \
  -v /opt/docveta/models:/app/models:ro \
  -e DOCVETA_URL=https://docs.example.com \
  -e DOCVETA_WORKER_TOKEN=dvt_wrk_... \
  docveta-worker-rknn
```

Create the token in Docveta: *Administration → Processing → Add worker*.

## Design notes

* **Static shapes.** NPUs are fastest with fixed input shapes. Detection uses square
  tiles (with overlap; fragments of a line split across tiles are merged). Recognition
  models are compiled for widths 320/640/960/1280 at height 48; each line uses the
  smallest width that fits.
* **Quantisation.** Detection is INT8 (robust, fast); recognition is FP16 by default
  because INT8 raises the character error rate.
* **Concurrency = NPU cores.** One inference context per core; the worker tells Docveta
  its concurrency, so Docveta never leases more pages than the NPU can run in parallel.
* **CPU/NPU overlap.** PDF rasterisation and pre/post-processing run on the CPU while
  other contexts use the NPU.
* **Word boxes.** PP-OCR returns line boxes; word positions are derived from CTC
  timesteps, which is accurate enough for a selectable/searchable PDF text layer.
* The PP-OCR pre/post-processing (`docveta_worker.ppocr` in the SDK) is plain numpy/OpenCV
  with injected model calls, shared with the ONNX Runtime engine (`workers/onnx`), and
  unit-tested without hardware (`workers/sdk-python/tests/test_ppocr.py`).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `cannot initialise the NPU runtime` | Pass `--device /dev/rknpu` (and/or `/dev/dri`); match `librknnrt.so` to the driver version |
| `No models found for this SoC` | Convert models for your SoC and mount them at `/app/models/<soc>/` |
| Worker shows *offline* in Docveta | Check `DOCVETA_URL` is reachable from the board and the token is valid |
| Poor accuracy on photos | Prefer flat, well-lit photos; Docveta can fall back to Tesseract for specific languages |
