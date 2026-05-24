"""Tests for spec_adapter package — registry, auto-registration, and T029 coverage."""
from __future__ import annotations

import os
import pytest
from pathlib import Path

from analysis.spec_adapter import (
    ModuleAdapter,
    ParseError,
    DuplicateIDError,
    register,
    lookup,
    all_adapters,
    detect_adapters,
    speckit_mod,
    superpowers_mod,
)
from analysis.spec_adapter.registry import _REGISTRY

# Resolve chitin repo root (two levels up from python/analysis/tests)
_REPO_ROOT = Path(__file__).resolve().parents[3]


class TestRegistry:
    """T006 / T019: adapter registry contract."""

    def test_speckit_registered(self):
        """AC6: adding a new adapter is one registry entry — speckit is present."""
        adapter = lookup("spec-kit")
        assert adapter is not None
        assert adapter.framework() == "spec-kit"

    def test_superpowers_registered(self):
        """Superpowers adapter registered via auto-import."""
        adapter = lookup("superpowers")
        assert adapter is not None
        assert adapter.framework() == "superpowers"

    def test_all_adapters_returns_both(self):
        adapters = all_adapters()
        frameworks = {a.framework() for a in adapters}
        assert "spec-kit" in frameworks
        assert "superpowers" in frameworks

    def test_detect_routes_speckit(self):
        """AC3: detect routes each path to exactly one adapter."""
        spec_dir = _REPO_ROOT / ".specify" / "specs" / "061-unified-spec-model"
        adapter = detect_adapters(spec_dir)
        assert adapter is not None
        assert adapter.framework() == "spec-kit"

    def test_detect_routes_superpowers(self):
        sp_dir = _REPO_ROOT / "docs" / "superpowers"
        if not sp_dir.is_dir():
            pytest.skip("No docs/superpowers directory in repo")
        # Pick any date-stemmed subdirectory
        for child in sorted(sp_dir.iterdir()):
            if child.is_dir() and child.name[0].isdigit():
                adapter = detect_adapters(child)
                assert adapter is not None
                assert adapter.framework() == "superpowers"
                return
        pytest.skip("No date-stemmed superpowers directory found")

    def test_detect_returns_none_for_unknown(self):
        adapter = detect_adapters("/tmp/unknown-format.yaml")
        assert adapter is None

    def test_duplicate_registration_raises(self):
        """Cannot register same framework twice."""
        with pytest.raises(ValueError, match="already registered"):
            register(ModuleAdapter("speckit-dupe", "spec-kit", speckit_mod))


class TestBaseClasses:
    """T001/T005: base interface contract."""

    def test_parse_error_fields(self):
        err = ParseError("/path/spec.md", "requirements", "missing id")
        assert err.path == "/path/spec.md"
        assert err.section == "requirements"
        assert "missing id" in err.detail

    def test_duplicate_id_error_fields(self):
        err = DuplicateIDError("requirement", "R1")
        assert err.kind == "requirement"
        assert err.id == "R1"


class TestModuleAdapter:
    """ModuleAdapter wraps functional modules."""

    def test_detect_delegates(self):
        spec_dir = _REPO_ROOT / ".specify" / "specs" / "061-unified-spec-model"
        adapter = lookup("spec-kit")
        assert adapter is not None
        assert adapter.detect(spec_dir) is True

    def test_parse_delegates(self):
        spec_dir = _REPO_ROOT / ".specify" / "specs" / "061-unified-spec-model"
        adapter = lookup("spec-kit")
        result = adapter.parse(spec_dir)
        assert result.spec_id == "061"

    def test_framework_returns_name(self):
        adapter = lookup("spec-kit")
        assert adapter.framework() == "spec-kit"


class TestPackageReexports:
    """T029: flat modules re-exported from the package."""

    def test_import_speckit_from_package(self):
        from analysis.spec_adapter import speckit_mod as sm
        assert hasattr(sm, "detect")
        assert hasattr(sm, "parse")

    def test_import_superpowers_from_package(self):
        from analysis.spec_adapter import superpowers_mod as sm
        assert hasattr(sm, "detect")
        assert hasattr(sm, "parse")

    def test_import_registry_from_package(self):
        from analysis.spec_adapter import register as r
        assert r is register