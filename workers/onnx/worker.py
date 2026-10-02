"""Docveta OCR engine on ONNX Runtime: PaddleOCR (PP-OCR) on a GPU, an NPU or the CPU.

One engine for desktops, laptops and servers:

* Windows: any DirectX 12 GPU through DirectML: integrated (Intel/AMD) or dedicated and
  external (NVIDIA/AMD/Intel Arc, Thunderbolt eGPUs). NPUs in Copilot+ PCs through
  DirectML or Qualcomm QNN.
* Linux: NVIDIA GPUs through CUDA (onnxruntime-gpu), Intel GPUs/NPUs through OpenVINO.
* macOS: Apple GPU and Neural Engine through Core ML.
* Everywhere: the CPU, which is always the fallback.

Choose the device with DOCVETA_OCR_DEVICE:

    auto  (default) the best GPU found, otherwise the CPU
    gpu   prefer a dedicated/external GPU
    igpu  prefer the integrated GPU (saves power; leaves the big GPU free)
    npu   a neural processing unit (Copilot+ PCs, Intel Core Ultra, Apple Neural Engine)
    cpu   the processor only

DOCVETA_OCR_GPU_ID picks a specific adapter by number (see `docveta-ocr --list-devices`).
If the chosen device can't start, the engine logs why and falls back to the CPU.
Rockchip NPUs use the dedicated worker in workers/rknn instead.

Models (from workers/onnx/convert.py) live in DOCVETA_MODELS_DIR, or in models/ next to
this program: det.onnx, rec_<script>.onnx + dict_<script>.txt, VERSION.
"""

from __future__ import annotations

import argparse
import logging
import math
import os
import platform
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from typing import Any, Optional

import cv2
import numpy as np
from PIL import Image

from docveta_worker import Engine, EngineError, Line, PageResult, Word, run
from docveta_worker import ppocr

log = logging.getLogger("docveta_worker.onnx")

DEVICES = ("auto", "gpu", "igpu", "npu", "cpu")
DET_SIZE = 1280  # detection input on GPU/CPU (multiple of 32); larger pages are tiled
DET_MEAN = np.array([0.485, 0.456, 0.406], dtype=np.float32)
DET_STD = np.array([0.229, 0.224, 0.225], dtype=np.float32)
REC_MAX_WIDTH = 3200


def app_dir() -> str:
    """Directory of this program (also when frozen into an executable)."""
    if getattr(sys, "frozen", False):
        return os.path.dirname(sys.executable)
    return os.path.dirname(os.path.abspath(__file__))


def models_dir() -> str:
    return os.environ.get("DOCVETA_MODELS_DIR") or os.path.join(app_dir(), "models")


# ---------------------------------------------------------------- device choice

@dataclass
class Plan:
    provider: str
    options: dict[str, Any] = field(default_factory=dict)
    label: str = ""
    kind: str = "gpu"  # gpu | npu | cpu: advertised as a worker tag for routing


def plans_for(device: str, available: list[str], gpu_id: Optional[int] = None) -> list[Plan]:
    """Execution providers to try, best first. The CPU is always last."""
    device = device if device in DEVICES else "auto"
    have = set(available)
    out: list[Plan] = []
    if device in ("auto", "gpu", "igpu"):
        if "CUDAExecutionProvider" in have and device != "igpu":
            out.append(Plan("CUDAExecutionProvider", {"device_id": gpu_id or 0}, "NVIDIA GPU (CUDA)"))
        if "DmlExecutionProvider" in have:
            if gpu_id is not None:
                out.append(Plan("DmlExecutionProvider", {"device_id": gpu_id}, f"GPU #{gpu_id} (DirectML)"))
            else:
                pref = "minimum_power" if device == "igpu" else "high_performance"
                what = "integrated GPU" if device == "igpu" else "GPU"
                out.append(Plan("DmlExecutionProvider", {"performance_preference": pref, "device_filter": "gpu"}, f"{what} (DirectML)"))
                out.append(Plan("DmlExecutionProvider", {}, "default GPU (DirectML)"))  # older runtimes lack the options
        if "ROCMExecutionProvider" in have and device != "igpu":
            out.append(Plan("ROCMExecutionProvider", {"device_id": gpu_id or 0}, "AMD GPU (ROCm)"))
        if "CoreMLExecutionProvider" in have:
            out.append(Plan("CoreMLExecutionProvider", {"MLComputeUnits": "CPUAndGPU"}, "Apple GPU (Core ML)"))
        if "OpenVINOExecutionProvider" in have:
            out.append(Plan("OpenVINOExecutionProvider", {"device_type": "GPU"}, "Intel GPU (OpenVINO)"))
    if device == "npu":
        if "QNNExecutionProvider" in have:
            out.append(Plan("QNNExecutionProvider", {"backend_path": "QnnHtp.dll" if os.name == "nt" else "libQnnHtp.so"}, "Qualcomm NPU (QNN)", "npu"))
        if "DmlExecutionProvider" in have:
            out.append(Plan("DmlExecutionProvider", {"device_filter": "npu"}, "NPU (DirectML)", "npu"))
        if "OpenVINOExecutionProvider" in have:
            out.append(Plan("OpenVINOExecutionProvider", {"device_type": "NPU"}, "Intel NPU (OpenVINO)", "npu"))
        if "CoreMLExecutionProvider" in have:
            out.append(Plan("CoreMLExecutionProvider", {"MLComputeUnits": "CPUAndNeuralEngine"}, "Apple Neural Engine (Core ML)", "npu"))
    out.append(Plan("CPUExecutionProvider", {}, "CPU", "cpu"))
    return out


