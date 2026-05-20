"""Tests for the adapter registry (AC6).

AC6 — adding a new adapter is a one-entry registry change.
Also validates AC3 (detect routing).
"""
from __future__ import annotations

import sys
import pytest
from pathlib import Path

_chitin_root = Path(__file__).resolve().parents[5]
if str(_chitin_root / "python") not in sys.path:
    sys.path.insert(0, str(_chitin_root / "python"))

from analysis.spec_adapter.adapter import Adapter, SpecAdapterError
from analysis.spec_adapter.types import UnifiedSpec
from analysis.spec_adapter.registry import (
    register,
    unregister,
    detect,
    parse,
    adapters,
    reset,
)
from analysis.spec_adapter.speckit_adapter import SpecKitAdapter


class FakeAdapter(Adapter):
    """A trivial adapter for testing registration / routing."""

    def __init__(self, name: str = "fake", ext: str = ".fake") -> None:
        self.name = name
        self.ext = ext

    def detect(self, path: str | Path) -> bool:
        return str(path).endswith(self.ext)

    def parse(self, source_path: str | Path) -> UnifiedSpec:
        return UnifiedSpec(
            spec_id="fake",
            title="Fake Spec",
            status="draft",
            source_framework="fake",
            source_path=str(source_path),
        )


class TestRegistry:
    """AC6: adding a new adapter is a one-entry registry change."""

    def setup_method(self):
        """Reset registry before each test."""
        reset()
        # Re-register house adapter
        register(SpecKitAdapter())

    def teardown_method(self):
        reset()
        register(SpecKitAdapter())

    def test_register_adds_adapter(self):
        fake = FakeAdapter()
        register(fake)
        assert any(type(a).__name__ == "FakeAdapter" for a in adapters())

    def test_unregister_removes_adapter(self):
        fake = FakeAdapter()
        register(fake)
        unregister(fake)
        assert not any(type(a).__name__ == "FakeAdapter" for a in adapters())

    def test_idempotent_register(self):
        fake = FakeAdapter()
        register(fake)
        register(fake)
        count = sum(1 for a in adapters() if type(a).__name__ == "FakeAdapter")
        assert count == 1

    def test_detect_returns_correct_adapter(self):
        """AC3: detect correctly routes paths."""
        fake = FakeAdapter(ext=".fakey")
        register(fake)
        assert detect("test.fakey") is fake
        # House adapter handles .specify/specs/ paths
        assert isinstance(detect(".specify/specs/061/spec.md"), SpecKitAdapter)

    def test_detect_returns_none_for_unknown(self):
        assert detect("unknown.xyz") is None

    def test_parse_dispatches_to_adapter(self):
        fake = FakeAdapter(ext=".specfake")
        register(fake)
        result = parse("foo.specfake")
        assert result.spec_id == "fake"

    def test_parse_raises_for_unknown(self):
        with pytest.raises(ValueError, match="No adapter"):
            parse("unknown.xyz")

    def test_ac6_one_entry_change(self):
        """AC6: register one new adapter — immediately routable."""
        before = len(adapters())
        openspec = FakeAdapter(name="openspec", ext=".openspec.yaml")
        register(openspec)
        after = len(adapters())
        assert after == before + 1
        assert detect("foo.openspec.yaml") is openspec