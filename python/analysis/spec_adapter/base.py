"""Base adapter interface — mirrors Go SpecAdapter (spec 061, R2).

Python uses a functional adapter pattern (module-level detect/parse) rather
than class instances. The registry wraps modules to present a uniform API.
"""
from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Union

from ..unified_spec import UnifiedSpec


class ParseError(Exception):
    """Typed error for adapter parse failures."""

    def __init__(self, path: str, section: str, detail: str) -> None:
        self.path = path
        self.section = section
        self.detail = detail
        super().__init__(f"Parse error in {path} (section {section}): {detail}")


class DuplicateIDError(Exception):
    """Raised when a parsed spec contains duplicate requirement or AC IDs."""

    def __init__(self, kind: str, id_: str) -> None:
        self.kind = kind
        self.id = id_
        super().__init__(f"Duplicate {kind} ID: {id_}")


class ModuleAdapter:
    """Wraps a module with detect()/parse() functions as a SpecAdapter-like object."""

    def __init__(self, name: str, framework: str, module) -> None:
        self._name = name
        self._framework = framework
        self._module = module

    def detect(self, path: Union[str, Path]) -> bool:
        return self._module.detect(path)

    def parse(self, path: Union[str, Path]) -> UnifiedSpec:
        return self._module.parse(path)

    def framework(self) -> str:
        return self._framework