"""Template for no-shell-sudo. Proposes allow for known-safe commands or test envs."""
from __future__ import annotations
from typing import Optional
import re
from analysis.templates import register
from analysis.models import Pattern, PredictedImpact, RuleDraft

SAFE_COMMANDS = [
    re.compile(r"sudo\s+(apt-get install|yum install|dnf install|apk add)\s+")
]

def _matches_safe(target: str) -> bool:
    if not target:
        return False
    return any(p.search(target) for p in SAFE_COMMANDS)

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None
    safe_count = sum(1 for d in pattern.decisions if _matches_safe(d.action_target or ""))
    if safe_count == 0:
        return None
    rule_yaml = (
        "# Insert ABOVE the existing no-shell-sudo rule in chitin.yaml.\n"
        "- id: no-shell-sudo-safe-commands\n"
        "  action: shell.exec\n"
        "  effect: allow\n"
        "  target_regex: 'sudo (apt-get install|yum install|dnf install|apk add) '"\n"
        "  reason: 'sudo allowed for known-safe install commands (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="regex-match-on-action_target",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_shell_sudo",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Proposes exemption for sudo install commands only.",
    )

register("no-shell-sudo", draft)
