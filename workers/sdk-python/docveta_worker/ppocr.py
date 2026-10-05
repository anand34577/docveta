"""PP-OCR text detection (DB) and recognition (CTC) pre/post-processing.

Pure numpy/OpenCV so it is unit-testable without an NPU. The actual model calls are
injected as functions, which lets the same code run on RKNN (Rockchip NPU), ONNX
Runtime or anything else.

Shapes follow the RKNN conversion in convert/convert.py:
  det input:  uint8 NHWC (1, S, S, 3) RGB, normalisation baked into the model
  det output: float (1, 1, S, S) text probability map
  rec input:  uint8 NHWC (1, 48, Wb, 3) RGB for bucket widths Wb in REC_WIDTHS
  rec output: float (1, T, C) per-timestep class probabilities (index 0 = CTC blank)
"""

from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Callable, Optional

import cv2
import numpy as np

REC_HEIGHT = 48
REC_WIDTHS = (320, 640, 960, 1280)

# Document language (ISO 639-1) -> PP-OCR recognition model script.
LANG_SCRIPT = {
    "en": "en", "de": "en", "fr": "en", "es": "en", "it": "en", "pt": "en", "nl": "en", "id": "en",
    "hi": "devanagari", "mr": "devanagari", "ne": "devanagari", "sa": "devanagari",
    "ta": "ta", "te": "te", "kn": "ka",
}

# Script -> the language reported when a page turns out to be in that script.
SCRIPT_LANG = {"en": "en", "devanagari": "hi", "ta": "ta", "te": "te", "ka": "kn"}


@dataclass
class TextBox:
    points: np.ndarray  # (4, 2) float32: tl, tr, br, bl in page pixels
    score: float

    @property
    def rect(self) -> tuple[float, float, float, float]:
        xs, ys = self.points[:, 0], self.points[:, 1]
        return float(xs.min()), float(ys.min()), float(xs.max()), float(ys.max())


@dataclass
class RecResult:
    text: str
    confidence: float
    # (word, x0_fraction, x1_fraction) along the crop width, from CTC timesteps
    words: list[tuple[str, float, float]]


# ---------------------------------------------------------------- detection

def order_points(pts: np.ndarray) -> np.ndarray:
    """Orders 4 points as top-left, top-right, bottom-right, bottom-left."""
    pts = pts.astype(np.float32)
    s = pts.sum(axis=1)
    d = np.diff(pts, axis=1).ravel()
    return np.array([pts[np.argmin(s)], pts[np.argmin(d)], pts[np.argmax(s)], pts[np.argmax(d)]], dtype=np.float32)


def db_postprocess(prob: np.ndarray, thresh: float = 0.3, box_thresh: float = 0.6, unclip_ratio: float = 1.6,
                   min_size: float = 3.0, max_candidates: int = 1500) -> list[TextBox]:
    """Extracts text boxes from a DB probability map (H, W) in map coordinates."""
    bitmap = (prob > thresh).astype(np.uint8) * 255
    contours, _ = cv2.findContours(bitmap, cv2.RETR_LIST, cv2.CHAIN_APPROX_SIMPLE)
    boxes: list[TextBox] = []
    h, w = prob.shape
    for c in contours[:max_candidates]:
        if len(c) < 4:
            continue
        (cx, cy), (bw, bh), angle = cv2.minAreaRect(c)
        if min(bw, bh) < min_size:
            continue
        # Box score: mean probability inside the contour.
        x0, y0, cw, ch = cv2.boundingRect(c)
        x1, y1 = min(w, x0 + cw), min(h, y0 + ch)
        mask = np.zeros((y1 - y0, x1 - x0), dtype=np.uint8)
        cv2.fillPoly(mask, [c.reshape(-1, 2) - [x0, y0]], 1)
        if mask.sum() == 0:
            continue
        score = float(cv2.mean(prob[y0:y1, x0:x1], mask)[0])
        if score < box_thresh:
            continue
        # Unclip (DB shrinks text regions during training): expand by area*ratio/perimeter.
        area, perim = bw * bh, 2 * (bw + bh)
        dist = area * unclip_ratio / max(perim, 1e-6)
        bw2, bh2 = bw + 2 * dist, bh + 2 * dist
        if min(bw2, bh2) < min_size + 2:
            continue
        pts = cv2.boxPoints(((cx, cy), (bw2, bh2), angle))
        pts[:, 0] = np.clip(pts[:, 0], 0, w - 1)
        pts[:, 1] = np.clip(pts[:, 1], 0, h - 1)
        boxes.append(TextBox(order_points(pts), score))
    return boxes


