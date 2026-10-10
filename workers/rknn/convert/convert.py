"""Converts PaddleOCR (PP-OCR) models to .rknn for Rockchip NPUs.

Runs on an x86_64 Linux machine (RKNN-Toolkit2 is x86-only), NOT on the board.

    pip install rknn-toolkit2 "paddlepaddle==3.2.0" "paddle2onnx==2.1.0" onnx "onnxsim==0.4.36" pyyaml packaging
    python convert.py --soc rk3588 --scripts en devanagari --calib-dir ./calib --out ../models
    python convert.py --soc rk3576 ...
    python convert.py --soc rk3566 ...      # also used on RK3568

Produces:
    <out>/<soc>/det.rknn
    <out>/<soc>/rec_<script>.rknn, <out>/<soc>/dict_<script>.txt
    <out>/<soc>/VERSION

Choices (see README.md for the reasoning):
  * Detection is INT8-quantised with a calibration set of real document crops: it is
    robust to quantisation and becomes much faster on the NPU.
  * Recognition stays FP16 by default: INT8 noticeably increases character errors.
  * Recognition is converted with several fixed input widths (dynamic_input), because
    NPUs need static shapes; the worker picks the smallest width that fits each line.
"""

from __future__ import annotations

import argparse
import glob
import os
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

PADDLE = "https://paddleocr.bj.bcebos.com"
PADDLE3 = "https://paddle-model-ecology.bj.bcebos.com/paddlex/official_inference_model/paddle3.0.0"
DICT_BASE = "https://raw.githubusercontent.com/PaddlePaddle/PaddleOCR/release/2.7/ppocr/utils"

# Model sources. Override with --det-url if Paddle moves them.
# The v4 detector still finds lines better on documents than PP-OCRv5_mobile_det, and the
# compiled Allwinner det.nb is built from it.
DET_URL = f"{PADDLE}/PP-OCRv4/chinese/ch_PP-OCRv4_det_infer.tar"  # multilingual-capable text detector
# Script -> (model, dictionary). A None dictionary is read from the model's inference.yml (PP-OCRv5).
# PP-OCRv5 readers: Hindi lines went from ~36% to ~2% character errors (v3 lost the spaces and
# the ि sign), and phone-photo English from ~5% to ~0.1%. "en" is the Latin model: it also reads
# the accents of German, French, Spanish… Kannada has no v5 model yet.
REC = {
    "en": (f"{PADDLE3}/latin_PP-OCRv5_mobile_rec_infer.tar", None),
    "devanagari": (f"{PADDLE3}/devanagari_PP-OCRv5_mobile_rec_infer.tar", None),
    "ta": (f"{PADDLE3}/ta_PP-OCRv5_mobile_rec_infer.tar", None),
    "te": (f"{PADDLE3}/te_PP-OCRv5_mobile_rec_infer.tar", None),
    "ka": (f"{PADDLE}/PP-OCRv3/multilingual/ka_PP-OCRv3_rec_infer.tar", f"{DICT_BASE}/dict/ka_dict.txt"),
}
MODELS_VERSION = "ppocr-v4det+v5rec"

DET_SIZE = {"rk3588": 960, "rk3576": 960, "rk3566": 640, "rk3568": 640}
REC_WIDTHS = (320, 640, 960, 1280)
# ImageNet normalisation baked into the det model; rec uses (x-127.5)/127.5.
DET_MEAN, DET_STD = [123.675, 116.28, 103.53], [58.395, 57.12, 57.375]
REC_MEAN, REC_STD = [127.5, 127.5, 127.5], [127.5, 127.5, 127.5]


def fetch(url: str, dest: str) -> str:
    print(f"  downloading {url}")
    urllib.request.urlretrieve(url, dest)
    return dest


def model_dir(work: str) -> str:
    return next(d for d in glob.glob(os.path.join(work, "*")) if os.path.isdir(d) and os.path.exists(os.path.join(d, "inference.pdiparams")))


def paddle_to_onnx(tar_path: str, work: str, name: str) -> str:
    """Paddle inference model (old .pdmodel or PaddlePaddle 3 .json) -> ONNX. Needs paddle2onnx 2.x."""
    with tarfile.open(tar_path) as t:
        t.extractall(work)
    d = model_dir(work)
    graph = "inference.json" if os.path.exists(os.path.join(d, "inference.json")) else "inference.pdmodel"
    out = os.path.join(work, f"{name}.onnx")
    subprocess.run([  # via this interpreter: works without paddle2onnx on PATH (venvs, Windows)
        sys.executable, "-c", "import sys; from paddle2onnx.command import main; sys.exit(main())", "--model_dir", d, "--model_filename", graph,
        "--params_filename", "inference.pdiparams", "--save_file", out, "--opset_version", "12",
        "--enable_onnx_checker", "True",
    ], check=True)
    return out


