"""Docveta OCR worker for the Allwinner A733 NPU (Radxa Cubie A7A and other A733 boards).

Runs PaddleOCR (PP-OCR) detection + recognition models compiled to NBG (.nb) with the
ACUITY Toolkit (see convert/convert.py and README.md) on the NPU through VIPLite 1.13 or 2.0
(device /dev/vipcore). Bound with ctypes, so no C
compiler is needed on the board.

Model layout (DOCVETA_MODELS_DIR):
    det.nb
    rec_<script>_<width>.nb  for every width in ppocr.REC_WIDTHS, + dict_<script>.txt

    python worker.py           run the worker (DOCVETA_URL, DOCVETA_WORKER_TOKEN)
    python worker.py --probe   check the NPU and models, time one inference each, exit
"""

from __future__ import annotations

import ctypes as C
import logging
import os
import sys
import threading
import time
from typing import Optional

import cv2
import numpy as np
from PIL import Image

from docveta_worker import ppocr
from docveta_worker import Engine, EngineError, Line, PageResult, Word, run

log = logging.getLogger("docveta_worker.allwinner")

# Folder of the program: next to the executable in the release package, else next to this file.
APP_DIR = os.path.dirname(sys.executable if getattr(sys, "frozen", False) else os.path.abspath(__file__))
MODELS_DIR = os.environ.get("DOCVETA_MODELS_DIR") or os.path.join(APP_DIR, "models")
LANG_SCRIPT = ppocr.LANG_SCRIPT

# PP-OCR input normalisation, as in the ONNX engine: BGR channel order, then
# detection (x - mean) / std with ImageNet values, recognition (x - 127.5) / 127.5.
# ACUITY keeps it out of the compiled model, so the worker applies it (see Net.lut).
DET_NORM = (np.array([0.485, 0.456, 0.406]) * 255, np.array([0.229, 0.224, 0.225]) * 255)
REC_NORM = (np.full(3, 127.5), np.full(3, 127.5))

# ponytail: the NPU does the heavy lifting; one OpenCV thread per page keeps the shared CPU free.
cv2.setNumThreads(1)

# ---------------------------------------------------------------- VIPLite ABI (vip_lite.h, same in 1.13 and 2.0)

FMT_FP32, FMT_FP16, FMT_UINT8, FMT_INT8, FMT_UINT16, FMT_INT16, FMT_BFP16, FMT_INT32 = 0, 1, 2, 3, 4, 5, 7, 8
NP_DTYPE = {FMT_FP32: np.float32, FMT_FP16: np.float16, FMT_UINT8: np.uint8, FMT_INT8: np.int8,
            FMT_UINT16: np.uint16, FMT_INT16: np.int16, FMT_BFP16: np.uint16, FMT_INT32: np.int32}
Q_NONE, Q_DFP, Q_AFFINE = 0, 1, 2
PROP_QUANT, PROP_NDIM, PROP_SIZES, PROP_FORMAT, PROP_DFP_POS, PROP_SCALE, PROP_ZERO_POINT = range(7)
NET_INPUT_COUNT, NET_OUTPUT_COUNT, NET_SET_CORE_INDEX = 1, 2, 70
CREATE_FROM_FILE = 0x01
OPER_FLUSH, OPER_INVALIDATE = 1, 2
HW_DEVICE_COUNT, HW_CORE_COUNT = 1, 2
POWER_SET_FREQUENCY = 0x0001


class _Affine(C.Structure):
    _fields_ = [("scale", C.c_float), ("zero_point", C.c_int32)]


class _QuantData(C.Union):
    _fields_ = [("dfp", C.c_int32), ("affine", _Affine)]


class BufferParams(C.Structure):  # vip_buffer_create_params_t
    _fields_ = [("num_of_dims", C.c_uint32), ("sizes", C.c_uint32 * 6), ("data_format", C.c_int32),
                ("quant_format", C.c_int32), ("quant_data", _QuantData), ("memory_type", C.c_uint32)]


def quantize(x: np.ndarray, out: np.ndarray, fmt: int, quant: int, scale: float = 1.0, zero_point: int = 0, fl: int = 0) -> None:
    """Writes real values x into the NPU tensor out (its dtype/quantisation)."""
    if out.dtype == x.dtype and (quant == Q_NONE or (quant == Q_AFFINE and scale == 1.0 and zero_point == 0) or (quant == Q_DFP and fl == 0)):
        np.copyto(out, x)  # nothing to convert
        return
    v = x.astype(np.float32)
    if quant == Q_AFFINE:
        v = np.rint(v / scale + zero_point)
    elif quant == Q_DFP:
        v = np.rint(v * np.float32(2.0 ** fl))
    if fmt == FMT_BFP16:
        np.copyto(out, (v.view(np.uint32) >> 16).astype(np.uint16))
    elif np.issubdtype(out.dtype, np.integer):
        info = np.iinfo(out.dtype)
        np.copyto(out, np.clip(v, info.min, info.max), casting="unsafe")
    else:
        np.copyto(out, v, casting="unsafe")


