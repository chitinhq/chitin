"""Tests for the Superpowers markdown adapter."""
from __future__ import annotations

from pathlib import Path

from analysis.superpowers_adapter import detect, parse
from analysis.unified_spec import SourceFramework, SpecStatus


SUPERPOWERS_SPEC = """# Chitin Dashboard — visual replay + self-improving feedback loop

Status: spec — open

## Goal

Build the dashboard without changing kernel authority.

## Hard rules

- Kernel events remain canonical.
- OTEL stays a projection.

## Slice 1 — Capture extension

Worker captures prompt and tool I/O.

**Acceptance:**
- Prompt sidecar is written.
- Chain id links back to the run.

## Slice 2 — Replay API

Expose replay for sessions.

## Non-goals

- No live execution from the dashboard.

## Open questions

1. Should token costs be sampled or exact?
"""


PLAN = """# Adopt spec-kit; retire docs/superpowers/specs/ — Implementation Plan

**Goal:** Replace the bespoke specs workflow.

### Task 1.1: Branch + install spec-kit CLI

- [ ] Create the feature branch
- [ ] Verify supported-agent list
"""


def _write(root: Path, rel: str, body: str) -> Path:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    return path


def test_detects_superpowers_markdown(tmp_path):
    path = _write(
        tmp_path,
        "docs/superpowers/specs/2026-05-12-chitin-dashboard.md",
        SUPERPOWERS_SPEC,
    )

    assert detect(path) is True
    assert detect(tmp_path / "docs/other/2026-05-12-chitin-dashboard.md") is False


def test_detects_only_superpowers_directories(tmp_path):
    superpowers_dir = tmp_path / "docs/superpowers/specs/2026-05-12-chitin-dashboard"
    superpowers_dir.mkdir(parents=True)
    (superpowers_dir / "spec.md").write_text(SUPERPOWERS_SPEC, encoding="utf-8")
    speckit_dir = tmp_path / ".specify/specs/061-unified-spec-model"
    speckit_dir.mkdir(parents=True)
    (speckit_dir / "spec.md").write_text(SUPERPOWERS_SPEC, encoding="utf-8")

    assert detect(superpowers_dir) is True
    assert detect(speckit_dir) is False


def test_parse_superpowers_spec_sections(tmp_path):
    path = _write(
        tmp_path,
        "docs/superpowers/specs/2026-05-12-chitin-dashboard.md",
        SUPERPOWERS_SPEC,
    )

    result = parse(path)

    assert result.spec_id == "2026-05-12-chitin-dashboard"
    assert result.title == "Chitin Dashboard — visual replay + self-improving feedback loop"
    assert result.status == SpecStatus.RATIFIED
    assert result.source_framework == SourceFramework.SUPERPOWERS
    assert [r.text for r in result.requirements] == [
        "Build the dashboard without changing kernel authority",
        "Kernel events remain canonical",
        "OTEL stays a projection",
    ]
    assert [a.text for a in result.acceptance] == [
        "Prompt sidecar is written",
        "Chain id links back to the run",
    ]
    assert result.slices[0].id == "Slice 1"
    assert result.slices[1].scope == "Replay API"
    assert result.boundaries == ("No live execution from the dashboard",)
    assert result.open_questions[0].id == "Q1"


def test_parse_superpowers_directory_uses_directory_spec_id(tmp_path):
    root = tmp_path / "docs/superpowers/specs/2026-05-12-chitin-dashboard"
    root.mkdir(parents=True)
    (root / "spec.md").write_text(SUPERPOWERS_SPEC, encoding="utf-8")

    result = parse(root)

    assert result.spec_id == "2026-05-12-chitin-dashboard"


def test_parse_superpowers_plan_checkboxes_as_slices(tmp_path):
    path = _write(
        tmp_path,
        "docs/superpowers/plans/2026-05-15-adopt-speckit-replace-spec-flow.md",
        PLAN,
    )

    result = parse(path)

    assert result.status == SpecStatus.DRAFT
    assert result.requirements[0].id == "R1"
    assert "Replace the bespoke specs workflow" in result.requirements[0].text
    assert [s.scope for s in result.slices] == [
        "Create the feature branch",
        "Verify supported-agent list",
    ]


def test_partial_superpowers_note_does_not_fabricate_fields(tmp_path):
    path = _write(
        tmp_path,
        "docs/superpowers/specs/2026-05-20-short-note.md",
        "# Short note\n\nJust a narrative note.\n",
    )

    result = parse(path)

    assert result.title == "Short note"
    assert result.requirements == ()
    assert result.acceptance == ()
    assert result.slices == ()


def test_parse_superpowers_preserves_explicit_question_ids(tmp_path):
    path = _write(
        tmp_path,
        "docs/superpowers/specs/2026-05-20-question-ids.md",
        "# Question IDs\n\n## Open questions\n\n- **Q2 — Should this retain numbering?**\n",
    )

    result = parse(path)

    assert result.open_questions[0].id == "Q2"
    assert result.open_questions[0].text == "Should this retain numbering?"