def resize_pad(img: np.ndarray, size: int) -> tuple[np.ndarray, float]:
    """Scales img to fit a size×size square (keeping aspect) and pads bottom/right."""
    h, w = img.shape[:2]
    scale = min(size / w, size / h)
    nw, nh = max(1, int(round(w * scale))), max(1, int(round(h * scale)))
    resized = cv2.resize(img, (nw, nh), interpolation=cv2.INTER_AREA if scale < 1 else cv2.INTER_LINEAR)
    out = np.zeros((size, size, 3), dtype=np.uint8)
    out[:nh, :nw] = resized
    return out, scale


def tiles(width: int, height: int, size: int, overlap: int) -> list[tuple[int, int]]:
    """Top-left corners of overlapping size×size tiles covering the page."""
    def axis(n: int) -> list[int]:
        if n <= size:
            return [0]
        step = size - overlap
        pos = list(range(0, n - size, step))
        pos.append(n - size)
        return sorted(set(pos))
    return [(x, y) for y in axis(height) for x in axis(width)]


def detect(img: np.ndarray, infer: Callable[[np.ndarray], np.ndarray], size: int) -> list[TextBox]:
    """Detects text lines on a page image (H, W, 3 RGB uint8).

    Small pages are detected in one pass. Large pages (e.g. A4 at 300 DPI) are first
    scaled so text stays readable for the NPU's fixed input, then processed in
    overlapping tiles; fragments of the same line from neighbouring tiles are merged.
    """
    h, w = img.shape[:2]
    if max(h, w) <= size * 1.4:
        inp, scale = resize_pad(img, size)
        prob = infer(inp[None])[0, 0]
        boxes = db_postprocess(prob)
        for b in boxes:
            b.points /= scale
        return sort_boxes(boxes)

    # Work at a resolution where the long side is ~2.2 tiles.
    work_scale = min(1.0, (size * 2.2) / max(h, w))
    work = cv2.resize(img, (int(w * work_scale), int(h * work_scale)), interpolation=cv2.INTER_AREA) if work_scale < 1 else img
    wh, ww = work.shape[:2]
    overlap = size // 6
    found: list[TextBox] = []
    for x, y in tiles(ww, wh, size, overlap):
        tile = work[y:y + size, x:x + size]
        inp = np.zeros((size, size, 3), dtype=np.uint8)
        inp[:tile.shape[0], :tile.shape[1]] = tile
        prob = infer(inp[None])[0, 0]
        for b in db_postprocess(prob):
            # Drop boxes touching an inner tile edge: the neighbouring tile sees them whole.
            x0, y0, x1, y1 = b.rect
            touches = (x0 <= 1 and x > 0) or (y0 <= 1 and y > 0) or \
                      (x1 >= size - 2 and x + size < ww) or (y1 >= size - 2 and y + size < wh)
            if touches and (x1 - x0) < size * 0.9:
                continue
            b.points[:, 0] += x
            b.points[:, 1] += y
            found.append(b)
    for b in found:
        b.points /= work_scale
    return sort_boxes(merge_boxes(found))


def merge_boxes(boxes: list[TextBox]) -> list[TextBox]:
    """Removes duplicates from overlapping tiles and joins split fragments of a line."""
    boxes = sorted(boxes, key=lambda b: -(b.rect[2] - b.rect[0]) * (b.rect[3] - b.rect[1]))
    kept: list[TextBox] = []
    for b in boxes:
        bx0, by0, bx1, by1 = b.rect
        merged = False
        for i, k in enumerate(kept):
            kx0, ky0, kx1, ky1 = k.rect
            ih = min(by1, ky1) - max(by0, ky0)
            if ih <= 0:
                continue
            vertical = ih / max(1.0, min(by1 - by0, ky1 - ky0))
            gap = max(bx0, kx0) - min(bx1, kx1)
            height = max(by1 - by0, ky1 - ky0)
            if vertical > 0.6 and gap < height * 0.6:
                x0, y0, x1, y1 = min(bx0, kx0), min(by0, ky0), max(bx1, kx1), max(by1, ky1)
                kept[i] = TextBox(np.array([[x0, y0], [x1, y0], [x1, y1], [x0, y1]], dtype=np.float32), max(b.score, k.score))
                merged = True
                break
        if not merged:
            kept.append(b)
    return kept