def dequantize(a: np.ndarray, fmt: int, quant: int, scale: float = 1.0, zero_point: int = 0, fl: int = 0) -> np.ndarray:
    """Returns a float32 copy of an NPU tensor's real values."""
    f = (a.astype(np.uint32) << 16).view(np.float32) if fmt == FMT_BFP16 else a.astype(np.float32)
    if quant == Q_AFFINE:
        f = (f - zero_point) * np.float32(scale)
    elif quant == Q_DFP:
        f *= np.float32(2.0 ** -fl)
    return f


def input_lut(norm: tuple[np.ndarray, np.ndarray], dtype: np.dtype, fmt: int, quant: int,
              scale: float = 1.0, zero_point: int = 0, fl: int = 0) -> np.ndarray:
    """(3, 256) table: pixel value -> normalised, quantised input value for each BGR channel."""
    mean, std = norm
    lut = np.empty((3, 256), dtype=dtype)
    for c in range(3):
        quantize(((np.arange(256) - mean[c]) / std[c]).astype(np.float32), lut[c], fmt, quant, scale, zero_point, fl)
    return lut


def write_input(lut: np.ndarray, x: np.ndarray, out: np.ndarray) -> None:
    """(1, H, W, 3) uint8 RGB -> (1, 3, H, W) BGR planes of the NPU input, through the table."""
    for c in range(3):
        np.take(lut[c], x[0, :, :, 2 - c], out=out[0, c], mode="clip")


class VipError(RuntimeError):
    pass


class VipLite:
    """Process-wide VIPLite runtime (vip_init once; thread-safe per vip_lite.h)."""

    def __init__(self) -> None:
        # VIPLite 2.0 (libNBGlinker + libVIPhal) or 1.13 (libVIPlite + libVIPuser): the API used here is identical.
        # It must match the host's vipcore driver (cat /sys/module/vipcore/version).
        d = os.environ.get("DOCVETA_VIPLITE_DIR") or (os.path.join(APP_DIR, "viplite") if os.path.isdir(os.path.join(APP_DIR, "viplite")) else "")
        lib, errors = None, []
        for runtime, api in (("libVIPhal.so", "libNBGlinker.so"), ("libVIPuser.so", "libVIPlite.so")):
            try:
                C.CDLL(os.path.join(d, runtime), mode=C.RTLD_GLOBAL)
                lib = C.CDLL(os.path.join(d, api))
                self.library = api
                break
            except OSError as e:
                errors.append(str(e))
        if lib is None:
            raise SystemExit(f"cannot load VIPLite ({'; '.join(errors)}). Copy libNBGlinker.so and libVIPhal.so from ai-sdk "
                             "(viplite-tina/lib/aarch64-none-linux-gnu/v2.0) into viplite/ next to the program, "
                             "or set DOCVETA_VIPLITE_DIR")
        lib.vip_get_version.restype = C.c_uint32
        lib.vip_get_buffer_size.restype = C.c_uint32
        lib.vip_map_buffer.restype = C.c_void_p  # everything else returns vip_status_e (int, the ctypes default)
        self.lib = lib
        if not os.path.exists("/dev/vipcore"):
            raise SystemExit("/dev/vipcore not found: the NPU driver is not loaded on the host, or the device is not "
                             "passed into this container (Proxmox: pct set <id> -dev0 /dev/vipcore,mode=0666)")
        self.check(lib.vip_init(), "vip_init")
        self.version = lib.vip_get_version()
        self.cores = 1
        if self.version >= 0x00010601:
            n = C.c_uint32(0)
            self.check(lib.vip_query_hardware(HW_DEVICE_COUNT, C.sizeof(n), C.byref(n)), "query device count")
            counts = (C.c_uint32 * max(1, n.value))()
            self.check(lib.vip_query_hardware(HW_CORE_COUNT, C.sizeof(counts), counts), "query core count")
            self.cores = max(1, counts[0])
        # The driver manages the NPU clock; Allwinner's builds usually refuse user control
        # (vpmdENABLE_USER_CONTROL_POWER), so only ask when the admin sets a value.
        pct = os.environ.get("DOCVETA_NPU_CLOCK_PERCENT")
        if pct and lib.vip_power_management(C.c_uint32(0), POWER_SET_FREQUENCY, C.byref(C.c_uint8(max(1, min(int(pct), 100))))) != 0:
            log.warning("this VIPLite build doesn't allow setting the NPU clock; the driver manages it")

    @staticmethod
    def check(status: int, what: str) -> None:
        if status != 0:
            raise VipError(f"{what} failed (VIPLite status {status})")


