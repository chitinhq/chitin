"""Research prompt template for default-deny-unknown. Emits a research prompt for operator review."""
from __future__ import annotations
from typing import Optional
from chitin_telemetry.templates import register
from chitin_telemetry.models import Pattern, RuleDraft

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    # For default-deny-unknown, emit a research prompt, not a YAML draft.
    return RuleDraft(
        kind="research-prompt",
        template="default-deny-unknown",
        confidence="low",
        rule_yaml="",
        predicted_impact=None,
        notes="Default-deny-unknown: operator should investigate unknown action/target."
    )

register("default-deny-unknown", draft)
