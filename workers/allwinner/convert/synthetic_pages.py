"""Generates document-like pages for calibrating the NPU detection model, and text-line strips
for calibrating (and checking) the NPU reading models.

Calibration only measures value ranges, so realistic layout matters more than real text.
Generated pages make the shipped models reproducible and keep anyone's documents out of them.
Indic text needs Pillow with libraqm (python -c "from PIL import features; print(features.check('raqm'))").

    python synthetic_pages.py --out calib --fonts ../../../fonts   # NotoSans*.ttf from the release
"""

from __future__ import annotations

import argparse
import glob
import math
import os
import random

import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

WORDS = ("invoice total amount date tax paid due account number customer address street road city "
         "state phone email order item quantity rate price discount balance payment receipt bill "
         "electricity water insurance policy premium bank statement transaction credit debit "
         "reference period summary description service charges subtotal grand signature").split()
HINDI = "बिल राशि दिनांक कुल भुगतान ग्राहक पता खाता संख्या विवरण कर सेवा शुल्क रसीद बीमा बैंक".split()


# Sample lines per reader script, for line strips (phrases are cut from them).
LINES = {
    "en": ["Electricity bill dated 05/08/2026", "Amount due: 1842.00 INR", "Customer ID: KA-0912-77341",
           "Invoice No. INV/2026/00458 GSTIN 29ABCDE1234F1Z5", "Total payable (incl. 18% GST) Rs. 12,450.75",
           "Statement period: 01 Apr 2026 to 30 Jun 2026", "email: support@example.co.in Ph: +91 98290 12345",
           "Flat 4B, Sunrise Apartments, MG Road, Pune 411001", "Rechnung Nr. 2026-117 Betrag fällig", "Facture n° 4521 à régler avant échéance"],
    "devanagari": ["विद्युत बिल दिनांक 05/08/2026", "कुल देय राशि ₹ 1,842.00", "उपभोक्ता का नाम: रमेश कुमार शर्मा",
                   "पता: मकान संख्या 12, गांधी नगर, जयपुर", "भारत सरकार आयकर विभाग स्थायी खाता संख्या कार्ड",
                   "कृपया भुगतान अंतिम तिथि से पहले करें", "क्षेत्रीय परिवहन अधिकारी वाहन पंजीकरण प्रमाणपत्र",
                   "कर्मचारी भविष्य निधि संगठन मासिक वेतन पर्ची", "आधार नामांकन संख्या १२३४ ५६७८ ९०१२", "Account No. 1100234567 बैंक खाता विवरण"],
    "ta": ["மின்சார கட்டணம் தேதி 05/08/2026", "மொத்த தொகை ரூ 1,842.00", "வாடிக்கையாளர் பெயர் ராமன்",
           "தமிழ்நாடு அரசு வருவாய் துறை", "பிறந்த தேதி 14/03/1985", "முகவரி சென்னை 600001", "வங்கி கணக்கு விவரம் ஏப்ரல் முதல் ஜூன் வரை"],
    "te": ["విద్యుత్ బిల్లు తేదీ 05/08/2026", "మొత్తం చెల్లించవలసిన మొత్తం 1,842.00", "వినియోగదారుని పేరు రాము",
           "ఆంధ్రప్రదేశ్ ప్రభుత్వం రెవెన్యూ శాఖ", "పుట్టిన తేదీ 14/03/1985", "చిరునామా హైదరాబాద్ 500001", "బ్యాంకు ఖాతా వివరాలు"],
    "ka": ["ವಿದ್ಯುತ್ ಬಿಲ್ ದಿನಾಂಕ 05/08/2026", "ಒಟ್ಟು ಪಾವತಿಸಬೇಕಾದ ಮೊತ್ತ 1,842.00", "ಗ್ರಾಹಕರ ಹೆಸರು ರಾಮು",
           "ಕರ್ನಾಟಕ ಸರ್ಕಾರ ಕಂದಾಯ ಇಲಾಖೆ", "ಹುಟ್ಟಿದ ದಿನಾಂಕ 14/03/1985", "ವಿಳಾಸ ಬೆಂಗಳೂರು 560001", "ಬ್ಯಾಂಕ್ ಖಾತೆ ವಿವರಗಳು"],
}
FONT_FILES = {"en": "NotoSans-*.ttf", "devanagari": "NotoSansDevanagari-*.ttf", "ta": "NotoSansTamil-*.ttf",
              "te": "NotoSansTelugu-*.ttf", "ka": "NotoSansKannada-*.ttf"}


def line_strip(text: str, font_path: str, px: int, width: int, rng: random.Random, height: int = 48) -> np.ndarray | None:
    """A rendered line, prepared as the worker feeds the NPU (docveta_worker.ppocr.rec_input): height 48,
    padded to width with mid-grey. B,G,R channel order (the model's). None if it doesn't fit."""
    f = ImageFont.truetype(font_path, px)
    x0, y0, x1, y1 = ImageDraw.Draw(Image.new("L", (1, 1))).textbbox((0, 0), text, font=f)
    m = max(3, px // 6)  # like a detector box after unclipping
    bg = rng.randint(215, 255)
    img = Image.new("RGB", (x1 - x0 + 2 * m, y1 - y0 + 2 * m), (bg, bg, bg))
    ImageDraw.Draw(img).text((m - x0, m - y0), text, fill=(rng.randint(0, 70),) * 3, font=f)
    if rng.random() < 0.4:
        img = img.filter(ImageFilter.GaussianBlur(rng.uniform(0.3, 1.0)))
    tw = int(math.ceil(height * img.width / img.height))
    if tw > width:
        return None
    out = np.full((height, width, 3), 128, np.uint8)
    out[:, :tw] = np.asarray(img.resize((tw, height), Image.BILINEAR))
    return out[..., ::-1]


def line_strips(script: str, width: int, count: int, fonts_dir: str, seed: int = 1) -> list[tuple[np.ndarray, str]]:
    """count (strip, text) pairs: phrases of LINES[script] (and some English: forms mix them)."""
    rng = random.Random(seed * 1000 + width)
    files = {s: sorted(glob.glob(os.path.join(fonts_dir, FONT_FILES[s]))) for s in (script, "en")}
    if not files[script]:
        raise SystemExit(f"no {FONT_FILES[script]} in {fonts_dir}")
    out = []
    while len(out) < count:
        sc = script if script == "en" or rng.random() < 0.7 else "en"
        words = rng.choice(LINES[sc]).split()
        k = rng.randint(1, len(words))
        i = rng.randint(0, len(words) - k)
        text = " ".join(words[i:i + k])
        if width >= 640 and rng.random() < 0.5:  # wide readers see long lines
            text = " ".join(rng.choice(LINES[sc]) for _ in range(width // 320))
        s = line_strip(text, rng.choice(files[sc]), rng.choice([20, 26, 32, 40]), width, rng)
        if s is not None:
            out.append((s, text))
    return out


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
