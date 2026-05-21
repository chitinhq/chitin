"""Legacy Icarus compatibility shim.

The harness was renamed from ``swarm.icarus_harness`` to
``swarm.chitin_bench``. Older tickets and repro instructions still
reference the pre-rename import path and class, so keep that surface
alive as a thin alias.
"""

from swarm.chitin_bench.agent import BenchAgent


class IcarusAgent(BenchAgent):
    """Backward-compatible alias for Harbor import-path stability."""

    @staticmethod
    def name() -> str:
        return "icarus"
