"""Diagnostic template for lockdown denies. Does not draft YAML, only emits a memo for operator review."""
from __future__ import annotations
from typing import Optional
from chitin_telemetry.templates import register
from chitin_telemetry.models import Pattern, RuleDraft

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    # Never auto-draft YAML for lockdown; emit diagnostic memo only.
    return RuleDraft(
        kind="diagnostic",
        template="lockdown",
        confidence="high",
        rule_yaml="",
        predicted_impact=None,
        notes="Lockdown denies require human review. No auto-draft."
    )

register("lockdown", draft)
