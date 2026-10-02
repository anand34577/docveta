"""Docveta OCR worker for Rockchip NPUs: RK3588/RK3588S (3 cores), RK3576 (2 cores) and
RK3566/RK3568 (1 core). Runs PaddleOCR (PP-OCR) detection + recognition models
converted to .rknn for the detected SoC (see convert/convert.py and README.md).

Model layout (per SoC):
    models/<soc>/det.rknn
    models/<soc>/rec_<script>.rknn   + models/<soc>/dict_<script>.txt
where <script> is en, devanagari, ta, te, ka, ...
"""

from __future__ import annotations

import logging
import os
import threading
from typing import Any, Optional

import numpy as np
from PIL import Image

from docveta_worker import ppocr
from docveta_worker import Engine, EngineError, Line, PageResult, Word, run
from soc import detect_soc, profile

log = logging.getLogger("docveta_worker.rknn")

MODELS_DIR = os.environ.get("DOCVETA_MODELS_DIR", os.path.join(os.path.dirname(__file__), "models"))

LANG_SCRIPT = ppocr.LANG_SCRIPT


def _find_model_dir(dirs: tuple[str, ...]) -> str:
    for d in dirs:
        p = os.path.join(MODELS_DIR, d)
        if os.path.isfile(os.path.join(p, "det.rknn")):
            return p
    raise SystemExit(
        f"No models found for this SoC in {MODELS_DIR}/{{{','.join(dirs)}}}/det.rknn. "
        "Convert them with convert/convert.py (see README.md) or mount a models volume."
    )


class Session:
    """One NPU context set (det + rec models) pinned to one NPU core."""

    def __init__(self, model_dir: str, core_mask_name: str, scripts: list[str]):
        from rknnlite.api import RKNNLite  # only available on the board

        self.lock = threading.Lock()
        self.core_mask = getattr(RKNNLite, core_mask_name, None)
        self.det = self._load(RKNNLite, os.path.join(model_dir, "det.rknn"))
        self.rec: dict[str, Any] = {}
        self.charsets: dict[str, list[str]] = {}
        for s in scripts:
            self.rec[s] = self._load(RKNNLite, os.path.join(model_dir, f"rec_{s}.rknn"))
            self.charsets[s] = ppocr.load_charset(os.path.join(model_dir, f"dict_{s}.txt"))

    def _load(self, RKNNLite: Any, path: str) -> Any:
        r = RKNNLite(verbose=False)
        if r.load_rknn(path) != 0:
            raise SystemExit(f"cannot load {path}")
        kwargs = {} if self.core_mask is None or self.core_mask == getattr(RKNNLite, "NPU_CORE_AUTO", None) else {"core_mask": self.core_mask}
        if r.init_runtime(**kwargs) != 0:
            raise SystemExit(
                f"cannot initialise the NPU runtime for {os.path.basename(path)}. Check that /dev/rknpu (or the DRM render node) "
                "is passed to the container and that librknnrt.so matches the kernel's RKNPU driver version."
            )
        return r

    def det_infer(self, x: np.ndarray) -> np.ndarray:
        out = self.det.inference(inputs=[x], data_format="nhwc")
        return np.asarray(out[0], dtype=np.float32)

    def rec_infer(self, script: str):
        model = self.rec[script]

        def infer(x: np.ndarray, _bucket: int) -> np.ndarray:
            out = model.inference(inputs=[x], data_format="nhwc")
            return np.asarray(out[0], dtype=np.float32)

        return infer

    def close(self) -> None:
        for m in [self.det, *self.rec.values()]:
            try:
                m.release()
            except Exception:
                pass


class RKNNEngine(Engine):
    name = "ppocr-rknn"

    def __init__(self) -> None:
        self.soc = detect_soc()
        self.profile = profile(self.soc)
        self.model_dir = _find_model_dir(self.profile.model_dirs)
        self.scripts = sorted(
            f[len("rec_"):-len(".rknn")]
            for f in os.listdir(self.model_dir)
            if f.startswith("rec_") and f.endswith(".rknn") and os.path.isfile(os.path.join(self.model_dir, f"dict_{f[4:-5]}.txt"))
        )
        if not self.scripts:
            raise SystemExit(f"No recognition models (rec_<script>.rknn + dict_<script>.txt) in {self.model_dir}")
        self.languages = sorted(l for l, s in LANG_SCRIPT.items() if s in self.scripts)
        self.tags = self.profile.tags
        self.concurrency = self.profile.npu_cores
        self.max_pages_per_task = self.profile.max_pages_per_task
        version = "unknown"
        try:
            with open(os.path.join(self.model_dir, "VERSION"), encoding="utf-8") as f:
                version = f.read().strip()
        except OSError:
            pass
        self.version = f"{self.soc}/{version}"
        self.models = {"det": "det.rknn", **{f"rec_{s}": f"rec_{s}.rknn" for s in self.scripts}}
        log.info("Rockchip %s: %d NPU context(s), det %dpx, scripts %s, languages %s",
                 self.soc.upper(), self.concurrency, self.profile.det_size, self.scripts, self.languages)

    def open_session(self, slot: int) -> Session:
        mask = self.profile.core_masks[slot % len(self.profile.core_masks)]
        return Session(self.model_dir, mask, self.scripts)

    def close_session(self, session: Session) -> None:
        session.close()

    def _script(self, languages: list[str]) -> str:
        for l in languages:
            s = LANG_SCRIPT.get(l.split("-")[0].lower())
            if s in self.scripts:
                return s
        if "en" in self.scripts:
            return "en"
        return self.scripts[0]

    def recognize(self, session: Optional[Session], image: Image.Image, page_no: int, languages: list[str]) -> PageResult:
        if session is None:
            raise EngineError("engine_error", "no NPU session", retryable=True)
        img = np.asarray(image.convert("RGB"))
        script = self._script(languages)
        rec = session.rec_infer(script)
        charset = session.charsets[script]
        with session.lock:
            boxes = ppocr.detect(img, session.det_infer, self.profile.det_size)
            lines: list[Line] = []
            for b in boxes:
                r = ppocr.recognize(img, b, rec, charset)
                if r is None or r.confidence < 0.5:
                    continue
                x0, y0, x1, y1 = b.rect
                w = x1 - x0
                words = [Word(t, (x0 + f0 * w, y0, x0 + f1 * w, y1), r.confidence) for t, f0, f1 in r.words]
                lines.append(Line(text=r.text, bbox=(x0, y0, x1, y1), confidence=r.confidence, words=words))
        lang = next((l for l in languages if LANG_SCRIPT.get(l) == script), None)
        return PageResult(lines=lines, language=lang)


if __name__ == "__main__":
    run(RKNNEngine())
