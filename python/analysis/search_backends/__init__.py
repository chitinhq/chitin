"""Pluggable web-search backends for stale-seed refresh.

The operator picks one in chitin.yaml under `seeds.web_search.backend`;
each backend reads its own credentials from env. The contract is one
method (`query`) that returns top-N results or raises BackendError on
credential / quota / network failure (so empty-results never silently
masquerade as broken-backend).

Add a backend by:
  1. Creating <name>.py with a class that implements SearchBackend
  2. Registering it in REGISTRY below
"""
from __future__ import annotations

from .base import BackendError, SearchBackend, SearchResult
from .tavily import TavilyBackend

REGISTRY: dict[str, type[SearchBackend]] = {
    "tavily": TavilyBackend,
}


def get_backend(name: str) -> SearchBackend:
    if name not in REGISTRY:
        raise BackendError(
            f"unknown search backend: {name!r}. "
            f"available: {sorted(REGISTRY)}"
        )
    return REGISTRY[name]()


__all__ = ["BackendError", "SearchBackend", "SearchResult", "get_backend", "REGISTRY"]
