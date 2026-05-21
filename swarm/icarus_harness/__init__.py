"""Backward-compatible import surface for historical Icarus harness runs.

The harness implementation moved to ``swarm.chitin_bench``. Keep the
old package path importable so queued or archived Harbor jobs that still
reference ``swarm.icarus_harness.agent:IcarusAgent`` can start.
"""

from .agent import IcarusAgent

__all__ = ["IcarusAgent"]
