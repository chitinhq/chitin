"""Template for `bounds:max_files_changed`.

Doc-batch detection: if deny reason mentions docs/wiki/graphify-out, propose
a context-aware higher ceiling.
"""
from __future__ import annotations

from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

DOC_KEYWORDS = ("docs/", "wiki/", "README", "graphify-out/")


def _is_doc_batch(reason: str) -> bool:
    if not reason:
        return False
    return any(k in reason for k in DOC_KEYWORDS)


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    doc_count = sum(1 for d in pattern.decisions if _is_doc_batch(d.reason or ""))
    if doc_count == 0:
        return None

    rule_yaml = (
        "rules:\n"
        "  - id: bounds-max-files-doc-batch\n"
        "    when:\n"
        "      action_type: git.push\n"
        "      changed_paths_all_match: '^(docs/|wiki/|graphify-out/)'\n"
        "    bounds:\n"
        "      max_files_changed: 200\n"
        "      max_lines_changed: 10000\n"
        "    reason: 'doc-batch ceiling override (analysis-suggested)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=doc_count,
        would_still_deny=pattern.count - doc_count,
        method="reason-mentions-doc-keyword",
    )
    return RuleDraft(
        kind="heuristic",
        template="bounds_max_files_changed",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Doc-batch detected via reason text; tighten with changed_paths inspection in v2.",
    )


register("bounds:max_files_changed", draft)
