"""Tests for the spec-kit/house adapter (speckit_adapter.py)."""
from __future__ import annotations

import json
import tempfile
from pathlib import Path

import pytest

from analysis.speckit_adapter import (
    SpecKitDuplicateIDError,
    SpecKitParseError,
    detect,
    parse,
    parse_tree,
)
from analysis.unified_spec import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    SourceFramework,
    SpecStatus,
    UnifiedSpec,
)


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

SPEC_061 = """# 061 — Unified spec model + framework adapters (L1)

**Status**: draft 2026-05-19

### R1 — the normalized model is the only upward contract

L2–L7 consume `UnifiedSpec` exclusively.

### R2 — the adapter interface

An adapter is a pure function parse(source) -> UnifiedSpec.

### R3 — spec-kit adapter (reference implementation)

The spec-kit / house-format adapter parses .specify/specs/.

**AC1** — UnifiedSpec schema is defined and documented.

**AC2** — spec-kit adapter parses all 53 existing specs.

**AC3** — detect correctly routes a path to exactly one adapter.

## Boundary cases

1. **Spec with no requirements** — may be valid if it has slices.
2. **Ambiguous spec_id** — raises typed error.
3. **Malformed source** — raises typed error naming file and section.

## Slice plan

- **Slice 1** — UnifiedSpec schema + adapter interface + spec-kit adapter. R1, R2, R3.

- **Slice 2** — Superpowers adapter (R5).

## Open questions

- **Q1 — model owner** — JSON Schema canonical?

- **Q2 — adapter location** — One package, one registry?
"""

SPEC_020 = """# 020 — Chitin enforces SDD + TDD as governance policy

**Status**: shipped

### R1 — no-code-without-test

Pre-commit hook rejects code changes without test changes.

## Boundary cases

1. **Legitimate refactor** — escape hatch with typed reason.

## Slice plan

- **Slice 1** — Phase 1 hooks. R1.
"""

SPEC_NO_AC = """# 036 — Some spec

**Status**: draft

### R1 — just a requirement

Some text.

## Slice plan

- **Slice 1** — First slice. R1.
"""


def _write_spec(tmp_path: Path, dir_name: str, content: str) -> Path:
    """Write a spec.md file into a temp directory and return the dir path."""
    spec_dir = tmp_path / dir_name
    spec_dir.mkdir(parents=True, exist_ok=True)
    (spec_dir / "spec.md").write_text(content, encoding="utf-8")
    return spec_dir


# ---------------------------------------------------------------------------
# Detection tests
# ---------------------------------------------------------------------------


class TestDetect:
    def test_valid_spec_kit_dir(self, tmp_path):
        dir_path = _write_spec(tmp_path, "020-sdd-tdd-enforcement", "# 020 — Title\n")
        assert detect(dir_path) is True

    def test_dir_without_spec_md(self, tmp_path):
        dir_path = tmp_path / "020-no-spec-file"
        dir_path.mkdir()
        assert detect(dir_path) is False

    def test_non_numeric_prefix(self, tmp_path):
        dir_path = _write_spec(tmp_path, "random-name", "# Title\n")
        assert detect(dir_path) is False

    def test_ic_prefix(self, tmp_path):
        dir_path = _write_spec(tmp_path, "020-ic-001-test", "# ic-001 — Title\n")
        # "020-ic-001-test" — dirName starts with "020" which matches \d{3,}
        assert detect(dir_path) is True

    def test_sw_prefix(self, tmp_path):
        dir_path = _write_spec(tmp_path, "037-sw-011-heartbeat", "# sw-011 — Title\n")
        assert detect(dir_path) is True


# ---------------------------------------------------------------------------
# Parsing tests
# ---------------------------------------------------------------------------


