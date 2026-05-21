"""Compatibility shim for the pre-rename Icarus Harbor agent path."""

from swarm.chitin_bench.agent import BenchAgent


class IcarusAgent(BenchAgent):
    """Alias the retired Icarus agent name to the current bench agent."""

