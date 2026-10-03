"""Generates document-like pages for calibrating the NPU detection model.

Calibration only measures value ranges, so realistic layout matters more than real text.
Generated pages make the shipped det.nb reproducible and keep anyone's documents out of it.

    python synthetic_pages.py --out calib --fonts ../../../fonts   # NotoSans*.ttf from the release
"""

from __future__ import annotations

import argparse
import glob
import os
import random

from PIL import Image, ImageDraw, ImageFilter, ImageFont

WORDS = ("invoice total amount date tax paid due account number customer address street road city "
         "state phone email order item quantity rate price discount balance payment receipt bill "
         "electricity water insurance policy premium bank statement transaction credit debit "
         "reference period summary description service charges subtotal grand signature").split()
HINDI = "बिल राशि दिनांक कुल भुगतान ग्राहक पता खाता संख्या विवरण कर सेवा शुल्क रसीद बीमा बैंक".split()


def text_line(rng: random.Random, script: str) -> str:
    if script == "hi":
        return " ".join(rng.choice(HINDI) for _ in range(rng.randint(2, 7)))
    parts = []
    for _ in range(rng.randint(2, 9)):
        r = rng.random()
        if r < 0.2:
            parts.append(f"{rng.randint(1, 99999):,}.{rng.randint(0, 99):02d}")
        elif r < 0.3:
            parts.append(f"{rng.randint(1, 28):02d}/{rng.randint(1, 12):02d}/20{rng.randint(10, 30)}")
        else:
            w = rng.choice(WORDS)
            parts.append(w.capitalize() if rng.random() < 0.3 else w.upper() if rng.random() < 0.05 else w)
    return " ".join(parts)


def page(rng: random.Random, fonts: dict[str, list[str]]) -> Image.Image:
    w, h = 1240, 1754  # A4 at 150 dpi
    tint = rng.randint(235, 255)
    img = Image.new("RGB", (w, h), (tint, tint, rng.randint(tint - 8, tint)))
    d = ImageDraw.Draw(img)

    def font(size: int, script: str = "en"):
        files = fonts.get(script) or fonts.get("en")
        return ImageFont.truetype(rng.choice(files), size) if files else ImageFont.load_default(size=size)

    y = rng.randint(50, 120)
    ink = (rng.randint(0, 60),) * 3
    d.text((rng.randint(60, 400), y), text_line(rng, "en").title(), fill=ink, font=font(rng.randint(30, 48)))
    y += 90
    columns = rng.choice([1, 1, 2])
    while y < h - 120:
        kind = rng.random()
        if kind < 0.15:  # table
            rows, cols = rng.randint(3, 8), rng.randint(3, 5)
            cw, rh, x0 = (w - 160) // cols, rng.randint(36, 50), 80
            for r in range(rows + 1):
                d.line([(x0, y + r * rh), (x0 + cols * cw, y + r * rh)], fill=(150, 150, 150), width=1)
            for c in range(cols + 1):
                d.line([(x0 + c * cw, y), (x0 + c * cw, y + rows * rh)], fill=(150, 150, 150), width=1)
            f = font(rng.randint(14, 20))
            for r in range(rows):
                for c in range(cols):
                    d.text((x0 + c * cw + 8, y + r * rh + 8), text_line(rng, "en")[: cw // 11], fill=ink, font=f)
            y += rows * rh + 40
        else:
            script = "hi" if fonts.get("hi") and rng.random() < 0.25 else "en"
            size = rng.choice([14, 16, 18, 20, 22, 24, 28, 34])
            f = font(size, script)
            for col in range(columns):
                x = 80 + col * (w // columns)
                d.text((x, y), text_line(rng, script)[: (w // columns) // max(6, size // 2)], fill=ink, font=f)
            y += int(size * rng.uniform(1.5, 2.6))
    if rng.random() < 0.5:
        img = img.rotate(rng.uniform(-2, 2), expand=True, fillcolor=(tint, tint, tint))
    if rng.random() < 0.3:
        img = img.filter(ImageFilter.GaussianBlur(rng.uniform(0.3, 0.9)))
    return img


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out", required=True)
    ap.add_argument("--fonts", default="", help="folder with NotoSans-*.ttf and NotoSansDevanagari-*.ttf")
    ap.add_argument("--count", type=int, default=60)
    ap.add_argument("--seed", type=int, default=1)
    args = ap.parse_args()
    fonts = {
        "en": sorted(f for f in glob.glob(os.path.join(args.fonts, "NotoSans-*.ttf"))),
        "hi": sorted(glob.glob(os.path.join(args.fonts, "NotoSansDevanagari-*.ttf"))),
    }
    os.makedirs(args.out, exist_ok=True)
    rng = random.Random(args.seed)
    for i in range(args.count):
        page(rng, fonts).save(os.path.join(args.out, f"page-{i:03}.png"))
    print(f"{args.count} pages in {args.out}")


if __name__ == "__main__":
    main()
