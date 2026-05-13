"""Diagnostic template for envelope-closed denies. Does not draft YAML, only emits a memo for operator review."""
from __future__ import annotations
from typing import Optional
from analysis.templates import register
from analysis.models import Pattern, RuleDraft

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    # Never auto-draft YAML for envelope-closed; emit diagnostic memo only.
    return RuleDraft(
        kind="diagnostic",
        template="envelope-closed",
        confidence="high",
        rule_yaml="",
        predicted_impact=None,
        notes="Envelope-closed denies require human review. No auto-draft."
    )

register("envelope-closed", draft)
