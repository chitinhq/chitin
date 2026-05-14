"""Template registry. Each entry maps a rule_id to a draft function.

A template function takes a Pattern and returns a RuleDraft, or None if the
template can't draft for this specific pattern.

Contract (I7, see SPEC.md):
    - Templates NEVER raise. Decline-by-return-None is the only failure mode.
    - Templates NEVER make a network call (Layer 1 is pure).
    - The returned `RuleDraft.kind` is one of:
        "heuristic"        — heuristic template, proposes YAML.
        "diagnostic"       — surfaces a finding for operator review (no YAML draft).
        "research-prompt"  — emits a research prompt instead of a rule draft.
      The `kind` informs the markdown renderer how to present the draft.
    - `register()` is idempotent: re-importing a template module won't crash,
      but the last registration for a given rule_id wins.

Boundaries:
    - `pattern.decisions` empty → most templates return None defensively.
    - No template registered for a rule_id → `draft.draft_for_pattern`
      returns None; the caller surfaces the pattern under `no_template_patterns`.
"""
from __future__ import annotations

from typing import Callable, Optional

from analysis.models import Pattern, RuleDraft

TemplateFunc = Callable[[Pattern], Optional[RuleDraft]]

REGISTRY: dict[str, TemplateFunc] = {}


def register(rule_id: str, fn: TemplateFunc) -> None:
    """Register a template function for a rule_id."""
    REGISTRY[rule_id] = fn