class TestParseTitle:
    @pytest.mark.parametrize(
        "content,title_substring",
        [
            ("# Spec 050: Mini MCP — dispatch\n", "Mini MCP — dispatch"),
            ("# 020 — Chitin enforces SDD + TDD\n", "Chitin enforces SDD + TDD"),
            ("# Feature Specification: Drift Guard — elimination\n", "Drift Guard — elimination"),
        ],
    )
    def test_title_formats(self, tmp_path, content, title_substring):
        dir_path = _write_spec(tmp_path, "050-mini-mcp-spec", content)
        result = parse(dir_path)
        assert title_substring in result.title

    def test_empty_doc_title(self, tmp_path):
        dir_path = _write_spec(tmp_path, "061-test", "")
        result = parse(dir_path)
        assert result.title == ""


class TestParseStatus:
    @pytest.mark.parametrize(
        "status_text,expected",
        [
            ("**Status**: shipped", SpecStatus.RATIFIED),
            ("**Status**: ratified", SpecStatus.RATIFIED),
            ("**Status**: draft", SpecStatus.DRAFT),
            ("**Status**: superseded", SpecStatus.SUPERSEDED),
            ("Some text without status", SpecStatus.DRAFT),
        ],
    )
    def test_status_extraction(self, tmp_path, status_text, expected):
        content = f"# 020 — Title\n\n{status_text}\n"
        dir_path = _write_spec(tmp_path, "020-test", content)
        result = parse(dir_path)
        assert result.status == expected


class TestParseRequirements:
    def test_h3_requirements(self, tmp_path):
        content = "# 061 — Title\n\n### R1 — model is the contract\n\n### R2 — adapter interface\n"
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.requirements) >= 2
        assert result.requirements[0].id == "R1"
        assert result.requirements[1].id == "R2"

    def test_bold_requirements(self, tmp_path):
        content = "# 061 — Title\n\n**R3 — spec-kit adapter**\n"
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.requirements) >= 1
        assert result.requirements[0].id == "R3"


class TestParseAcceptance:
    def test_bold_ac(self, tmp_path):
        content = "# 061 — Title\n\n**AC1** — UnifiedSpec schema is defined.\n\n**AC2** — spec-kit adapter parses all specs.\n"
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.acceptance) >= 2
        assert result.acceptance[0].id == "AC1"
        assert result.acceptance[1].id == "AC2"


class TestParseBoundaries:
    def test_numbered_boundaries(self, tmp_path):
        content = "# 061 — Title\n\n## Boundary cases\n\n1. **Spec with no requirements** — may be valid.\n2. **Ambiguous spec_id** — raises typed error.\n"
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.boundaries) >= 2
        assert "no requirements" in result.boundaries[0]

    def test_bullet_boundaries(self, tmp_path):
        content = "# 061 — Title\n\n## Boundary cases\n\n- Spec with no requirements\n- Ambiguous spec_id\n"
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.boundaries) >= 2


class TestParseSlices:
    def test_slice_plan(self, tmp_path):
        content = (
            "# 061 — Title\n\n"
            "### R1 — model\n\n### R2 — adapter\n\n### R3 — spec-kit\n\n"
            "## Slice plan\n\n"
            "- **Slice 1** — Schema + adapter. R1, R2, R3.\n\n"
            "- **Slice 2** — Superpowers adapter. R5.\n"
        )
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.slices) >= 2
        assert result.slices[0].id == "Slice 1"
        # Slice 1 should link to R1, R2, R3
        assert "R1" in result.slices[0].requirement_ids
        assert "R2" in result.slices[0].requirement_ids


class TestParseQuestions:
    def test_open_questions(self, tmp_path):
        content = (
            "# 061 — Title\n\n"
            "## Open questions\n\n"
            "- **Q1 — model owner** — JSON Schema canonical?\n\n"
            "- **Q2 — adapter location** — One package?\n"
        )
        dir_path = _write_spec(tmp_path, "061-test", content)
        result = parse(dir_path)
        assert len(result.open_questions) >= 2
        assert result.open_questions[0].id == "Q1"
        assert result.open_questions[1].id == "Q2"