def sort_boxes(boxes: list[TextBox]) -> list[TextBox]:
    """Reading order: top-to-bottom, then left-to-right within a visual line."""
    boxes = sorted(boxes, key=lambda b: (b.rect[1], b.rect[0]))
    lines: list[list[TextBox]] = []
    for b in boxes:
        y0, y1 = b.rect[1], b.rect[3]
        if lines:
            last = lines[-1][-1]
            ly0, ly1 = last.rect[1], last.rect[3]
            if min(y1, ly1) - max(y0, ly0) > 0.5 * min(y1 - y0, ly1 - ly0):
                lines[-1].append(b)
                continue
        lines.append([b])
    return [b for line in lines for b in sorted(line, key=lambda b: b.rect[0])]


# ---------------------------------------------------------------- recognition

def crop_box(img: np.ndarray, box: TextBox) -> np.ndarray:
    """Perspective-crops a text box into a horizontal strip."""
    tl, tr, br, bl = box.points
    w = int(max(np.linalg.norm(tr - tl), np.linalg.norm(br - bl)))
    h = int(max(np.linalg.norm(bl - tl), np.linalg.norm(br - tr)))
    w, h = max(w, 1), max(h, 1)
    dst = np.array([[0, 0], [w - 1, 0], [w - 1, h - 1], [0, h - 1]], dtype=np.float32)
    m = cv2.getPerspectiveTransform(box.points.astype(np.float32), dst)
    crop = cv2.warpPerspective(img, m, (w, h), borderMode=cv2.BORDER_REPLICATE, flags=cv2.INTER_CUBIC)
    if h > w * 1.5:  # vertical text: rotate to horizontal
        crop = np.rot90(crop)
    return crop


def rec_input(crop: np.ndarray) -> tuple[np.ndarray, int, int]:
    """Resizes a crop to height 48 and pads it to the smallest fitting bucket width.

    Returns (input NHWC uint8, bucket width, content width)."""
    h, w = crop.shape[:2]
    target_w = int(math.ceil(REC_HEIGHT * w / max(h, 1)))
    bucket = next((b for b in REC_WIDTHS if b >= target_w), REC_WIDTHS[-1])
    content_w = min(target_w, bucket)  # very long lines are squeezed into the largest bucket
    resized = cv2.resize(crop, (max(1, content_w), REC_HEIGHT), interpolation=cv2.INTER_LINEAR)
    out = np.zeros((REC_HEIGHT, bucket, 3), dtype=np.uint8)
    out[:, :content_w] = resized
    return out[None], bucket, content_w


REC_MAX_WIDTH = 3200


def rec_input_float(crop: np.ndarray) -> tuple[np.ndarray, float]:
    """Recognition input for engines that run the float models (ONNX Runtime): height 48,
    width padded to a multiple of 160 (few distinct shapes keep GPUs fast), normalised BGR.
    Returns NCHW float input and the share of the width holding the image."""
    h, w = crop.shape[:2]
    tw = min(REC_MAX_WIDTH, max(1, int(math.ceil(REC_HEIGHT * w / max(h, 1)))))
    width = max(160, int(math.ceil(tw / 160)) * 160)
    resized = cv2.resize(crop, (tw, REC_HEIGHT), interpolation=cv2.INTER_LINEAR)
    a = (resized[..., ::-1].astype(np.float32) / 255.0 - 0.5) / 0.5
    out = np.zeros((REC_HEIGHT, width, 3), dtype=np.float32)  # zero = padding, as in PaddleOCR
    out[:, :tw] = a
    return np.ascontiguousarray(out.transpose(2, 0, 1)[None]), tw / width


def ctc_decode(probs: np.ndarray, charset: list[str], content_frac: float = 1.0) -> RecResult:
    """Greedy CTC decode of (T, C) probabilities with character timestep positions.

    content_frac is the share of the input width that contained the image (rest is
    padding) so word positions can be mapped back onto the text box.
    """
    idx = probs.argmax(axis=1)
    conf = probs.max(axis=1)
    T = len(idx)
    chars: list[tuple[str, int, float]] = []
    prev = 0
    for t, k in enumerate(idx):
        if k != 0 and k != prev and 0 < k <= len(charset):
            chars.append((charset[k - 1], t, float(conf[t])))
        prev = k
    text = "".join(c for c, _, _ in chars)
    confidence = float(np.mean([c for _, _, c in chars])) if chars else 0.0

    words: list[tuple[str, float, float]] = []
    cur, start, end = "", None, None
    for ch, t, _ in chars + [(" ", T, 0.0)]:
        if ch.isspace():
            if cur:
                frac = max(content_frac, 1e-6)
                words.append((cur, min(1.0, start / T / frac), min(1.0, (end + 1) / T / frac)))
            cur, start, end = "", None, None
            continue
        if start is None:
            start = t
        cur += ch
        end = t
    return RecResult(text.strip(), confidence, words)


