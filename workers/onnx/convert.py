"""Builds the ONNX models for the GPU/CPU OCR engine from the official PaddleOCR models.

The release workflow runs this once and ships the result in the docveta-ocr packages;
you only need it to add scripts or refresh models.

    pip install paddlepaddle "paddle2onnx==1.3.1" onnx
    python convert.py --scripts en devanagari ta te ka --out models

Produces <out>/det.onnx, <out>/rec_<script>.onnx + dict_<script>.txt and VERSION. Shapes
stay dynamic (GPUs and CPUs don't need fixed sizes, unlike the Rockchip NPU).
"""

from __future__ import annotations

import argparse
import os
import shutil
import sys
import tempfile

# Same model sources and Paddle→ONNX step as the Rockchip converter.
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "rknn", "convert"))
from convert import DET_URL, REC, fetch, paddle_to_onnx  # noqa: E402


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--scripts", nargs="+", default=["en", "devanagari"], choices=sorted(REC))
    ap.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "models"))
    ap.add_argument("--det-url", default=DET_URL)
    args = ap.parse_args()

    os.makedirs(args.out, exist_ok=True)
    with tempfile.TemporaryDirectory() as work:
        print("[1] detection model")
        det = paddle_to_onnx(fetch(args.det_url, os.path.join(work, "det.tar")), _mk(work, "det"), "det")
        shutil.copy(det, os.path.join(args.out, "det.onnx"))
        for i, script in enumerate(args.scripts, start=2):
            url, dict_url = REC[script]
            print(f"[{i}] recognition model '{script}'")
            rec = paddle_to_onnx(fetch(url, os.path.join(work, f"rec_{script}.tar")), _mk(work, f"rec_{script}"), f"rec_{script}")
            shutil.copy(rec, os.path.join(args.out, f"rec_{script}.onnx"))
            fetch(dict_url, os.path.join(args.out, f"dict_{script}.txt"))
    with open(os.path.join(args.out, "VERSION"), "w", encoding="utf-8") as f:
        f.write("ppocr-v4det+" + "+".join(args.scripts) + "\n")
    print(f"done: {args.out}")


def _mk(work: str, name: str) -> str:
    d = os.path.join(work, name)  # one folder per model: paddle_to_onnx globs for the model dir
    os.makedirs(d)
    return d


if __name__ == "__main__":
    main()
