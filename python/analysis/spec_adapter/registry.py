"""Adapter registry — mirrors Go registry (spec 061, R2/T006)."""
from __future__ import annotations

from pathlib import Path
from typing import Union

from .base import ModuleAdapter

# Global registry: framework name -> ModuleAdapter
_REGISTRY: dict[str, ModuleAdapter] = {}


def register(adapter: ModuleAdapter) -> None:
    """Register an adapter. Adding a new framework is one call."""
    key = adapter.framework()
    if key in _REGISTRY:
        raise ValueError(f"Adapter already registered for framework: {key}")
    _REGISTRY[key] = adapter


def lookup(framework: str) -> ModuleAdapter | None:
    """Look up an adapter by framework name."""
    return _REGISTRY.get(framework)


def all_adapters() -> tuple[ModuleAdapter, ...]:
    """Return all registered adapters."""
    return tuple(_REGISTRY.values())


def detect(path: Union[str, Path]) -> ModuleAdapter | None:
    """Return the first adapter whose detect() matches, or None."""
    for adapter in _REGISTRY.values():
        if adapter.detect(path):
            return adapter
    return None