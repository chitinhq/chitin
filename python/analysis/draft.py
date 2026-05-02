"""Dispatch a Pattern to its template, returning a RuleDraft or None."""
from __future__ import annotations

from typing import Optional

from analysis.templates import REGISTRY
from analysis.models import Pattern, RuleDraft


def draft_for_pattern(pattern: Pattern) -> Optional[RuleDraft]:
    """Look up a template by rule_id and produce a draft.

    Returns None if no template is registered for this rule_id, OR if the
    template returns None (heuristic declined this specific pattern).
    """
    fn = REGISTRY.get(pattern.rule_id)
    if fn is None:
        return None
    return fn(pattern)


def reason_no_template(pattern: Pattern) -> str:
    """Human-readable explanation for why no draft was generated."""
    if pattern.rule_id not in REGISTRY:
        return f"no template registered for rule_id={pattern.rule_id!r}"
    return f"template for {pattern.rule_id!r} declined this pattern"