def load_charset(path: str) -> list[str]:
    """PaddleOCR dictionaries list one character per line; a space is appended (use_space_char)."""
    with open(path, encoding="utf-8") as f:
        chars = [line.rstrip("\n").rstrip("\r") for line in f]
    chars = [c for c in chars if c != ""]
    return chars + [" "]


def recognize(img: np.ndarray, box: TextBox, infer: Callable[[np.ndarray, int], np.ndarray], charset: list[str]) -> Optional[RecResult]:
    crop = crop_box(img, box)
    inp, bucket, content_w = rec_input(crop)
    probs = infer(inp, bucket)[0]
    if probs.ndim != 2:
        return None
    res = ctc_decode(probs, charset, content_w / bucket)
    return res if res.text else None


# ---------------------------------------------------------------- script detection

def script_order(languages: list[str], scripts: list[str]) -> list[str]:
    """Available scripts, those of the requested languages first (in order), then the rest."""
    out: list[str] = []
    for l in languages:
        sc = LANG_SCRIPT.get(l.split("-")[0].lower())
        if sc in scripts and sc not in out:
            out.append(sc)
    if not out and "en" in scripts:
        out.append("en")
    out += [sc for sc in scripts if sc not in out]
    return out


def page_language(languages: list[str], script: str) -> Optional[str]:
    """The language to report for a page read with script: a requested one if it matches."""
    for l in languages:
        if LANG_SCRIPT.get(l.split("-")[0].lower()) == script:
            return l
    return SCRIPT_LANG.get(script)


def read_lines(boxes: list[TextBox], read: Callable[[TextBox, str], Optional[RecResult]], languages: list[str],
               scripts: list[str], min_conf: float = 0.5, sample: int = 8, sure: float = 0.85,
               retry_below: float = 0.8, alt_conf: float = 0.75) -> tuple[list[tuple[TextBox, RecResult]], str]:
    """Reads every box and works out the page's script by itself.

    A document's language is only a hint: spaces default to English, so a Hindi letter would
    otherwise be read with the English model and every line dropped as unreadable. The widest
    lines are read with the hinted script first; if it isn't clearly right, with every script,
    and the most confident one reads the page. Lines the page's script reads poorly are tried
    again with the other plausible scripts (bilingual forms mix English and Hindi).

    Returns the (box, result) pairs worth keeping, in box order, and the page's script.
    """
    order = script_order(languages, scripts)
    if not boxes or not order:
        return [], (order[0] if order else "en")
    cache: dict[tuple[int, str], Optional[RecResult]] = {}

    def get(i: int, sc: str) -> Optional[RecResult]:
        if (i, sc) not in cache:
            cache[(i, sc)] = read(boxes[i], sc)
        return cache[(i, sc)]

    def conf(r: Optional[RecResult]) -> float:
        return r.confidence if r is not None else 0.0

    primary, others = order[0], order[1:]
    if others:
        widest = sorted(range(len(boxes)), key=lambda i: boxes[i].rect[0] - boxes[i].rect[2])[:sample]
        score = {sc: 0.0 for sc in order}
        score[primary] = sum(conf(get(i, primary)) for i in widest) / len(widest)
        if score[primary] < sure:
            for sc in others:
                score[sc] = sum(conf(get(i, sc)) for i in widest) / len(widest)
            primary = max(order, key=lambda sc: score[sc])  # ties keep the hinted script (max is stable)
            hinted = {LANG_SCRIPT.get(l.split("-")[0].lower()) for l in languages}
            # Worth retrying weak lines with: requested scripts, ones that read the sample fairly
            # well, and ones that clearly read at least one sampled line (a few Hindi lines).
            clear = {sc for sc in order for i in widest if conf(get(i, sc)) >= alt_conf}
            others = [sc for sc in order if sc != primary and (sc in hinted or sc in clear or score[sc] >= 0.5)]
    # Lines the page's script reads poorly are tried with the others: the English reader turns a
    # Hindi line into confident-looking Latin letters (0.7-0.8), so a low bar would miss it.
    out: list[tuple[TextBox, RecResult]] = []
    for i, b in enumerate(boxes):
        best = get(i, primary)
        if conf(best) < retry_below:
            for sc in others:
                r = get(i, sc)
                # Another script must read the line clearly, or noise turns into foreign text.
                if conf(r) > conf(best) and conf(r) >= alt_conf:
                    best = r
        if best is not None and best.text and best.confidence >= min_conf:
            out.append((b, best))
    return out, primary
