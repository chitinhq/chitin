"""Template for no-shell-chmod-777. Proposes allow for temp/test dirs only."""
from __future__ import annotations
from typing import Optional
import re
from analysis.templates import register
from analysis.models import Pattern, PredictedImpact, RuleDraft

SAFE_PATTERNS = [
    re.compile(r"chmod\s+777\s+(?:/tmp|/test|/out|/build|/dist|/node_modules)(?:/|$)")
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
        "# Insert ABOVE the existing no-shell-chmod-777 rule in chitin.yaml.\n"
        "- id: no-shell-chmod-777-safe-dirs\n"
        "  action: shell.exec\n"
        "  effect: allow\n"
        "  target_regex: 'chmod 777 (?:/tmp|/test|/out|/build|/dist|/node_modules)(?:/|$)'\n"
        "  reason: 'chmod 777 allowed on known-temp dirs (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="regex-match-on-action_target",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_shell_chmod_777",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Proposes exemption for chmod 777 in /tmp, /test, /out, /build, /dist, /node_modules.",
    )

register("no-shell-chmod-777", draft)
