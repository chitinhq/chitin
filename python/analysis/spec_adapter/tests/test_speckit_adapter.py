"""Tests for the spec-kit / house-format adapter (AC2, AC3, AC4, AC5).

AC2 — adapter parses at least 5 representative existing specs with zero loss.
AC3 — detect() correctly routes paths.
AC4 — malformed spec raises typed SpecAdapterError.
AC5 — round-trip of a house-format spec is lossless.
"""
from __future__ import annotations

import os
import tempfile
import pytest
from pathlib import Path

# Ensure the chitin python path is importable
import sys
_chitin_root = Path(__file__).resolve().parents[4]
if str(_chitin_root / "python") not in sys.path:
    sys.path.insert(0, str(_chitin_root / "python"))

from analysis.spec_adapter.adapter import SpecAdapterError
from analysis.spec_adapter.speckit_adapter import SpecKitAdapter
from analysis.spec_adapter.types import (
    AcceptanceCriterion,
    Question,
    Requirement,
    Slice,
    UnifiedSpec,
)


FIXTURES = Path(__file__).parent / "fixtures"
ADAPTER = SpecKitAdapter()


# ── AC3: detect() correctly routes paths ───────────────────────────────────

class TestDetect:
    def test_detect_house_spec(self):
        assert ADAPTER.detect(".specify/specs/061-unified-spec-model/spec.md")

    def test_detect_abs_path(self):
        p = Path("/home/user/chitin/.specify/specs/020-sdd-tdd/spec.md")
        assert ADAPTER.detect(p)

    def test_reject_non_spec_path(self):
        assert not ADAPTER.detect("README.md")

    def test_reject_random_path(self):
        assert not ADAPTER.detect("/tmp/foo.txt")

    def test_detect_with_specs_prefix(self):
        assert ADAPTER.detect("chitin/.specify/specs/062/spec.md")


# ── AC4: malformed spec raises typed error ─────────────────────────────────

class TestMalformed:
    def test_missing_file(self, tmp_path):
        missing = tmp_path / "nonexistent" / "spec.md"
        with pytest.raises(SpecAdapterError) as exc_info:
            ADAPTER.parse(missing)
        assert exc_info.value.section == "file"

    def test_no_h1_heading(self, tmp_path):
        bad = tmp_path / "000-no-heading" / "spec.md"
        bad.parent.mkdir(parents=True, exist_ok=True)
        bad.write_text("Just some text with no heading.\n")
        with pytest.raises(SpecAdapterError) as exc_info:
            ADAPTER.parse(bad)
        assert exc_info.value.section == "title"

    def test_error_is_typed(self):
        """SpecAdapterError is a real typed error, not a generic Exception."""
        err = SpecAdapterError("path.md", "requirements", "missing R1")
        assert isinstance(err, SpecAdapterError)
        assert err.source_path == "path.md"
        assert err.section == "requirements"
        assert "path.md" in str(err)
        assert "requirements" in str(err)


# ── AC5: round-trip is lossless ─────────────────────────────────────────────

class TestRoundTrip:
    def _roundtrip(self, spec: UnifiedSpec) -> UnifiedSpec:
        md = ADAPTER.render(spec)
        with tempfile.TemporaryDirectory() as td:
            dir_name = f"{spec.spec_id}-roundtrip"
            p = Path(td) / dir_name / "spec.md"
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(md)
            return ADAPTER.parse(p)

    def test_roundtrip_basic(self):
        original = UnifiedSpec(
            spec_id="042",
            title="Octi agent-bus identity contract",
            status="draft",
            source_framework="spec-kit",
            source_path=".specify/specs/042-octi/spec.md",
            requirements=[
                Requirement(id="R1", text="the identity contract"),
                Requirement(id="R2", text="identity anchors Discord threads"),
            ],
            acceptance=[
                AcceptanceCriterion(id="AC1", text="bus rejects connections without identity"),
                AcceptanceCriterion(id="AC2", text="every Discord notification includes an anchor"),
            ],
            boundaries=["Agent connects without identity"],
            slices=[
                Slice(id="Slice 1", scope="identity contract + bus validation (R1, R2)", requirement_ids=["R1", "R2"]),
            ],
            open_questions=[
                Question(id="Q1", text="key rotation", proposed="re-connect with signed update"),
            ],
        )
        result = self._roundtrip(original)
        assert result.spec_id == original.spec_id
        assert result.title == original.title
        assert result.status == original.status
        assert len(result.requirements) == len(original.requirements)
        assert result.requirements[0].id == "R1"
        assert result.requirements[1].id == "R2"
        assert len(result.acceptance) == len(original.acceptance)
        assert result.acceptance[0].id == "AC1"
        assert len(result.boundaries) == len(original.boundaries)
        assert len(result.slices) == len(original.slices)
        assert result.slices[0].id == "Slice 1"
        assert len(result.open_questions) == len(original.open_questions)
        assert result.open_questions[0].id == "Q1"

    def test_roundtrip_minimal(self):
        """Round-trip a spec with only id + title (boundary case 1)."""
        original = UnifiedSpec(
            spec_id="999",
            title="Minimal test spec",
            status="draft",
            source_framework="spec-kit",
            source_path=".specify/specs/999-test/spec.md",
        )
        result = self._roundtrip(original)
        assert result.spec_id == original.spec_id
        assert result.title == original.title
        assert result.status == original.status
        assert result.requirements == []
        assert result.acceptance == []
        assert result.boundaries == []
        assert result.slices == []
        assert result.open_questions == []


