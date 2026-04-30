"""Template for the `no-destructive-rm` rule.

Proposes an exemption for known-safe directories.
"""
from __future__ import annotations

import re
from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

SAFE_PATTERNS = [
    re.compile(r"\brm\s+-rf?\s+(?:[^/\s]*?/)?(?:tmp|test|out|graphify-out|build|dist|node_modules)(?:/|$)"),
    re.compile(r"\brm\s+-rf?\s+/tmp/"),
    re.compile(r"\brm\s+-rf?\s+\.\./?(?:tmp|test|out)/"),
]


def _matches_safe(target: str) -> bool:
    if not target:
        return False
    return any(p.search(target) for p in SAFE_PATTERNS)


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    safe_count = sum(1 for d in pattern.decisions if _matches_safe(d.action_target or ""))
    if safe_count == 0:
        return None

    rule_yaml = (
        "rules:\n"
        "  - id: no-destructive-rm-safe-dirs\n"
        "    when:\n"
        "      action_type: shell.exec\n"
        "      action_target_regex: '\\brm\\s+-rf?\\s+(?:[^/\\s]*?/)?(?:tmp|test|out|graphify-out|build|dist|node_modules)(?:/|$)'\n"
        "    decide: allow\n"
        "    reason: 'cleanup of known-temp dirs (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="regex-match-on-action_target",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_destructive_rm",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Proposes exemption for cleanup in /tmp, /test, out/, graphify-out/, build/, dist/, node_modules/.",
    )


register("no-destructive-rm", draft)
