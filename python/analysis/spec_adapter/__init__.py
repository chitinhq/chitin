"""spec_adapter — unified spec model + framework adapters (spec 061 R1/R2).

Public API:

    Types (analysis.spec_adapter.types):
        UnifiedSpec         — the normalized spec shape
        Requirement         — R1, R2, ...
        AcceptanceCriterion — AC1, AC2, ...
        Slice               — implementation slice
        Question            — open question

    Adapter (analysis.spec_adapter.adapter):
        Adapter             — abstract base class
        SpecAdapterError    — typed parse-error

    Registry (analysis.spec_adapter.registry):
        register(adapter)   — one-entry registration (AC6)
        detect(path)        — route to the right adapter (AC3)
        parse(source_path)  — detect + parse in one call
        adapters()          — list registered adapters
        reset()             — test helper

    spec-kit adapter (analysis.spec_adapter.speckit_adapter):
        SpecKitAdapter      — house-format adapter (R3)
"""
from __future__ import annotations

__version__ = "0.1.0"

from analysis.spec_adapter.types import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    UnifiedSpec,
)
from analysis.spec_adapter.adapter import Adapter, SpecAdapterError
from analysis.spec_adapter.speckit_adapter import SpecKitAdapter
from analysis.spec_adapter.registry import register, detect, parse, adapters, reset

# Auto-register the spec-kit adapter on import
_house_adapter = SpecKitAdapter()
register(_house_adapter)

__all__ = [
    "__version__",
    # types
    "UnifiedSpec",
    "Requirement",
    "AcceptanceCriterion",
    "Slice",
    "Question",
    # adapter base
    "Adapter",
    "SpecAdapterError",
    # house adapter
    "SpecKitAdapter",
    # registry
    "register",
    "detect",
    "parse",
    "adapters",
    "reset",
]