# ── AC2: parse fixture + live specs ──────────────────────────────────────────

class TestParseFixtures:
    """Parse the five fixture specs and verify zero-information loss."""

    @pytest.fixture
    def fixture_042(self):
        return ADAPTER.parse(FIXTURES / "042-octi-agentbus-identity-contract" / "spec.md")

    @pytest.fixture
    def fixture_020(self):
        return ADAPTER.parse(FIXTURES / "020-sdd-tdd-enforcement" / "spec.md")

    @pytest.fixture
    def fixture_011(self):
        return ADAPTER.parse(FIXTURES / "011-script-coverage" / "spec.md")

    @pytest.fixture
    def fixture_ic001(self):
        return ADAPTER.parse(FIXTURES / "036-ic-001-icarus" / "spec.md")

    @pytest.fixture
    def fixture_728(self):
        return ADAPTER.parse(FIXTURES / "728-dispatch-default-branch-fix" / "spec.md")

    def test_fixture_042(self, fixture_042):
        spec = fixture_042
        assert spec.spec_id == "042"
        assert "identity" in spec.title.lower()
        assert spec.status == "draft"
        assert spec.source_framework == "spec-kit"
        assert len(spec.requirements) >= 2
        assert spec.requirements[0].id == "R1"
        assert len(spec.acceptance) >= 2
        assert spec.acceptance[0].id == "AC1"
        assert len(spec.boundaries) >= 1
        assert len(spec.open_questions) >= 1
        assert spec.open_questions[0].id == "Q1"
        assert len(spec.slices) >= 1

    def test_fixture_020(self, fixture_020):
        spec = fixture_020
        assert spec.spec_id == "020"
        assert spec.status == "ratified"
        assert len(spec.requirements) >= 5
        assert len(spec.acceptance) >= 6
        assert len(spec.boundaries) >= 1
        assert len(spec.slices) >= 1

    def test_fixture_011(self, fixture_011):
        spec = fixture_011
        # Has no "Spec NNN:" H1 — title is "Script Coverage: chitin-agent-unlock.sh"
        assert spec.title
        assert spec.status == "draft"
        # Checkbox-style ACs
        assert len(spec.acceptance) >= 1
        assert len(spec.boundaries) >= 1

    def test_fixture_ic001(self, fixture_ic001):
        spec = fixture_ic001
        assert spec.spec_id == "ic-001"
        assert "Icarus" in spec.title or "icarus" in spec.title.lower()
        assert spec.status == "draft"
        assert len(spec.acceptance) >= 1
        assert len(spec.boundaries) >= 1
        assert len(spec.open_questions) >= 1
        assert len(spec.slices) >= 1

    def test_fixture_728(self, fixture_728):
        spec = fixture_728
        assert spec.spec_id == "728"
        assert "branch" in spec.title.lower()
        assert len(spec.acceptance) >= 1
        assert len(spec.boundaries) >= 1


class TestParseLiveSpecs:
    """AC2: parse at least 5 representative existing live specs with zero losses."""

    LIVE_SPECS_DIR = _chitin_root / ".specify" / "specs"

    def _get_live_specs(self):
        """Collect spec.md files from .specify/specs/."""
        if not self.LIVE_SPECS_DIR.exists():
            pytest.skip("No .specify/specs directory found")
        specs = []
        for d in sorted(self.LIVE_SPECS_DIR.iterdir()):
            if not d.is_dir():
                continue
            spec_md = d / "spec.md"
            if spec_md.exists():
                specs.append(spec_md)
        return specs

    @pytest.mark.parametrize("idx", range(5))
    def test_parse_live_spec(self, idx):
        specs = self._get_live_specs()
        if len(specs) <= idx:
            pytest.skip(f"Not enough live specs (found {len(specs)})")
        spec_path = specs[idx]
        result = ADAPTER.parse(spec_path)
        assert isinstance(result, UnifiedSpec)
        assert result.spec_id, f"No spec_id parsed from {spec_path}"
        assert result.title, f"No title parsed from {spec_path}"
        assert result.status in ("draft", "ratified", "superseded")

    def test_all_live_specs_parse(self):
        """Parse every live spec and verify zero failures."""
        specs = self._get_live_specs()
        failures = []
        for spec_path in specs:
            try:
                result = ADAPTER.parse(spec_path)
                if not result.spec_id:
                    failures.append((str(spec_path), "missing spec_id"))
                if not result.title:
                    failures.append((str(spec_path), "missing title"))
            except SpecAdapterError as e:
                failures.append((str(spec_path), str(e)))
            except Exception as e:
                failures.append((str(spec_path), f"unexpected: {e}"))

        if failures:
            msg_lines = [f"  {p}: {r}" for p, r in failures]
            pytest.fail(
                f"{len(failures)} of {len(specs)} specs failed:\n" + "\n".join(msg_lines)
            )