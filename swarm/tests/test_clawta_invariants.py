"""Tests for invariants gate regex — must accept both Boundary and Boundaries."""
from __future__ import annotations

import importlib.util
import sys
from importlib.machinery import SourceFileLoader
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
POLLER = REPO_ROOT / "swarm" / "bin" / "clawta-poller"


def _load_module():
    spec = importlib.util.spec_from_loader(
        "clawta_poller_invariants_test",
        SourceFileLoader("clawta_poller_invariants_test", str(POLLER)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["clawta_poller_invariants_test"] = module
    spec.loader.exec_module(module)
    return module


def test_extract_named_boundaries_accepts_singular_boundary():
    """Singular 'Boundary:' must satisfy the gate (the bug being fixed)."""
    module = _load_module()
    result = module.extract_named_boundaries("Boundary: empty, max")
    assert result == ["empty", "max"], f"expected ['empty', 'max'], got {result}"


def test_extract_named_boundaries_accepts_plural_boundaries():
    """Plural 'Boundaries:' must still satisfy the gate (existing behaviour)."""
    module = _load_module()
    result = module.extract_named_boundaries("Boundaries: empty, max")
    assert result == ["empty", "max"], f"expected ['empty', 'max'], got {result}"


def test_extract_named_boundaries_rejects_boundarie():
    """'Boundarie:' (incorrect spelling, missing 's') must NOT match."""
    module = _load_module()
    result = module.extract_named_boundaries("Boundarie: empty, max")
    assert result == [], f"expected [], got {result}"


def test_extract_named_boundaries_rejects_missing_field():
    """Missing boundary field entirely must return empty list."""
    module = _load_module()
    assert module.extract_named_boundaries(None) == []
    assert module.extract_named_boundaries("") == []
    assert module.extract_named_boundaries("Invariant: something") == []


def test_missing_invariants_reason_accepts_singular_boundary():
    """A ticket with 'Boundary: empty, max' should NOT be demoted."""
    module = _load_module()
    ticket = {
        "body": (
            "invariants_and_boundaries:\n"
            "  Invariant: parser never returns an empty action.\n"
            "  Boundary: empty, max\n"
        ),
    }
    reason = module.missing_invariants_reason(ticket)
    assert reason is None, f"expected None (no demotion), got: {reason}"


def test_missing_invariants_reason_accepts_plural_boundaries():
    """A ticket with 'Boundaries: empty, max' should NOT be demoted."""
    module = _load_module()
    ticket = {
        "body": (
            "invariants_and_boundaries:\n"
            "  Invariant: parser never returns an empty action.\n"
            "  Boundaries: empty, max\n"
        ),
    }
    reason = module.missing_invariants_reason(ticket)
    assert reason is None, f"expected None (no demotion), got: {reason}"


def test_missing_invariants_reason_rejects_boundarie_typo():
    """A ticket with 'Boundarie: empty, max' (typo) should be demoted."""
    module = _load_module()
    ticket = {
        "body": (
            "invariants_and_boundaries:\n"
            "  Invariant: parser never returns an empty action.\n"
            "  Boundarie: empty, max\n"
        ),
    }
    reason = module.missing_invariants_reason(ticket)
    assert reason is not None, "expected a demotion reason for 'Boundarie:' typo, got None"
    assert "missing explicit boundary list" in reason


def test_missing_invariants_reason_rejects_no_boundary():
    """A ticket with invariant but no boundary line should be demoted."""
    module = _load_module()
    ticket = {
        "body": (
            "invariants_and_boundaries:\n"
            "  Invariant: parser never returns an empty action.\n"
        ),
    }
    reason = module.missing_invariants_reason(ticket)
    assert reason is not None, "expected a demotion reason for missing boundary, got None"
    assert "missing explicit boundary list" in reason