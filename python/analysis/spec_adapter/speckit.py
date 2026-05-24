"""speckit adapter submodule — wraps the flat module's functional API (spec 061, R3)."""
from __future__ import annotations

from ..speckit_adapter import (
    SpecKitParseError,
    SpecKitDuplicateIDError,
    detect,
    parse,
    parse_tree,
)

__all__ = [
    "SpecKitParseError",
    "SpecKitDuplicateIDError",
    "detect",
    "parse",
    "parse_tree",
]