def _session(ort: Any, path: str, plan: Plan, threads: int) -> Any:
    so = ort.SessionOptions()
    so.log_severity_level = 3
    if plan.provider == "DmlExecutionProvider":
        # DirectML requires these and doesn't support parallel runs on one session.
        so.enable_mem_pattern = False
        so.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    if plan.kind == "cpu":
        so.intra_op_num_threads = threads
    providers: list[Any] = [(plan.provider, plan.options)] if plan.options else [plan.provider]
    if plan.provider != "CPUExecutionProvider":
        providers.append("CPUExecutionProvider")  # ops the device can't run
    return ort.InferenceSession(path, sess_options=so, providers=providers)


@dataclass
class Models:
    det: Any
    rec: dict[str, Any]
    charsets: dict[str, list[str]]
    lock: threading.Lock = field(default_factory=threading.Lock)


def load_models(ort: Any, mdir: str, scripts: list[str], plan: Plan, threads: int) -> Models:
    det = _session(ort, os.path.join(mdir, "det.onnx"), plan, threads)
    used = det.get_providers()[0]
    if used != plan.provider:  # ONNX Runtime silently fell back; don't load the rest
        raise RuntimeError(f"ONNX Runtime used {used} instead")
    rec = {s: _session(ort, os.path.join(mdir, f"rec_{s}.onnx"), plan, threads) for s in scripts}
    charsets = {s: ppocr.load_charset(os.path.join(mdir, f"dict_{s}.txt")) for s in scripts}
    return Models(det, rec, charsets)


def warm_up(m: Models, plan: Plan) -> None:
    """Runs each model once: fails fast if the device can't actually execute them."""
    det_infer(m.det, np.zeros((1, 64, 64, 3), dtype=np.uint8))
    for sess in m.rec.values():
        sess.run(None, {sess.get_inputs()[0].name: np.zeros((1, 3, 48, 160), dtype=np.float32)})
    used = m.det.get_providers()[0]
    if used != plan.provider:
        raise RuntimeError(f"ONNX Runtime used {used} instead")


# ---------------------------------------------------------------- inference

def det_infer(sess: Any, x: np.ndarray) -> np.ndarray:
    """ppocr.detect gives (1, S, S, 3) uint8 RGB; PP-OCR wants normalised BGR NCHW."""
    a = x[..., ::-1].astype(np.float32) / 255.0
    a = (a - DET_MEAN) / DET_STD
    a = np.ascontiguousarray(a.transpose(0, 3, 1, 2))
    return sess.run(None, {sess.get_inputs()[0].name: a})[0]


def rec_input(crop: np.ndarray) -> tuple[np.ndarray, float]:
    """Height 48, width padded to a multiple of 160 (few distinct shapes keep GPUs fast).
    Returns NCHW float input and the share of the width holding the image."""
    h, w = crop.shape[:2]
    tw = min(REC_MAX_WIDTH, max(1, int(math.ceil(ppocr.REC_HEIGHT * w / max(h, 1)))))
    width = max(160, int(math.ceil(tw / 160)) * 160)
    resized = cv2.resize(crop, (tw, ppocr.REC_HEIGHT), interpolation=cv2.INTER_LINEAR)
    a = (resized[..., ::-1].astype(np.float32) / 255.0 - 0.5) / 0.5
    out = np.zeros((ppocr.REC_HEIGHT, width, 3), dtype=np.float32)  # zero = padding, as in PaddleOCR
    out[:, :tw] = a
    return np.ascontiguousarray(out.transpose(2, 0, 1)[None]), tw / width


