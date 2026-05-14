"""GPU politeness probe.

Wraps `nvidia-smi` to determine whether the kernel may use the GPU
this tick. Operator's opencode use takes priority — if the GPU is
saturated or VRAM is tight, Argus must defer.
"""
from __future__ import annotations

import subprocess
import time
from dataclasses import dataclass
from pathlib import Path

OPERATOR_ACTIVE_SENTINEL = Path.home() / ".argus" / "operator-active"
OPERATOR_ACTIVE_TTL_SECONDS = 300  # 5min — touched by opencode wrapper


@dataclass(frozen=True)
class GpuStatus:
    available: bool          # safe for Argus to run an LLM call
    reason: str              # explanation if not available
    util_pct: float          # 0..100, -1 if unknown
    vram_free_mib: int       # -1 if unknown
    operator_active: bool    # sentinel file recently touched


def _operator_active() -> bool:
    """True if the operator-active sentinel was touched within TTL."""
    try:
        mtime = OPERATOR_ACTIVE_SENTINEL.stat().st_mtime
        return (time.time() - mtime) < OPERATOR_ACTIVE_TTL_SECONDS
    except FileNotFoundError:
        return False
    except OSError:
        return False


def _query_nvidia_smi() -> tuple[float, int]:
    """Return (utilization_pct, vram_free_mib). (-1, -1) on failure."""
    try:
        result = subprocess.run(
            [
                "nvidia-smi",
                "--query-gpu=utilization.gpu,memory.free",
                "--format=csv,noheader,nounits",
            ],
            capture_output=True,
            text=True,
            timeout=3,
        )
        if result.returncode != 0:
            return -1.0, -1
        line = (result.stdout or "").strip().splitlines()[0]
        parts = [p.strip() for p in line.split(",")]
        if len(parts) < 2:
            return -1.0, -1
        return float(parts[0]), int(parts[1])
    except (FileNotFoundError, subprocess.TimeoutExpired, ValueError, IndexError):
        return -1.0, -1


def status(
    *,
    util_threshold_pct: float = 85.0,
    vram_free_floor_mib: int = 1024,
) -> GpuStatus:
    """Snapshot of GPU state for kernel scheduling.

    Returns `available=False` if:
      - the operator-active sentinel was touched in the last 5 minutes
      - GPU utilization exceeds util_threshold_pct
      - free VRAM is below vram_free_floor_mib
      - nvidia-smi is unreachable (timeout / not installed)
    """
    op_active = _operator_active()
    util, vram_free = _query_nvidia_smi()

    if op_active:
        return GpuStatus(
            available=False,
            reason="operator_active",
            util_pct=util,
            vram_free_mib=vram_free,
            operator_active=True,
        )
    if util < 0 or vram_free < 0:
        return GpuStatus(
            available=False,
            reason="nvidia_smi_unreachable",
            util_pct=util,
            vram_free_mib=vram_free,
            operator_active=False,
        )
    if util > util_threshold_pct:
        return GpuStatus(
            available=False,
            reason=f"gpu_busy({util:.0f}%>{util_threshold_pct:.0f}%)",
            util_pct=util,
            vram_free_mib=vram_free,
            operator_active=False,
        )
    if vram_free < vram_free_floor_mib:
        return GpuStatus(
            available=False,
            reason=f"vram_tight({vram_free}MiB<{vram_free_floor_mib}MiB)",
            util_pct=util,
            vram_free_mib=vram_free,
            operator_active=False,
        )
    return GpuStatus(
        available=True,
        reason="ok",
        util_pct=util,
        vram_free_mib=vram_free,
        operator_active=False,
    )


def touch_operator_active() -> None:
    """Touch the sentinel — for use in the operator's opencode wrapper."""
    OPERATOR_ACTIVE_SENTINEL.parent.mkdir(parents=True, exist_ok=True)
    OPERATOR_ACTIVE_SENTINEL.touch(exist_ok=True)
