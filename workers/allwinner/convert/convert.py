"""Converts PaddleOCR (PP-OCR) models to NBG (.nb) for the Allwinner A733 NPU.

Two steps, both on an x86_64 Linux PC (not on the board):

1. Fixed-shape ONNX models and calibration images (a Python venv):
       pip install paddlepaddle "paddle2onnx==1.3.1" onnx "onnxsim==0.4.36" pillow setuptools
       python convert.py onnx --scripts en devanagari --calib-dir ~/Scans --work work

2. NBG compilation inside Allwinner's ACUITY Toolkit container (ubuntu-npu:v2.0.10.x):
       docker run --rm -v "$PWD/../..:/workspace" -w /workspace/allwinner/convert ubuntu-npu:v2.0.10.2 \
           python3 convert.py nb --work work --out ../models

Produces <out>/det.nb, <out>/rec_<script>_<width>.nb, <out>/dict_<script>.txt, <out>/VERSION.

Choices:
  * Detection is uint8 (asymmetric affine): robust to quantisation and the fastest NPU path.
  * Recognition is int16 (dynamic fixed point) by default: close to float accuracy and still
    on the NPU's integer units. --rec-dtype uint8 is faster but raises character errors;
    bf16 is the most accurate and slowest.
  * The NPU needs static shapes: recognition is compiled once per width in REC_WIDTHS.
  * The models expect normalised input. ACUITY uses the mean/std below only to calibrate
    quantisation, and the worker normalises each tile with a lookup table.
"""

from __future__ import annotations

import argparse
import glob
import os
import random
import shutil
import subprocess
import sys
import tempfile

# Same model sources and Paddle→ONNX steps as the Rockchip converter.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "rknn", "convert"))
from convert import DET_MEAN, DET_STD, DET_URL, REC, REC_MEAN, REC_STD, REC_WIDTHS, calibration_list, fetch, fix_shape, paddle_to_onnx  # noqa: E402

# A733 = NPU v3 (ai-sdk scripts/pegasus_setup.sh).
TARGET = "VIP9000NANODI_PLUS_PID0X1000003B"
QUANTIZER = {"uint8": ("asymmetric_affine", "uint8"), "int16": ("dynamic_fixed_point", "int16"), "bf16": ("qbfloat16", "qbfloat16")}


# ---------------------------------------------------------------- step 1: ONNX

def rec_calibration(calib_dir: str, width: int, out_dir: str, count: int = 100) -> None:
    """Text-line-like strips (48×width) cut from the sample documents, for recognition calibration."""
    from PIL import Image

    rng = random.Random(width)
    files = [f for f in glob.glob(os.path.join(calib_dir, "*")) if f.lower().endswith((".png", ".jpg", ".jpeg", ".tif", ".tiff"))]
    for i in range(count):
        img = Image.open(rng.choice(files)).convert("RGB")
        img.thumbnail((1400, 1400))
        h = rng.randint(18, 40)  # typical line height at this scale
        w = min(img.width, int(h * width / 48))
        x, y = rng.randint(0, img.width - w), rng.randint(0, max(0, img.height - h))
        strip = img.crop((x, y, x + w, y + h)).resize((width, 48))
        strip.save(os.path.join(out_dir, f"{i}.png"))


def write_dataset(d: str, images: list[str]) -> None:
    with open(os.path.join(d, "dataset.txt"), "w") as f:
        f.write("\n".join(os.path.relpath(p, d) for p in images) + "\n")