def recognize_page(m: Models, img: np.ndarray, script: str) -> list[Line]:
    sess = m.rec[script]
    name = sess.get_inputs()[0].name
    charset = m.charsets[script]
    lines: list[Line] = []
    boxes = ppocr.detect(img, lambda x: det_infer(m.det, x), DET_SIZE)
    # One line per model run keeps this simple; batching same-width lines would speed up GPUs.
    for b in boxes:
        inp, frac = rec_input(ppocr.crop_box(img, b))
        probs = sess.run(None, {name: inp})[0][0]
        r = ppocr.ctc_decode(probs, charset, frac)
        if not r.text or r.confidence < 0.5:
            continue
        x0, y0, x1, y1 = b.rect
        w = x1 - x0
        words = [Word(t, (x0 + f0 * w, y0, x0 + f1 * w, y1), r.confidence) for t, f0, f1 in r.words]
        lines.append(Line(text=r.text, bbox=(x0, y0, x1, y1), confidence=r.confidence, words=words))
    return lines


# ---------------------------------------------------------------- engine

class OnnxEngine(Engine):
    name = "ppocr-onnx"

    def __init__(self, device: Optional[str] = None) -> None:
        import onnxruntime as ort

        self.ort = ort
        self.mdir = models_dir()
        if not os.path.isfile(os.path.join(self.mdir, "det.onnx")):
            raise SystemExit(f"No OCR models in {self.mdir} (det.onnx missing). Download the Docveta OCR package, or set DOCVETA_MODELS_DIR.")
        self.scripts = sorted(f[4:-5] for f in os.listdir(self.mdir)
                              if f.startswith("rec_") and f.endswith(".onnx") and os.path.isfile(os.path.join(self.mdir, f"dict_{f[4:-5]}.txt")))
        if not self.scripts:
            raise SystemExit(f"No recognition models (rec_<script>.onnx + dict_<script>.txt) in {self.mdir}")
        self.languages = sorted(l for l, s in ppocr.LANG_SCRIPT.items() if s in self.scripts)

        device = (device or os.environ.get("DOCVETA_OCR_DEVICE", "auto")).lower()
        gpu_id = os.environ.get("DOCVETA_OCR_GPU_ID")
        cpus = os.cpu_count() or 2
        self.plan, self.cpu_slots = None, max(1, min(4, cpus // 4))
        for plan in plans_for(device, ort.get_available_providers(), int(gpu_id) if gpu_id else None):
            try:
                m = load_models(ort, self.mdir, self.scripts, plan, max(1, cpus // self.cpu_slots))
                warm_up(m, plan)
            except Exception as e:  # noqa: BLE001 - any failure means: try the next device
                log.warning("%s isn't usable (%s); trying the next option", plan.label, str(e).splitlines()[0][:200])
                continue
            self.plan, self.first = plan, m
            break
        if self.plan is None:
            raise SystemExit("ONNX Runtime couldn't run the models on any device")
        if device not in ("auto", "cpu") and self.plan.kind == "cpu":
            log.warning("DOCVETA_OCR_DEVICE=%s isn't available on this computer; using the CPU", device)
        # GPU/NPU: one stream (DirectML can't run sessions in parallel); CPU: a few.
        self.concurrency = self.cpu_slots if self.plan.kind == "cpu" else 1
        self.tags = [self.plan.kind]
        version = "unknown"
        try:
            with open(os.path.join(self.mdir, "VERSION"), encoding="utf-8") as f:
                version = f.read().strip()
        except OSError:
            pass
        self.version = f"{version}/{self.plan.kind}"
        self.models = {"det": "det.onnx", **{f"rec_{s}": f"rec_{s}.onnx" for s in self.scripts}}
        log.info("OCR on %s; scripts %s; %d parallel page(s)", self.plan.label, self.scripts, self.concurrency)

    def open_session(self, slot: int) -> Models:
        if slot == 0:
            return self.first
        cpus = os.cpu_count() or 2
        return load_models(self.ort, self.mdir, self.scripts, self.plan, max(1, cpus // self.cpu_slots))

    def _script(self, languages: list[str]) -> str:
        for l in languages:
            s = ppocr.LANG_SCRIPT.get(l.split("-")[0].lower())
            if s in self.scripts:
                return s
        return "en" if "en" in self.scripts else self.scripts[0]

    def recognize(self, session: Optional[Models], image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        if session is None:
            raise EngineError("engine_error", "no model session", retryable=True)
        img = np.asarray(image.convert("RGB"))
        script = self._script(languages)
        with session.lock:
            lines = recognize_page(session, img, script)
        lang = next((l for l in languages if ppocr.LANG_SCRIPT.get(l.split("-")[0].lower()) == script), None)
        return PageResult(lines=lines, language=lang)


# ---------------------------------------------------------------- command line

def list_devices() -> None:
    import onnxruntime as ort

    print(f"ONNX Runtime {ort.__version__} ({platform.system()} {platform.machine()})")
    print("Execution providers:", ", ".join(ort.get_available_providers()))
    if os.name == "nt":
        try:
            out = subprocess.run(["powershell", "-NoProfile", "-Command",
                                  "Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name }"],
                                 capture_output=True, text=True, timeout=20).stdout
            gpus = [l.strip() for l in out.splitlines() if l.strip()]
            print("Graphics adapters (DOCVETA_OCR_GPU_ID usually follows this order):")
            for i, g in enumerate(gpus):
                print(f"  {i}: {g}")
        except Exception:  # noqa: BLE001
            pass
    for d in DEVICES:
        labels = [p.label for p in plans_for(d, ort.get_available_providers())]
        print(f"DOCVETA_OCR_DEVICE={d:<5} tries: {' -> '.join(labels)}")


def self_test(device: Optional[str]) -> None:
    """Recognises a generated test image and prints the text and timing."""
    from PIL import ImageDraw, ImageFont

    img = Image.new("RGB", (1240, 400), "white")
    draw = ImageDraw.Draw(img)
    font = None
    shipped = os.path.join(os.environ.get("DOCVETA_FONTS_DIR") or os.path.join(app_dir(), "fonts"), "NotoSans-Regular.ttf")
    for name in (shipped, "arial.ttf", "DejaVuSans.ttf", "/System/Library/Fonts/Supplemental/Arial.ttf"):
        try:
            font = ImageFont.truetype(name, 44)
            break
        except OSError:
            continue
    if font is None:
        font = ImageFont.load_default(size=44)  # Pillow's built-in scalable font
    text = ["Electricity bill dated 05/08/2026", "Amount due: 1842.00 INR"]
    for i, t in enumerate(text):
        draw.text((60, 80 + i * 120), t, fill="black", font=font)
    eng = OnnxEngine(device)
    s = eng.open_session(0)
    t0 = time.perf_counter()
    res = eng.recognize(s, img, 1, ["en"])
    dt = time.perf_counter() - t0
    print(f"Device: {eng.plan.label}")
    for line in res.lines:
        print(f"  {line.confidence:.2f}  {line.text}")
    print(f"{len(res.lines)} line(s) in {dt * 1000:.0f} ms")
    if not res.lines:
        raise SystemExit("self-test recognised no text")


def main() -> None:
    p = argparse.ArgumentParser(prog="docveta-ocr", description="Docveta OCR engine (PaddleOCR on ONNX Runtime). "
                                "Normally started by Docveta itself; see the wiki page 'OCR engines'.")
    p.add_argument("--list-devices", action="store_true", help="show GPUs/NPUs ONNX Runtime can use and exit")
    p.add_argument("--self-test", action="store_true", help="recognise a test image and exit")
    p.add_argument("--device", choices=DEVICES, help="override DOCVETA_OCR_DEVICE")
    args = p.parse_args()
    fonts = os.path.join(app_dir(), "fonts")  # Noto fonts shipped in the package, for searchable PDFs
    if os.path.isdir(fonts):
        os.environ.setdefault("DOCVETA_FONTS_DIR", fonts)
    logging.basicConfig(level=os.environ.get("DOCVETA_LOG_LEVEL", "INFO").upper(), format="%(asctime)s %(levelname)s %(message)s")
    if args.list_devices:
        list_devices()
    elif args.self_test:
        self_test(args.device)
    else:
        run(OnnxEngine(args.device))


if __name__ == "__main__":
    main()
