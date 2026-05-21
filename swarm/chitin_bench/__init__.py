"""Chitin Bench harness — a deterministic, bash-only Harbor agent.

Cleanroomed from mini-swe-agent (Princeton/SWE-agent team) with three
additions distinctive to Chitin Bench: environment bootstrap, loop detection,
and loud-fail (no silent retries). See ``agent.py:BenchAgent`` for the
entry point Harbor invokes.
"""

from swarm.chitin_bench.agent import BenchAgent

__all__ = ["BenchAgent"]
