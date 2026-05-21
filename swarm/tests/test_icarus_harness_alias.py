"""Regression tests for the legacy Icarus Harbor import path."""

from __future__ import annotations

from importlib import import_module
import sys
import types
from unittest import TestCase


class TestIcarusHarnessAlias(TestCase):
    """Invariant: historical Harbor repro commands still import IcarusAgent."""

    def tearDown(self) -> None:
        for name in (
            "swarm.icarus_harness",
            "swarm.icarus_harness.agent",
            "swarm.chitin_bench.agent",
        ):
            sys.modules.pop(name, None)

    def test_import_path_exposes_icarus_agent(self) -> None:
        bench_module = types.ModuleType("swarm.chitin_bench.agent")

        class BenchAgent:
            pass

        bench_module.BenchAgent = BenchAgent
        sys.modules[bench_module.__name__] = bench_module

        agent_module = import_module("swarm.icarus_harness.agent")

        self.assertTrue(hasattr(agent_module, "IcarusAgent"))
        self.assertTrue(issubclass(agent_module.IcarusAgent, bench_module.BenchAgent))
        self.assertEqual(agent_module.IcarusAgent.name(), "icarus")