def step_onnx(args: argparse.Namespace) -> None:
    work = os.path.abspath(args.work)
    os.makedirs(work, exist_ok=True)
    size = args.det_size
    with tempfile.TemporaryDirectory() as tmp:
        print(f"[det] {size}x{size}")
        d = os.path.join(work, "det")
        os.makedirs(d, exist_ok=True)
        onnx = paddle_to_onnx(fetch(args.det_url, os.path.join(tmp, "det.tar")), _mk(tmp, "det"), "det")
        shutil.copy(fix_shape(onnx, [1, 3, size, size]), os.path.join(d, "det.onnx"))
        lst = calibration_list(args.calib_dir, size, tmp)
        calib = os.path.join(d, "calib")
        shutil.copytree(os.path.join(tmp, "calib"), calib, dirs_exist_ok=True)
        write_dataset(d, [os.path.join(calib, os.path.basename(p)) for p in open(lst).read().split()])

        for script in args.scripts:
            url, dict_url = REC[script]
            onnx = paddle_to_onnx(fetch(url, os.path.join(tmp, f"rec_{script}.tar")), _mk(tmp, f"rec_{script}"), f"rec_{script}")
            fetch(dict_url, os.path.join(work, f"dict_{script}.txt"))
            for w in REC_WIDTHS:
                name = f"rec_{script}_{w}"
                print(f"[{name}]")
                d = os.path.join(work, name)
                os.makedirs(os.path.join(d, "calib"), exist_ok=True)
                shutil.copy(fix_shape(onnx, [1, 3, 48, w]), os.path.join(d, f"{name}.onnx"))
                rec_calibration(args.calib_dir, w, os.path.join(d, "calib"))
                write_dataset(d, sorted(glob.glob(os.path.join(d, "calib", "*.png"))))
    with open(os.path.join(work, "VERSION"), "w") as f:
        f.write("ppocr-v4det+" + "+".join(args.scripts) + "\n")
    print(f"done: now run step 2 ('nb') inside the ACUITY container with --work {args.work}")


def _mk(tmp: str, name: str) -> str:
    d = os.path.join(tmp, name)  # paddle_to_onnx globs one folder per model
    os.makedirs(d)
    return d


# ---------------------------------------------------------------- step 2: NBG (inside ACUITY)

def pegasus() -> list[str]:
    acuity = os.environ.get("ACUITY_PATH")
    if not acuity:
        sys.exit("ACUITY_PATH is not set: run this step inside the ACUITY Toolkit container (ubuntu-npu:v2.0.10.x)")
    exe = os.path.join(acuity, "pegasus")
    return [exe] if os.path.exists(exe) else ["python3", exe + ".py"]


def set_inputmeta(d: str, name: str, mean: list[float], std: list[float]) -> None:
    """Mean/std for calibration (same approach as ai-sdk scripts/awnet_normalize.py)."""
    from acuitylib.vsi_nn import VSInn

    nn = VSInn()
    net = nn.create_net()
    nn.load_model(net, os.path.join(d, name + ".json"))
    nn.load_model_inputmeta(net, os.path.join(d, name + "_inputmeta.yml"))
    meta = net.get_input_meta()
    port = meta.databases[0].ports[0]
    port.preprocess["reverse_channel"] = False
    port.preprocess["mean"] = list(mean)
    port.preprocess["scale"] = [1.0 / s for s in std]
    net.update_input_meta(meta)
    nn.save_model_inputmeta(net, os.path.join(d, name + "_inputmeta.yml"))


