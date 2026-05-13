"""Diagnostic template for router-heuristic:deny. Does not draft YAML, only emits a memo for operator review."""
from __future__ import annotations
from typing import Optional
from analysis.templates import register
from analysis.models import Pattern, RuleDraft

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    # Never auto-draft YAML for router-heuristic:deny; emit diagnostic memo only.
    return RuleDraft(
        kind="diagnostic",
        template="router-heuristic:deny",
        confidence="high",
        rule_yaml="",
        predicted_impact=None,
        notes="Router-heuristic:deny requires human review. No auto-draft."
    )

register("router-heuristic:deny", draft)
