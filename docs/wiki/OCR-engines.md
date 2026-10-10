# OCR engines: CPU, GPU and NPU

**OCR** (text recognition) reads the words in scans and photos, so you can search them and
copy text from them. PDFs that already contain text (bank statements, e-bills) don't need
OCR; they're searchable right away.

Docveta doesn't do OCR inside the main program. It hands pages to an **OCR engine**: a
separate program on the same computer or on another one. This lets one engine use your
graphics card, another a board's AI chip, and lets you add or swap engines without touching
Docveta.

- [The bundled engine](#the-bundled-engine-recommended)
- [Choose the device](#choose-the-device-cpu-gpu-npu)
- [Windows: integrated and external GPUs](#windows-integrated-and-external-gpus)
- [Linux: NVIDIA, Intel and AMD](#linux-nvidia-intel-and-amd)
- [macOS: Apple GPU and Neural Engine](#macos-apple-gpu-and-neural-engine)
- [Check what's found and test](#check-whats-found-and-test)
- [An engine on another computer](#an-engine-on-another-computer)
- [Rockchip NPU](#rockchip-npu)
- [Allwinner A733 NPU](#allwinner-a733-npu)
- [Tesseract (100+ languages)](#tesseract)
- [Several engines: who gets which document](#several-engines-who-gets-which-document)

## The bundled engine (recommended)

`docveta-ocr` runs PaddleOCR, accurate for printed documents in English, Hindi, Marathi,
Nepali (Devanagari), Tamil, Telugu and Kannada. It's in the Windows installer and in the
`docveta-ocr-<version>-<system>-<cpu>.zip` downloads (Windows x64, Linux x64/arm64, macOS
Apple silicon).

**When the `ocr` folder sits next to the Docveta program, Docveta starts the engine by itself.**
Nothing to configure: no token, no second service. It appears as `local-ocr` under
*Administration → Processing*, and its output appears in Docveta's log. If it crashes, Docveta
restarts it.

```
Docveta/
  docveta(.exe)
  docveta.conf          ← optional settings
  ocr/
    docveta-ocr(.exe)
    models/           ← recognition models (det.onnx, rec_*.onnx)
    fonts/            ← fonts for searchable PDFs
```

To stop Docveta from starting it, set `DOCVETA_LOCAL_OCR=off`.

## Choose the device: CPU, GPU, NPU

Set `DOCVETA_OCR_DEVICE` in `docveta.conf` (the Windows installer asks during setup), then
restart Docveta:

| Setting | Uses |
|---|---|
| `auto` (default) | the best graphics card found, otherwise the processor |
| `gpu` | a **dedicated or external** graphics card (NVIDIA, AMD Radeon, Intel Arc, eGPU over Thunderbolt/USB4) |
| `igpu` | the **integrated** graphics in your processor (Intel Iris/UHD/Arc, AMD Radeon). Uses less power and keeps the big GPU free for games or video |
| `npu` | an **AI accelerator**: Copilot+ PCs (Snapdragon X), Intel Core Ultra, Apple Neural Engine |
| `cpu` | the processor only |
| `DOCVETA_OCR_GPU_ID=1` | a specific graphics adapter by number (see `--list-devices`) |

**If the chosen device isn't there or doesn't work, the engine falls back to the processor
automatically** and says so in the log, for example:

```
WARNING GPU (DirectML) isn't usable (…); trying the next option
WARNING DOCVETA_OCR_DEVICE=gpu isn't available on this computer; using the CPU
INFO OCR on CPU; scripts ['devanagari', 'en']; 3 parallel page(s)
```

How much faster is a GPU? For a typical A4 page the processor needs 1–3 seconds, a GPU
0.3–1 second. A processor is perfectly fine for a household; a GPU helps when you import
thousands of old scans.

## Windows: integrated and external GPUs

The Windows engine uses **DirectML**, which works with every DirectX 12 graphics card:
NVIDIA (GTX 900 series or newer), AMD (GCN or newer), Intel (HD 500 or newer, Iris, Arc),
built in or external. No CUDA or other driver packs are needed: just a current graphics driver
from Windows Update or the vendor.

- **Laptop with integrated and dedicated GPU** (e.g. Intel + NVIDIA): `gpu` picks the
  dedicated one, `igpu` the integrated one.
- **External GPU (eGPU)**: connect it before Docveta starts; `gpu` prefers it, because Windows
  reports it as the high-performance adapter. If you unplug it, Docveta falls back to the
  integrated GPU or processor at the next start.
- **Several GPUs**: run `ocr\docveta-ocr.exe --list-devices` and set `DOCVETA_OCR_GPU_ID` to
  the adapter's number.
- **NPU (Copilot+ PC, Intel Core Ultra)**: `npu` tries the NPU through DirectML. NPU support
  in Windows is new; if your NPU isn't used, keep `auto` (GPU) or `cpu`.

## Linux: NVIDIA, Intel and AMD

The Linux download uses the **processor**, which works everywhere. For a GPU:

**NVIDIA (CUDA)**: install the NVIDIA driver, CUDA 12 and cuDNN 9, then run the engine
from Python with the GPU build of ONNX Runtime:

```bash
python3 -m venv ~/docveta-ocr && . ~/docveta-ocr/bin/activate
pip install "docveta-worker[heic,ppocr] @ git+https://github.com/anand34577/docveta#subdirectory=workers/sdk-python" onnxruntime-gpu
curl -LO https://github.com/anand34577/docveta/releases/latest/download/docveta-ocr-models-<version>.zip
unzip docveta-ocr-models-*.zip -d ~/docveta-ocr/models
curl -LO https://raw.githubusercontent.com/anand34577/docveta/main/workers/onnx/worker.py
DOCVETA_MODELS_DIR=~/docveta-ocr/models python worker.py --list-devices    # should list CUDAExecutionProvider
```

Then connect it as [an engine on another computer](#an-engine-on-another-computer) (it can be
the same computer: use `http://localhost:8080`), and set `DOCVETA_LOCAL_OCR=off` so Docveta doesn't
also start the CPU engine.

**Intel GPUs and NPUs (OpenVINO)**: the same, with `pip install onnxruntime-openvino`, then
`DOCVETA_OCR_DEVICE=gpu` or `npu`.

**AMD (ROCm)**: the same, with ONNX Runtime built for ROCm.

The service installed by `install.sh` already has access to the GPU (groups `video` and
`render`).

## macOS: Apple GPU and Neural Engine

The macOS engine uses **Core ML**: `auto`/`gpu` run on the Apple GPU, `npu` on the Neural
Engine, `cpu` on the processor. All Apple-silicon Macs have both.

## Check what's found and test

```bash
ocr/docveta-ocr --list-devices      # Windows: ocr\docveta-ocr.exe --list-devices
ocr/docveta-ocr --self-test         # reads a test image and shows the time
ocr/docveta-ocr --self-test --device cpu
```

Example on a laptop with NVIDIA and AMD graphics:

```
ONNX Runtime 1.23.0 (Windows AMD64)
Execution providers: DmlExecutionProvider, CPUExecutionProvider
Graphics adapters (DOCVETA_OCR_GPU_ID usually follows this order):
  0: NVIDIA GeForce GTX 1650
  1: AMD Radeon(TM) Graphics
DOCVETA_OCR_DEVICE=auto  tries: GPU (DirectML) -> default GPU (DirectML) -> CPU
DOCVETA_OCR_DEVICE=igpu  tries: integrated GPU (DirectML) -> default GPU (DirectML) -> CPU
…
Device: GPU (DirectML)
  1.00  Electricity bill dated 05/08/2026
  0.98  Amount due: 1842.00 INR
```

In Docveta, *Administration → Processing* shows each engine, its device (`gpu`, `npu`, `cpu`
tag), and how many pages it handled.

## An engine on another computer

Any engine can run on a different machine, for example a gaming PC with a big GPU
processing documents for a small home server.

1. In Docveta: *Administration → Processing → Add worker*, give it a name, copy the token.
2. On the other computer, unzip the `docveta-ocr` package and run:

   ```bash
   # Linux / macOS
   DOCVETA_URL=http://my-server:8080 DOCVETA_WORKER_TOKEN=dvt_wrk_... DOCVETA_OCR_DEVICE=gpu ./ocr/docveta-ocr
   ```
   ```bat
   :: Windows (Command Prompt)
   set DOCVETA_URL=http://my-server:8080
   set DOCVETA_WORKER_TOKEN=dvt_wrk_...
   set DOCVETA_OCR_DEVICE=gpu
   ocr\docveta-ocr.exe
   ```

The engine only connects *out* to Docveta, so the other computer needs no open ports. Several
engines can work at once; each document page goes to one of them.

## Rockchip NPU

On RK3588, RK3576 and RK3566/RK3568 boards, the dedicated NPU engine is several times faster
than the processor. It runs in Docker (`docker compose --profile npu-rockchip up -d`) and needs models
converted for your chip. See
[workers/rknn/README.md](https://github.com/anand34577/docveta/blob/main/workers/rknn/README.md).

## Allwinner A733 NPU

On A733 boards such as the Radxa Cubie A7A, the NPU finds the text on each page and reads the
lines, with the same accuracy as the GPU/CPU engine (about 7x faster than the processor). The
[Linux installer](Install-on-Linux) finds the NPU and sets everything up, also inside a Proxmox
container (`get-docveta.sh --proxmox <CTID>` on the host first). See
[workers/allwinner/README.md](https://github.com/anand34577/docveta/blob/main/workers/allwinner/README.md).

## Tesseract

Tesseract supports more than 100 languages (including Bengali, Gujarati, Punjabi, Malayalam,
Urdu) but is less accurate on photos than PaddleOCR. It runs in Docker:
`docker compose --profile tesseract up -d`. See [Install with Docker](Install-with-Docker).

## Several engines: who gets which document

*Administration → Processing*, processing settings:

- **Prefer workers tagged** (default `npu`): pages go first to engines with these tags. If
  no engine with them has been online in the last day, any engine takes pages immediately,
  so a GPU-only or CPU-only setup never waits.
- **Fall back to any worker after (minutes)** (default 15): how long a page waits for a
  preferred engine before any engine may take it.
- Engines first get pages in languages they support. A page in a language no engine lists
  is picked up once the fallback time has passed (or immediately when no preferred engine is
  online); the engine then reads it with its closest model, usually English.

Writing your own engine takes about 100 lines of Python: see the
[worker protocol](https://github.com/anand34577/docveta/blob/main/docs/workers.md).
