"""Template registry. Each entry maps a rule_id to a draft function.

A template function takes a Pattern and returns a RuleDraft, or None if the
template can't draft for this specific pattern.
"""
from __future__ import annotations

from typing import Callable, Optional

from analysis.types import Pattern, RuleDraft

TemplateFunc = Callable[[Pattern], Optional[RuleDraft]]

REGISTRY: dict[str, TemplateFunc] = {}


def register(rule_id: str, fn: TemplateFunc) -> None:
    """Register a template function for a rule_id."""
    REGISTRY[rule_id] = fn
