"""Adapter registry — single list so a new framework is one entry (R2/AC6).

The registry is the *only* place adapters are listed.  Adding a new
adapter means appending to ``_ADAPTERS`` and re-exporting; downstream
code never branches on framework.
"""
from __future__ import annotations

from pathlib import Path
from typing import List, Optional

from analysis.spec_adapter.adapter import Adapter, SpecAdapterError
from analysis.spec_adapter.types import UnifiedSpec


# ── Registry state ─────────────────────────────────────────────────────────────

_ADAPTERS: List[Adapter] = []


def register(adapter: Adapter) -> None:
    """Register an adapter.  Idempotent — adding twice is a no-op."""
    if adapter not in _ADAPTERS:
        _ADAPTERS.append(adapter)


def unregister(adapter: Adapter) -> None:
    """Remove an adapter from the registry."""
    _ADAPTERS.remove(adapter)  # ValueError if absent — caller error


def adapters() -> List[Adapter]:
    """Return a shallow copy of the current adapter list."""
    return list(_ADAPTERS)


def detect(path: str | Path) -> Optional[Adapter]:
    """Return the adapter that handles *path*, or None.

    If multiple adapters claim *path*, the *first* registered wins.
    If none claim it, returns None (boundary case 4).
    """
    for adapter in _ADAPTERS:
        if adapter.detect(path):
            return adapter
    return None


def parse(source_path: str | Path) -> UnifiedSpec:
    """Detect the right adapter and parse.

    Raises:
        SpecAdapterError: on malformed input.
        ValueError: if no adapter handles *source_path*.
    """
    adapter = detect(source_path)
    if adapter is None:
        raise ValueError(
            f"No adapter registered for {source_path!r}. "
            f"Registered: {[type(a).__name__ for a in _ADAPTERS]}"
        )
    return adapter.parse(source_path)


def reset() -> None:
    """Clear the registry (test helper)."""
    _ADAPTERS.clear()