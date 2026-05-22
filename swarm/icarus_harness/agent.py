"""Compatibility shim for legacy Icarus Harbor import paths."""

from swarm.chitin_bench.agent import BenchAgent


class IcarusAgent(BenchAgent):
    """Alias of ``BenchAgent`` kept for historical Harbor commands."""


__all__ = ["IcarusAgent"]
