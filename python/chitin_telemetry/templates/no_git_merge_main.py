"""Template for no-git-merge-main. Proposes allow on feature branches."""
from __future__ import annotations
from typing import Optional
from chitin_telemetry.templates import register
from chitin_telemetry.models import Pattern, PredictedImpact, RuleDraft

PROTECTED_BRANCHES = frozenset({"main", "master", "production", "release"})
FEATURE_PREFIXES = ("feat/", "fix/", "spike/", "feature/", "bugfix/", "wip/", "draft/")

def _is_safe_branch(target: str) -> bool:
    if not target:
        return False
    branch = target.strip()
    if branch in PROTECTED_BRANCHES:
        return False
    return any(branch.startswith(p) for p in FEATURE_PREFIXES)

def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None
    safe_count = sum(1 for d in pattern.decisions if _is_safe_branch(d.action_target or ""))
    if safe_count == 0:
        return None
    rule_yaml = (
        "# Insert ABOVE the existing no-git-merge-main rule in chitin.yaml.\n"
        "- id: no-git-merge-feature-branches\n"
        "  action: git.merge\n"
        "  effect: allow\n"
        "  target_regex: '^(feat|fix|spike|feature|bugfix|wip|draft)/'\n"
        "  reason: 'Merge allowed on personal/feature branches (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="branch-prefix-match",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_git_merge_main",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Protected branches (main/master/production/release) keep the deny.",
    )

register("no-git-merge-main", draft)
