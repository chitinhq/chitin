"""Adapter base class and error type for spec 061 R2.

An adapter is a pure function ``parse(source) -> UnifiedSpec`` plus a
``detect(path) -> bool``.  Adapters are stateless, deterministic, and
side-effect-free.  Registration is a single list (one entry per framework).
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path

from analysis.spec_adapter.types import UnifiedSpec


class SpecAdapterError(Exception):
    """Typed error raised when a spec fails to parse (boundary case 3).

    Carries the source path and the failed section so the caller can
    surface a useful diagnostic without inspecting tracebacks.
    """

    def __init__(self, source_path: str, section: str, detail: str) -> None:
        self.source_path = source_path
        self.section = section
        self.detail = detail
        super().__init__(
            f"SpecAdapterError({source_path!r}, section={section!r}): {detail}"
        )


class Adapter(ABC):
    """Base class for spec-framework adapters (spec 061 R2).

    Subclasses must implement:
    - ``detect(path)`` — return True iff this adapter handles the path.
    - ``parse(source_path)`` — return a fully-populated ``UnifiedSpec``.
    - ``render(spec)`` — render a ``UnifiedSpec`` back to source-format markdown.
    """

    @abstractmethod
    def detect(self, path: str | Path) -> bool:
        """Return True if this adapter can handle the given path."""
        ...

    @abstractmethod
    def parse(self, source_path: str | Path) -> UnifiedSpec:
        """Parse a source file into a UnifiedSpec.

        Raises:
            SpecAdapterError: on malformed input (boundary case 3).
        """
        ...

    def render(self, spec: UnifiedSpec) -> str:
        """Render a UnifiedSpec back to source-format markdown.

        The base implementation raises NotImplementedError; house-format
        adapters MUST override for round-trip integrity (R6).
        """
        raise NotImplementedError(
            f"{type(self).__name__} does not support round-trip rendering"
        )