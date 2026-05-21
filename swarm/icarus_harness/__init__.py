"""Backward-compatible Icarus harness import surface.

The canonical Harbor agent lives under ``swarm.chitin_bench.agent``.
This package preserves the historical ``swarm.icarus_harness`` import
path used by older bench tickets and repro commands.
"""

from swarm.icarus_harness.agent import IcarusAgent

__all__ = ["IcarusAgent"]
