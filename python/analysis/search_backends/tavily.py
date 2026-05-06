"""Tavily search backend.

API: https://docs.tavily.com/api-reference/endpoint/search
Credential: TAVILY_API_KEY env var (operator brings their own).
"""
from __future__ import annotations

import json
import os
import urllib.error
import urllib.request

from .base import BackendError, SearchResult


class TavilyBackend:
    name = "tavily"

    def __init__(self) -> None:
        key = os.environ.get("TAVILY_API_KEY")
        if not key:
            raise BackendError(
                "TAVILY_API_KEY not set. Get a key at https://tavily.com "
                "and export it in your shell or .env."
            )
        self._key = key

    def query(self, q: str, n: int = 5) -> list[SearchResult]:
        body = json.dumps({
            "api_key": self._key,
            "query": q,
            "max_results": n,
            "search_depth": "basic",
            "include_answer": False,
        }).encode("utf-8")
        req = urllib.request.Request(
            "https://api.tavily.com/search",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            raise BackendError(f"tavily HTTP {e.code}: {e.read()[:200].decode('utf-8', 'replace')}") from e
        except (urllib.error.URLError, TimeoutError) as e:
            raise BackendError(f"tavily network: {e}") from e
        except json.JSONDecodeError as e:
            raise BackendError(f"tavily returned non-JSON: {e}") from e

        out: list[SearchResult] = []
        for r in data.get("results", []) or []:
            out.append({
                "title": r.get("title") or "",
                "url": r.get("url") or "",
                "snippet": r.get("content") or "",
            })
        return out
