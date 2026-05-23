"""spec_adapter — unified spec adapter package (spec 061, T029).

Re-exports the public API from submodules so callers can::

    from analysis.spec_adapter import detect, parse, lookup, all_adapters
    from analysis.spec_adapter import SpecKitParseError, SuperpowersParseError
    from analysis.spec_adapter import ModuleAdapter, ParseError, DuplicateIDError

Or use framework-specific submodules::

    from analysis.spec_adapter import speckit, superpowers
    speckit.detect(path)
    speckit.parse(path)
"""
from __future__ import annotations

from .base import ModuleAdapter, ParseError, DuplicateIDError
from .registry import register, lookup, all_adapters, detect as detect_adapters
from . import speckit as speckit_mod
from . import superpowers as superpowers_mod

# Auto-register concrete adapters on import (T019 / AC6)
from .base import ModuleAdapter as _MA

register(_MA("speckit", "spec-kit", speckit_mod))
register(_MA("superpowers", "superpowers", superpowers_mod))

__all__ = [
    # Base
    "ModuleAdapter",
    "ParseError",
    "DuplicateIDError",
    # Registry
    "register",
    "lookup",
    "all_adapters",
    "detect_adapters",
    # Submodule aliases
    "speckit_mod",
    "superpowers_mod",
]