def build_nb(peg: list[str], d: str, name: str, mean, std, dtype: str, out: str) -> None:
    def sh(*a: str) -> None:
        subprocess.run([*peg, *a], cwd=d, check=True)

    for f in glob.glob(os.path.join(d, f"{name}.json")) + glob.glob(os.path.join(d, f"{name}*.quantize*")):
        os.remove(f)
    shutil.rmtree(os.path.join(d, "wksp"), ignore_errors=True)
    sh("import", "onnx", "--model", f"{name}.onnx", "--output-model", f"{name}.json", "--output-data", f"{name}.data")
    sh("generate", "inputmeta", "--model", f"{name}.json", "--separated-database", "--input-meta-output", f"{name}_inputmeta.yml")
    set_inputmeta(d, name, mean, std)
    sh("generate", "postprocess-file", "--model", f"{name}.json", "--postprocess-file-output", f"{name}_postprocess_file.yml")
    quantizer, qtype = QUANTIZER[dtype]
    n = sum(1 for _ in open(os.path.join(d, "dataset.txt")))
    sh("quantize", "--model", f"{name}.json", "--model-data", f"{name}.data", "--device", "CPU",
       "--with-input-meta", f"{name}_inputmeta.yml", "--rebuild", "--model-quantize", f"{name}_{dtype}.quantize",
       "--quantizer", quantizer, "--qtype", qtype, "--iterations", str(n))
    model = f"{name}_{dtype}.quantize.json" if os.path.exists(os.path.join(d, f"{name}_{dtype}.quantize.json")) else f"{name}.json"
    viv_sdk = os.environ.get("VIV_SDK") or os.environ.get("VIVANTE_SDK_DIR")
    if not viv_sdk:
        sys.exit("VIV_SDK (or VIVANTE_SDK_DIR) is not set: it should point at the Vivante IDE vsimulator in the ACUITY container")
    sh("export", "ovxlib", "--model", model, "--model-data", f"{name}.data", "--dtype", "quantized",
       "--model-quantize", f"{name}_{dtype}.quantize", "--target-ide-project", "linux64",
       "--with-input-meta", f"{name}_inputmeta.yml", "--postprocess-file", f"{name}_postprocess_file.yml",
       "--output-path", f"wksp/{name}_{dtype}/{name}_{dtype}", "--pack-nbg-unify", "--optimize", TARGET, "--viv-sdk", viv_sdk)
    nbs = glob.glob(os.path.join(d, "wksp", "**", "network_binary.nb"), recursive=True)
    if not nbs:
        sys.exit(f"export produced no network_binary.nb for {name}")
    shutil.copy(max(nbs, key=os.path.getmtime), os.path.join(out, f"{name}.nb"))
    print(f"  wrote {os.path.join(out, name)}.nb")


def step_nb(args: argparse.Namespace) -> None:
    peg = pegasus()
    work, out = os.path.abspath(args.work), os.path.abspath(args.out)
    os.makedirs(out, exist_ok=True)
    print(f"[det] {args.det_dtype}")
    build_nb(peg, os.path.join(work, "det"), "det", DET_MEAN, DET_STD, args.det_dtype, out)
    for d in sorted(glob.glob(os.path.join(work, "rec_*"))):
        name = os.path.basename(d)
        print(f"[{name}] {args.rec_dtype}")
        build_nb(peg, d, name, REC_MEAN, REC_STD, args.rec_dtype, out)
    for f in glob.glob(os.path.join(work, "dict_*.txt")):
        shutil.copy(f, out)
    with open(os.path.join(work, "VERSION")) as src, open(os.path.join(out, "VERSION"), "w") as dst:
        dst.write(f"{src.read().strip()}/{args.det_dtype}-{args.rec_dtype}\n")
    print(f"done: copy {out} to the board and set DOCVETA_MODELS_DIR to it")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="step", required=True)
    o = sub.add_parser("onnx", help="step 1: fixed-shape ONNX + calibration images")
    o.add_argument("--scripts", nargs="+", default=["en", "devanagari"], choices=sorted(REC))
    o.add_argument("--calib-dir", required=True, help="folder with ≥20 sample document images (scans/photos like yours)")
    o.add_argument("--det-size", type=int, default=960, help="detection tile size (default 960)")
    o.add_argument("--det-url", default=DET_URL)
    o.add_argument("--work", default="work")
    n = sub.add_parser("nb", help="step 2: compile NBG models (inside the ACUITY container)")
    n.add_argument("--work", default="work")
    n.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "models"))
    n.add_argument("--det-dtype", default="uint8", choices=["uint8", "int16"])
    n.add_argument("--rec-dtype", default="int16", choices=sorted(QUANTIZER))
    args = ap.parse_args()
    (step_onnx if args.step == "onnx" else step_nb)(args)


if __name__ == "__main__":
    main()
