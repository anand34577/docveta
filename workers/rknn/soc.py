"""Rockchip SoC detection and per-SoC NPU runtime profiles (DESIGN §11.7)."""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class SocProfile:
    soc: str               # canonical id used for model directories
    npu_cores: int         # parallel inference contexts we run
    core_masks: tuple      # names of RKNNLite core-mask constants, one per context
    det_size: int          # detection input tile size (square)
    max_pages_per_task: int
    tags: tuple
    model_dirs: tuple      # model directories to try, in order


PROFILES = {
    "rk3588": SocProfile("rk3588", 3, ("NPU_CORE_0", "NPU_CORE_1", "NPU_CORE_2"), 960, 30, ("npu", "rk3588", "fast"), ("rk3588",)),
    "rk3576": SocProfile("rk3576", 2, ("NPU_CORE_0", "NPU_CORE_1"), 960, 20, ("npu", "rk3576", "fast"), ("rk3576",)),
    # RK3566/RK3568 share the same single-core ~1 TOPS NPU and model format.
    "rk3566": SocProfile("rk3566", 1, ("NPU_CORE_AUTO",), 640, 10, ("npu", "rk3566", "low-power"), ("rk3566", "rk3568", "rk356x")),
    "rk3568": SocProfile("rk3568", 1, ("NPU_CORE_AUTO",), 640, 10, ("npu", "rk3568", "low-power"), ("rk3568", "rk3566", "rk356x")),
}


def detect_soc() -> str:
    """Returns rk3588 / rk3576 / rk3566 / rk3568, honouring DOCVETA_RKNN_SOC."""
    forced = os.environ.get("DOCVETA_RKNN_SOC", "").strip().lower()
    if forced:
        if forced not in PROFILES:
            raise SystemExit(f"DOCVETA_RKNN_SOC={forced!r} is not supported; use one of {', '.join(PROFILES)}")
        return forced
    compat = ""
    for path in ("/proc/device-tree/compatible", "/sys/firmware/devicetree/base/compatible"):
        try:
            with open(path, "rb") as f:
                compat = f.read().decode("ascii", "ignore").replace("\x00", " ").lower()
                break
        except OSError:
            continue
    # Order matters: "rk3588s" contains "rk3588".
    for soc in ("rk3588", "rk3576", "rk3568", "rk3566"):
        if soc in compat:
            return soc
    raise SystemExit(
        "Could not detect a supported Rockchip SoC (RK3588/RK3588S, RK3576, RK3566/RK3568) from the device tree. "
        "Set DOCVETA_RKNN_SOC explicitly if you're sure the NPU is available."
    )


def profile(soc: str) -> SocProfile:
    p = PROFILES[soc]
    override = os.environ.get("DOCVETA_CONCURRENCY")
    if override:
        n = max(1, min(int(override), p.npu_cores))
        p = SocProfile(p.soc, n, p.core_masks[:n], p.det_size, p.max_pages_per_task, p.tags, p.model_dirs)
    return p