class Tensor:
    """One network input/output: a driver buffer mapped once into a numpy view."""

    def __init__(self, vip: VipLite, net: C.c_void_p, index: int, output: bool) -> None:
        lib = vip.lib
        query = lib.vip_query_output if output else lib.vip_query_input

        def q(prop: int, ctype):
            v = ctype(0)
            vip.check(query(net, C.c_uint32(index), prop, C.byref(v)), f"query {'output' if output else 'input'} {index}")
            return v.value

        p = BufferParams()
        p.data_format = q(PROP_FORMAT, C.c_int32)
        p.num_of_dims = q(PROP_NDIM, C.c_uint32)
        vip.check(query(net, C.c_uint32(index), PROP_SIZES, p.sizes), "query sizes")
        p.quant_format = q(PROP_QUANT, C.c_int32)
        self.fmt, self.quant, self.scale, self.zero_point, self.fl = p.data_format, p.quant_format, 1.0, 0, 0
        if self.quant == Q_DFP:
            self.fl = C.c_int8(q(PROP_DFP_POS, C.c_int32) & 0xFF).value  # header documents a vip_uint8_t
            p.quant_data.dfp = self.fl
        elif self.quant == Q_AFFINE:
            self.scale, self.zero_point = q(PROP_SCALE, C.c_float), q(PROP_ZERO_POINT, C.c_int32)
            p.quant_data.affine.scale, p.quant_data.affine.zero_point = self.scale, self.zero_point
        if self.fmt not in NP_DTYPE:
            raise VipError(f"unsupported tensor data format {self.fmt}")
        # VIPLite lists dimensions fastest-first (W, H, C, N); numpy wants (N, C, H, W).
        self.shape = tuple(reversed(p.sizes[:p.num_of_dims]))
        dtype = np.dtype(NP_DTYPE[self.fmt])
        nbytes = int(np.prod(self.shape)) * dtype.itemsize
        self.buf = C.c_void_p()
        vip.check(lib.vip_create_buffer(C.byref(p), C.sizeof(p), C.byref(self.buf)), "vip_create_buffer")
        if lib.vip_get_buffer_size(self.buf) < nbytes:
            raise VipError(f"buffer smaller than tensor {self.shape}: VIPLite library does not match the host driver")
        ptr = lib.vip_map_buffer(self.buf)
        self.array = np.frombuffer((C.c_uint8 * nbytes).from_address(ptr), dtype=dtype).reshape(self.shape)

    def describe(self) -> str:
        q = {Q_AFFINE: f"affine scale={self.scale:.6g} zp={self.zero_point}", Q_DFP: f"dfp fl={self.fl}"}.get(self.quant, "float")
        return f"{self.shape} {self.array.dtype} {q}"


