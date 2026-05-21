"""Research template for no-shell-sudo. Never proposes sudo allow rules."""
from __future__ import annotations
from typing import Optional
import re
from chitin_telemetry.templates import register
from chitin_telemetry.models import Pattern, PredictedImpact, RuleDraft

PACKAGE_INSTALL_COMMANDS = [
    re.compile(r"sudo\s+(apt-get install|yum install|dnf install|apk add)\s+")
]

def _matches_package_install(target: str) -> bool:
    if not target:
        return False
    return any(p.search(target) for p in PACKAGE_INSTALL_COMMANDS)

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None
    install_count = sum(
        1 for d in pattern.decisions
        if _matches_package_install(d.action_target or "")
    )
    if install_count == 0:
        return None
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=0,
        would_still_deny=pattern.count,
        method="diagnostic-only-package-install-match",
    )
    return RuleDraft(
        kind="research-prompt",
        template="no_shell_sudo",
        confidence="high",
        rule_yaml="",
        predicted_impact=impact,
        notes=(
            "Observed sudo package-install denies. Do not auto-allow privileged "
            "system mutation; operator should review package, host, and task context."
        ),
    )

register("no-shell-sudo", draft)
