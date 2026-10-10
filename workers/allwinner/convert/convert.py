"""Converts the PaddleOCR (PP-OCR) models to NBG (.nb) for the Allwinner A733 NPU: text detection
(det.nb) and reading, one model per script and input width (rec_<script>_<width>.nb).

Two steps, both on an x86_64 Linux PC (not on the board):

1. Fixed-shape ONNX models and calibration images (Python 3.10/3.11 venv; Pillow with libraqm):
       pip install "paddlepaddle==3.2.0" "paddle2onnx==2.1.0" onnx "onnxsim==0.4.36" onnxruntime pyyaml packaging pillow setuptools
       python convert.py onnx --calib-dir ~/Scans --fonts <Noto fonts folder> --work work

2. NBG compilation inside Allwinner's ACUITY Toolkit container (ubuntu-npu:v2.0.10.x):
       docker run --rm -v "$PWD/../..:/workspace" -w /workspace/allwinner/convert ubuntu-npu:v2.0.10.2 \
           python3 convert.py nb --work work --out ../models

Produces <out>/det.nb and <out>/rec_<script>_<width>.nb (dictionaries come with the release's ONNX models).

Choices:
  * Detection: uint8 (asymmetric affine), robust to quantisation and the NPU's fast path.
  * Reading: int16 (dynamic fixed point), PP-OCRv5 rewritten by npu_rewrite.py so the chip
    computes it right (see there): on a Cubie A7A it reads exactly like the CPU, 7x faster.
    The NPU needs fixed shapes, so there's one model per width; the worker splits longer lines.
  * The models expect normalised input. ACUITY uses the mean/std below only to calibrate
    quantisation, and the worker normalises input with a lookup table.
"""

from __future__ import annotations

import argparse
import glob
import os
import shutil
import subprocess
import sys
import tempfile

# Same model source and Paddle→ONNX steps as the Rockchip converter.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "rknn", "convert"))
from convert import DET_MEAN, DET_STD, DET_URL, REC, REC_MEAN, REC_STD, calibration_list, fetch, fix_shape, paddle_to_onnx  # noqa: E402

# A733 = NPU v3 (ai-sdk scripts/pegasus_setup.sh).
TARGET = "VIP9000NANODI_PLUS_PID0X1000003B"
QUANTIZER = {"uint8": ("asymmetric_affine", "uint8"), "int16": ("dynamic_fixed_point", "int16")}
REC_WIDTHS = (320, 640, 960)  # NPU memory grows with width (~24 MB at 320); the worker splits longer lines
HERE = os.path.dirname(os.path.abspath(__file__))


# ---------------------------------------------------------------- step 1: ONNX

def step_onnx(args: argparse.Namespace) -> None:
    import numpy as np
    from PIL import Image

    from synthetic_pages import line_strips
    d = os.path.join(os.path.abspath(args.work), "det")
    os.makedirs(d, exist_ok=True)
    size = args.det_size
    with tempfile.TemporaryDirectory() as tmp:
        print(f"[det] {size}x{size}")
        m = os.path.join(tmp, "det")
        os.makedirs(m)  # paddle_to_onnx globs one folder per model
        onnx = paddle_to_onnx(fetch(args.det_url, os.path.join(tmp, "det.tar")), m, "det")
        shutil.copy(fix_shape(onnx, [1, 3, size, size]), os.path.join(d, "det.onnx"))
        lst = calibration_list(args.calib_dir, size, tmp)
        calib = os.path.join(d, "calib")
        shutil.copytree(os.path.join(tmp, "calib"), calib, dirs_exist_ok=True)
        write_dataset(d, [os.path.join(calib, os.path.basename(p)) for p in open(lst).read().split()])
        for script in args.scripts:
            url, _ = REC[script]
            m = os.path.join(tmp, f"rec_{script}")
            os.makedirs(m)
            onnx = paddle_to_onnx(fetch(url, os.path.join(tmp, f"rec_{script}.tar")), m, f"rec_{script}")
            for w in args.widths:
                name = f"rec_{script}_{w}"
                print(f"[{name}]")
                d = os.path.join(os.path.abspath(args.work), name)
                os.makedirs(os.path.join(d, "calib"), exist_ok=True)
                subprocess.run([sys.executable, os.path.join(HERE, "npu_rewrite.py"), fix_shape(onnx, [1, 3, 48, w]),
                                os.path.join(d, f"{name}.onnx")], check=True)
                images = []
                for i, (strip, _) in enumerate(line_strips(script, w, 200, args.fonts)):
                    images.append(os.path.join(d, "calib", f"{i:03}.png"))
                    # Stored so that loading the file as RGB gives the model's B,G,R order.
                    Image.fromarray(np.ascontiguousarray(strip)).save(images[-1])
                write_dataset(d, images)
    print(f"done: now run step 2 ('nb') inside the ACUITY container with --work {args.work}")