class Net:
    """A prepared NBG network with its I/O buffers. run() is serialised per network."""

    def __init__(self, vip: VipLite, path: str, core: Optional[int], norm: tuple[np.ndarray, np.ndarray]) -> None:
        lib = vip.lib
        self.vip, self.path, self.lock = vip, path, threading.Lock()
        self.net = C.c_void_p()
        vip.check(lib.vip_create_network(path.encode(), C.c_uint32(0), CREATE_FROM_FILE, C.byref(self.net)), f"load {path}")
        if core is not None:
            vip.check(lib.vip_set_network(self.net, NET_SET_CORE_INDEX, C.byref(C.c_uint32(core))), "set core index")
        vip.check(lib.vip_prepare_network(self.net), f"prepare {os.path.basename(path)}")
        n_in, n_out = C.c_uint32(0), C.c_uint32(0)
        vip.check(lib.vip_query_network(self.net, NET_INPUT_COUNT, C.byref(n_in)), "query inputs")
        vip.check(lib.vip_query_network(self.net, NET_OUTPUT_COUNT, C.byref(n_out)), "query outputs")
        self.inputs = [Tensor(vip, self.net, i, False) for i in range(n_in.value)]
        self.outputs = [Tensor(vip, self.net, i, True) for i in range(n_out.value)]
        for i, t in enumerate(self.inputs):
            vip.check(lib.vip_set_input(self.net, C.c_uint32(i), t.buf), "vip_set_input")
        for i, t in enumerate(self.outputs):
            vip.check(lib.vip_set_output(self.net, C.c_uint32(i), t.buf), "vip_set_output")
        # Pixel value -> normalised, quantised input value, per BGR channel: writing a tile is
        # then one table lookup per channel instead of float maths on every pixel.
        t = self.inputs[0]
        if len(t.shape) != 4 or t.shape[1] != 3:
            raise SystemExit(f"{os.path.basename(path)} input is {t.shape}; expected (1, 3, H, W). Re-convert with convert/convert.py")
        self.lut = input_lut(norm, t.array.dtype, t.fmt, t.quant, t.scale, t.zero_point, t.fl)

    def run(self, x: np.ndarray) -> np.ndarray:
        """x: (1, H, W, 3) uint8 RGB. Returns output 0 as float32."""
        lib, t, o = self.vip.lib, self.inputs[0], self.outputs[0]
        with self.lock:
            write_input(self.lut, x, t.array)
            self.vip.check(lib.vip_flush_buffer(t.buf, OPER_FLUSH), "flush input")
            self.vip.check(lib.vip_run_network(self.net), f"run {os.path.basename(self.path)}")
            self.vip.check(lib.vip_flush_buffer(o.buf, OPER_INVALIDATE), "invalidate output")
            return dequantize(o.array, o.fmt, o.quant, o.scale, o.zero_point, o.fl)


class CoreModels:
    """det + all rec networks loaded for one NPU core."""

    def __init__(self, vip: VipLite, model_dir: str, scripts: list[str], core: Optional[int]) -> None:
        self.det = Net(vip, os.path.join(model_dir, "det.nb"), core, DET_NORM)
        shape = self.det.inputs[0].shape
        if len(shape) != 4 or shape[1] != 3 or shape[2] != shape[3]:
            raise SystemExit(f"det.nb input is {shape}; expected (1, 3, S, S). Re-convert with convert/convert.py")
        self.det_size = shape[2]
        self.rec = {s: {w: Net(vip, os.path.join(model_dir, f"rec_{s}_{w}.nb"), core, REC_NORM) for w in ppocr.REC_WIDTHS}
                    for s in scripts}
        self.charsets = {s: ppocr.load_charset(os.path.join(model_dir, f"dict_{s}.txt")) for s in scripts}

    def det_infer(self, x: np.ndarray) -> np.ndarray:  # (1, S, S, 3) uint8 -> (1, 1, S, S)
        return self.det.run(x).reshape(1, 1, self.det_size, self.det_size)

    def rec_infer(self, script: str):
        nets = self.rec[script]

        def infer(x: np.ndarray, bucket: int) -> np.ndarray:  # (1, 48, W, 3) uint8 -> (1, T, C)
            out = nets[bucket].run(x)
            return out.reshape((1,) + out.shape[-2:])

        return infer