def rec_dict(work: str, dict_url: str | None, dest: str) -> None:
    """The reader's character list: downloaded, or from the model's inference.yml (PP-OCRv5)."""
    if dict_url:
        fetch(dict_url, dest)
        return
    import yaml

    with open(os.path.join(model_dir(work), "inference.yml"), encoding="utf-8") as f:
        chars = yaml.safe_load(f)["PostProcess"]["character_dict"]
    with open(dest, "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(chars) + "\n")


def fix_shape(onnx_path: str, shape: list[int]) -> str:
    """Pins the input shape and simplifies the graph (RKNN prefers static graphs)."""
    import onnx
    from onnxsim import simplify

    model = onnx.load(onnx_path)
    model, ok = simplify(model, overwrite_input_shapes={model.graph.input[0].name: shape})
    if not ok:
        raise RuntimeError(f"onnxsim failed for {onnx_path}")
    # Newer onnxsim releases (0.7.x) emit inconsistent recognition graphs: catch that here,
    # not as a confusing error in the NPU toolkit.
    try:
        onnx.shape_inference.infer_shapes(model, strict_mode=True)
    except Exception as e:
        raise RuntimeError(f"onnxsim produced an invalid graph for {onnx_path} ({e}); install onnxsim==0.4.36") from e
    out = onnx_path.replace(".onnx", f"_{'x'.join(map(str, shape))}.onnx")
    onnx.save(model, out)
    return out


def build_rknn(onnx_path: str, out: str, soc: str, mean, std, quantize: bool, dataset: str | None, dynamic=None, input_size=None) -> None:
    from rknn.api import RKNN

    rknn = RKNN(verbose=False)
    cfg = dict(mean_values=[mean], std_values=[std], target_platform=soc, optimization_level=3)
    if dynamic:
        cfg["dynamic_input"] = dynamic
    rknn.config(**cfg)
    kwargs = {}
    if input_size:
        kwargs["input_size_list"] = [input_size]
    if rknn.load_onnx(model=onnx_path, **kwargs) != 0:
        raise RuntimeError(f"load_onnx failed: {onnx_path}")
    if rknn.build(do_quantization=quantize, dataset=dataset) != 0:
        raise RuntimeError(f"build failed: {onnx_path}")
    if rknn.export_rknn(out) != 0:
        raise RuntimeError(f"export failed: {out}")
    rknn.release()
    print(f"  wrote {out}")


def calibration_list(calib_dir: str, size: int, work: str) -> str:
    """Prepares det calibration images (document crops resized like the worker does)."""
    from PIL import Image

    files = [f for f in glob.glob(os.path.join(calib_dir, "*")) if f.lower().endswith((".png", ".jpg", ".jpeg", ".tif", ".tiff"))]
    if len(files) < 20:
        sys.exit(f"--calib-dir needs at least 20 document images (scans/photos similar to yours); found {len(files)}")
    out_dir = os.path.join(work, "calib")
    os.makedirs(out_dir, exist_ok=True)
    lines = []
    for i, f in enumerate(files[:200]):
        img = Image.open(f).convert("RGB")
        img.thumbnail((size, size))
        canvas = Image.new("RGB", (size, size))
        canvas.paste(img, (0, 0))
        p = os.path.join(out_dir, f"{i}.png")
        canvas.save(p)
        lines.append(p)
    lst = os.path.join(work, "calib.txt")
    with open(lst, "w") as fh:
        fh.write("\n".join(lines))
    return lst


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--soc", required=True, choices=sorted(DET_SIZE))
    ap.add_argument("--scripts", nargs="+", default=["en", "devanagari"], choices=sorted(REC))
    ap.add_argument("--calib-dir", required=True, help="folder with ≥20 sample document images for INT8 calibration")
    ap.add_argument("--out", default=os.path.join(os.path.dirname(__file__), "..", "models"))
    ap.add_argument("--det-url", default=DET_URL)
    args = ap.parse_args()

    size = DET_SIZE[args.soc]
    target = os.path.join(args.out, args.soc)
    os.makedirs(target, exist_ok=True)
    with tempfile.TemporaryDirectory() as work:
        print(f"[1/3] detection model for {args.soc} ({size}x{size}, INT8)")
        det_onnx = paddle_to_onnx(fetch(args.det_url, os.path.join(work, "det.tar")), os.path.join(work, "det"), "det")
        det_fixed = fix_shape(det_onnx, [1, 3, size, size])
        dataset = calibration_list(args.calib_dir, size, work)
        build_rknn(det_fixed, os.path.join(target, "det.rknn"), args.soc, DET_MEAN, DET_STD, True, dataset)

        for i, script in enumerate(args.scripts, start=2):
            url, dict_url = REC[script]
            print(f"[{i}/{len(args.scripts) + 1}] recognition model '{script}' (FP16, widths {REC_WIDTHS})")
            rec_work = os.path.join(work, f"rec_{script}")
            rec_onnx = paddle_to_onnx(fetch(url, os.path.join(work, f"rec_{script}.tar")), rec_work, f"rec_{script}")
            dynamic = [[[1, 3, 48, w]] for w in REC_WIDTHS]
            build_rknn(rec_onnx, os.path.join(target, f"rec_{script}.rknn"), args.soc, REC_MEAN, REC_STD, False, None, dynamic=dynamic)
            rec_dict(rec_work, dict_url, os.path.join(target, f"dict_{script}.txt"))

        with open(os.path.join(target, "VERSION"), "w") as f:
            f.write(MODELS_VERSION + "+" + "+".join(args.scripts) + "\n")
    shutil.rmtree(os.path.join(target, "__pycache__"), ignore_errors=True)
    print(f"done: copy {target} to the board's models/{args.soc}/ (or mount it into the worker container)")


if __name__ == "__main__":
    main()
