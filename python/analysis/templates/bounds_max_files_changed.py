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

    # The kernel's Bounds (gov/policy.go) is GLOBAL — there is no per-rule
    # bounds override in v1 schema. So this template suggests raising the
    # global ceiling, which has broader effect than just doc-batches. Per-rule
    # bounds with a path predicate would require a kernel change (Issue #70).
    rule_yaml = (
        "# Replace the top-level `bounds:` block in chitin.yaml.\n"
        "# WARNING: Bounds is global — this raises the ceiling for ALL git.push,\n"
        "# not just doc-batches. Per-rule bounds with changed_paths predicate\n"
        "# requires kernel work (see Issue #70).\n"
        "bounds:\n"
        "  max_files_changed: 200\n"
        "  max_lines_changed: 10000\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=doc_count,
        would_still_deny=pattern.count - doc_count,
        method="reason-mentions-doc-keyword (global Bounds raise — broader than doc-only)",
    )
    return RuleDraft(
        kind="heuristic",
        template="bounds_max_files_changed",
        confidence="low",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes=(
            "Global Bounds raise. Doc-batch detected via reason text; "
            "for per-path-predicate bounds, the kernel needs a Rule.Bounds "
            "field (Issue #70). Confidence is low because the global raise "
            "affects all git.push actions, not just doc batches."
        ),
    )


register("bounds:max_files_changed", draft)
