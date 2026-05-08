"""SearchBackend contract.

Read by any future caller that needs operator-credential web search.
"""
from __future__ import annotations

from typing import Protocol, TypedDict


class SearchResult(TypedDict):
    title: str
    url: str
    snippet: str          # backend-provided summary; may be empty


class BackendError(Exception):
    """Raised on credential / quota / network failures.

    Crucially distinct from "empty result list" — empty means the
    search succeeded but returned nothing; BackendError means the
    backend itself failed and the caller should not treat the
    absence of results as authoritative.
    """


class SearchBackend(Protocol):
    name: str

    def query(self, q: str, n: int = 5) -> list[SearchResult]: ...
