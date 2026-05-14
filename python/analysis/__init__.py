"""chitin analysis layer — decisions, debt, soul-routing, and swarm-run streams.

The read side of the chitin loop. Loads what the kernel wrote, detects
patterns, drafts candidate rules, emits JSON + markdown reports.

Public API:

    Models (analysis.models):
        Decision              — one parsed gov-decisions row
        Pattern               — a repeat (rule_id, action_type, agent_id) bucket
        RuleDraft             — heuristic / diagnostic / research output
        PredictedImpact       — would_allow / would_still_deny / method
        parse_decision_line   — tolerant single-line parser (never raises)

    Loaders (analysis.loaders):
        Window                — half-open time window (since <= ts < until)
        LoadResult            — decisions + files_read + parse_errors
        load_gov_decisions    — directory → LoadResult
        parse_window_str      — "7d" / "60m" / "24h" → Window

    Detection (analysis.detect):
        detect_patterns       — group + rank denies, deterministic tie-breaker

    Drafting (analysis.draft):
        draft_for_pattern     — REGISTRY dispatch, returns RuleDraft | None
        reason_no_template    — human-readable decline reason

    Writers (analysis.writers):
        Finding               — (rank, pattern, draft) triple
        build_finding         — Finding constructor
        write_json            — canonical output (I2 / I4)
        write_markdown_from_json — projection from JSON (regenerable, I2)

See `python/analysis/SPEC.md` for the full library spec — module map,
invariants I1-I8, named boundaries.
"""
from __future__ import annotations

__version__ = "0.1.0"

from analysis.models import (
    Decision,
    Pattern,
    PredictedImpact,
    RuleDraft,
    parse_decision_line,
)
from analysis.loaders import (
    LoadResult,
    Window,
    load_gov_decisions,
    parse_window_str,
)
from analysis.detect import detect_patterns
from analysis.draft import draft_for_pattern, reason_no_template
from analysis.writers import (
    Finding,
    build_finding,
    write_json,
    write_markdown_from_json,
)

__all__ = [
    "__version__",
    # models
    "Decision",
    "Pattern",
    "PredictedImpact",
    "RuleDraft",
    "parse_decision_line",
    # loaders
    "LoadResult",
    "Window",
    "load_gov_decisions",
    "parse_window_str",
    # detection
    "detect_patterns",
    # drafting
    "draft_for_pattern",
    "reason_no_template",
    # writers
    "Finding",
    "build_finding",
    "write_json",
    "write_markdown_from_json",
]
