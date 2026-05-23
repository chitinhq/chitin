"""superpowers adapter submodule — wraps the flat module's functional API (spec 061, R5)."""
from __future__ import annotations

from ..superpowers_adapter import (
    SuperpowersParseError,
    detect,
    parse,
)

__all__ = [
    "SuperpowersParseError",
    "detect",
    "parse",
]