class AllwinnerEngine(Engine):
    name = "ppocr-allwinner"

    def __init__(self, vip: Optional[VipLite] = None) -> None:
        files = set(os.listdir(MODELS_DIR)) if os.path.isdir(MODELS_DIR) else set()
        if "det.nb" not in files:
            raise SystemExit(f"No det.nb in {MODELS_DIR}. Convert models with convert/convert.py (README.md) and set DOCVETA_MODELS_DIR.")
        self.scripts = sorted(s for s in set(LANG_SCRIPT.values())
                              if f"dict_{s}.txt" in files and all(f"rec_{s}_{w}.nb" in files for w in ppocr.REC_WIDTHS))
        if not self.scripts:
            raise SystemExit(f"No complete recognition model set in {MODELS_DIR}: need rec_<script>_<w>.nb for w in "
                             f"{ppocr.REC_WIDTHS} plus dict_<script>.txt")
        self.vip = vip or VipLite()
        # One model set per NPU core (A733 has one); several pages in flight so the CPU
        # pre/post-processing of one page overlaps the NPU work of another.
        self.cores = [CoreModels(self.vip, MODELS_DIR, self.scripts, c if self.vip.cores > 1 else None) for c in range(self.vip.cores)]
        self.concurrency = max(1, int(os.environ.get("DOCVETA_CONCURRENCY", self.vip.cores + 1)))
        self.languages = sorted(l for l, s in LANG_SCRIPT.items() if s in self.scripts)
        self.tags = ("npu", "allwinner", "a733")
        self.max_pages_per_task = 20
        try:
            with open(os.path.join(MODELS_DIR, "VERSION"), encoding="utf-8") as f:
                version = f.read().strip()
        except OSError:
            version = "unknown"
        self.version = f"a733/{version}"
        self.models = {"det": "det.nb", **{f"rec_{s}": f"rec_{s}_*.nb" for s in self.scripts}}
        log.info("Allwinner NPU (VIPLite 0x%08x): %d core(s), %d pages in flight, det %dpx, scripts %s, languages %s",
                 self.vip.version, self.vip.cores, self.concurrency, self.cores[0].det_size, self.scripts, self.languages)

    def open_session(self, slot: int) -> CoreModels:
        return self.cores[slot % len(self.cores)]

    def _script(self, languages: list[str]) -> str:
        for l in languages:
            s = LANG_SCRIPT.get(l.split("-")[0].lower())
            if s in self.scripts:
                return s
        return "en" if "en" in self.scripts else self.scripts[0]

    def recognize(self, session: Optional[CoreModels], image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        if session is None:
            raise EngineError("engine_error", "no NPU session", retryable=True)
        img = np.asarray(image.convert("RGB"))
        script = self._script(languages)
        rec, charset = session.rec_infer(script), session.charsets[script]
        try:
            boxes = ppocr.detect(img, session.det_infer, session.det_size)
            lines: list[Line] = []
            for b in boxes:
                r = ppocr.recognize(img, b, rec, charset)
                if r is None or r.confidence < 0.5:
                    continue
                x0, y0, x1, y1 = b.rect
                w = x1 - x0
                words = [Word(t, (x0 + f0 * w, y0, x0 + f1 * w, y1), r.confidence) for t, f0, f1 in r.words]
                lines.append(Line(text=r.text, bbox=(x0, y0, x1, y1), confidence=r.confidence, words=words))
        except VipError as e:
            raise EngineError("engine_error", str(e), retryable=True)
        lang = next((l for l in languages if LANG_SCRIPT.get(l) == script), None)
        return PageResult(lines=lines, language=lang)


def probe() -> None:
    """Loads every model on the NPU, times each one and reads a rendered test image."""
    logging.basicConfig(level="INFO", format="%(message)s")
    vip = VipLite()
    v = vip.version
    print(f"VIPLite {vip.library} {v >> 16}.{v >> 8 & 0xFF}.{v & 0xFF}, {vip.cores} NPU core(s)")  # 0x00MMmmpp
    if not os.path.isfile(os.path.join(MODELS_DIR, "det.nb")):
        print(f"NPU runtime OK; no models in {MODELS_DIR} yet")
        return
    e = AllwinnerEngine(vip)
    m = e.cores[0]
    nets = [m.det] + [n for per in m.rec.values() for n in per.values()]
    for n in nets:
        nn, c, h, w = n.inputs[0].shape
        x = np.full((nn, h, w, c), 255, dtype=np.uint8)
        n.run(x)  # warm-up
        t = time.perf_counter()
        for _ in range(5):
            n.run(x)
        ms = (time.perf_counter() - t) / 5 * 1000
        print(f"{os.path.basename(n.path):24} in {n.inputs[0].describe():50} out {n.outputs[0].describe():50} {ms:7.1f} ms")

    # Speed alone proves little: read back a rendered test image.
    from PIL import ImageDraw, ImageFont

    img = Image.new("RGB", (1240, 400), "white")
    draw = ImageDraw.Draw(img)
    try:
        font = ImageFont.truetype(os.path.join(os.environ.get("DOCVETA_FONTS_DIR", ""), "NotoSans-Regular.ttf"), 44)
    except OSError:
        font = ImageFont.load_default(size=44)
    for i, line in enumerate(["Electricity bill dated 05/08/2026", "Amount due: 1842.00 INR"]):
        draw.text((60, 80 + i * 120), line, fill="black", font=font)
    t = time.perf_counter()
    res = e.recognize(m, img, 1, ["en"])
    print(f"Test image: {len(res.lines)} line(s) in {(time.perf_counter() - t) * 1000:.0f} ms")
    for line in res.lines:
        print(f"  {line.confidence:.2f}  {line.text}")
    if not res.lines:
        raise SystemExit("the NPU ran, but no text was recognised in the test image")
    print("NPU OK")


if __name__ == "__main__":
    if os.path.isdir(os.path.join(APP_DIR, "fonts")):  # Noto fonts shipped in the release package
        os.environ.setdefault("DOCVETA_FONTS_DIR", os.path.join(APP_DIR, "fonts"))
    if "--probe" in sys.argv:
        probe()
    else:
        run(AllwinnerEngine())