def write_dataset(d: str, images: list[str]) -> None:
    with open(os.path.join(d, "dataset.txt"), "w") as f:
        f.write("\n".join(os.path.relpath(p, d) for p in images) + "\n")


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
        sys.exit("VIV_SDK (or VIVANTE_SDK_DIR) is not set: it should point at the Vivante IDE cmdtools folder in the ACUITY container")
    sh("export", "ovxlib", "--model", model, "--model-data", f"{name}.data", "--dtype", "quantized",
       "--model-quantize", f"{name}_{dtype}.quantize", "--target-ide-project", "linux64",
       "--with-input-meta", f"{name}_inputmeta.yml", "--postprocess-file", f"{name}_postprocess_file.yml",
       "--output-path", f"wksp/{name}_{dtype}/{name}_{dtype}", "--pack-nbg-unify", "--optimize", TARGET, "--viv-sdk", viv_sdk)
    nbs = glob.glob(os.path.join(d, "wksp", "**", "network_binary.nb"), recursive=True)
    if not nbs:
        sys.exit(f"export produced no network_binary.nb for {name}")
    shutil.copy(max(nbs, key=os.path.getmtime), os.path.join(out, f"{name}.nb"))
    shutil.rmtree(os.path.join(d, "wksp"), ignore_errors=True)  # hundreds of MB per model
    print(f"  wrote {os.path.join(out, name)}.nb")


def step_nb(args: argparse.Namespace) -> None:
    out = os.path.abspath(args.out)
    os.makedirs(out, exist_ok=True)
    for d in sorted(glob.glob(os.path.join(os.path.abspath(args.work), "*"))):
        name = os.path.basename(d)
        if not os.path.exists(os.path.join(d, f"{name}.onnx")) or (args.only and name not in args.only):
            continue
        det = name == "det"
        dtype = args.det_dtype if det else args.rec_dtype
        print(f"[{name}] {dtype}")
        build_nb(pegasus(), d, name, DET_MEAN if det else REC_MEAN, DET_STD if det else REC_STD, dtype, out)
    print(f"done: copy {out}/*.nb into the worker's models/ folder on the board")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="step", required=True)
    o = sub.add_parser("onnx", help="step 1: fixed-shape ONNX + calibration images")
    o.add_argument("--calib-dir", required=True, help="folder with ≥20 sample document images (scans/photos like yours)")
    o.add_argument("--det-size", type=int, default=960, help="detection tile size (default 960)")
    o.add_argument("--det-url", default=DET_URL)
    o.add_argument("--fonts", required=True, help="folder with the Noto fonts (NotoSans*, NotoSansDevanagari*, ...) for reading calibration")
    o.add_argument("--scripts", nargs="+", default=["en", "devanagari", "ta", "te", "ka"], choices=sorted(REC))
    o.add_argument("--widths", nargs="+", type=int, default=list(REC_WIDTHS))
    o.add_argument("--work", default="work")
    n = sub.add_parser("nb", help="step 2: compile det.nb (inside the ACUITY container)")
    n.add_argument("--work", default="work")
    n.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "models"))
    n.add_argument("--det-dtype", default="uint8", choices=sorted(QUANTIZER))
    n.add_argument("--rec-dtype", default="int16", choices=sorted(QUANTIZER))
    n.add_argument("--only", nargs="*", help="build only these (det, rec_en_320, ...)")
    args = ap.parse_args()
    (step_onnx if args.step == "onnx" else step_nb)(args)


if __name__ == "__main__":
    main()