class TestParseErrors:
    def test_missing_spec_md(self, tmp_path):
        dir_path = tmp_path / "020-no-spec"
        dir_path.mkdir()
        with pytest.raises(SpecKitParseError) as exc_info:
            parse(dir_path)
        assert exc_info.value.section == "file-read"

    def test_directory_without_spec_id(self, tmp_path):
        dir_path = _write_spec(tmp_path, "random-name", "# Title\n")
        with pytest.raises(SpecKitParseError) as exc_info:
            parse(dir_path)
        assert exc_info.value.section == "spec_id"


class TestParseTree:
    def test_parse_all_specs(self, tmp_path):
        specs_dir = tmp_path / "specs"
        specs_dir.mkdir()
        _write_spec(tmp_path / "specs", "020-sdd-tdd", SPEC_020)
        _write_spec(tmp_path / "specs", "061-unified", SPEC_061)
        results = parse_tree(specs_dir)
        assert len(results) >= 2

    def test_duplicate_spec_id(self, tmp_path):
        specs_dir = tmp_path / "specs"
        specs_dir.mkdir()
        # Two dirs with the same spec_id prefix "020"
        _write_spec(tmp_path / "specs", "020-sdd-tdd", SPEC_020)
        _write_spec(tmp_path / "specs", "020-other-spec", "# 020 — Other\n")
        with pytest.raises(SpecKitDuplicateIDError):
            parse_tree(specs_dir)

    def test_nonexistent_dir(self, tmp_path):
        results = parse_tree(tmp_path / "nonexistent")
        assert results == []


class TestSpecIDExtraction:
    @pytest.mark.parametrize(
        "dir_name,expected_id",
        [
            ("020-sdd-tdd-enforcement", "020"),
            ("036-ic-001-icarus-local-llm-driver", "036"),
            ("062-spec-build-attribution", "062"),
            ("728-dispatch-default-branch-fix", "728"),
        ],
    )
    def test_spec_id_patterns(self, tmp_path, dir_name, expected_id):
        content = f"# {dir_name} — Test\n"
        dir_path = _write_spec(tmp_path, dir_name, content)
        result = parse(dir_path)
        assert result.spec_id == expected_id


class TestIntegration_FullSpec:
    def test_full_spec_061(self, tmp_path):
        dir_path = _write_spec(tmp_path, "061-unified-spec-model", SPEC_061)
        result = parse(dir_path)

        # Verify spec_id
        assert result.spec_id == "061"
        # Verify source_framework
        assert result.source_framework == SourceFramework.SPEC_KIT
        # Verify title is not empty
        assert result.title != ""
        # Verify requirements
        assert len(result.requirements) >= 3
        assert result.requirements[0].id == "R1"
        # Verify acceptance criteria
        assert len(result.acceptance) >= 2
        assert result.acceptance[0].id == "AC1"
        # Verify boundaries
        assert len(result.boundaries) >= 2
        # Verify slices
        assert len(result.slices) >= 2
        # Verify open questions
        assert len(result.open_questions) >= 2
        assert result.open_questions[0].id == "Q1"

    def test_spec_no_acceptance(self, tmp_path):
        dir_path = _write_spec(tmp_path, "036-dispatch", SPEC_NO_AC)
        result = parse(dir_path)
        # Should still parse fine with empty acceptance
        assert result.spec_id == "036"
        assert result.requirements[0].id == "R1"

    def test_to_dict_roundtrip(self, tmp_path):
        dir_path = _write_spec(tmp_path, "061-unified-spec-model", SPEC_061)
        result = parse(dir_path)
        d = result.to_dict()

        # Verify JSON-serializable
        json_str = json.dumps(d)
        assert json_str is not None

        # Verify required fields are present
        assert "spec_id" in d
        assert "title" in d
        assert "status" in d
        assert "source_framework" in d
        assert "source_path" in d
        assert "requirements" in d
        assert "acceptance" in d
        assert "boundaries" in d
        assert "slices" in d
        assert "open_questions" in d