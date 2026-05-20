"""Icarus harness — a deterministic, bash-only Harbor agent.

Cleanroomed from mini-swe-agent (Princeton/SWE-agent team) with three
additions distinctive to Icarus: environment bootstrap, loop detection,
and loud-fail (no silent retries). See ``agent.py:IcarusAgent`` for the
entry point Harbor invokes.
"""

from swarm.icarus_harness.agent import IcarusAgent

__all__ = ["IcarusAgent"]
