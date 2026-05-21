"""Legacy Icarus Harbor agent import path.

Older tickets and trial repro instructions still reference
``swarm.icarus_harness.agent:IcarusAgent``. Keep that import stable
while the implementation lives in ``swarm.chitin_bench.agent``.
"""

from swarm.chitin_bench.agent import BenchAgent as IcarusAgent

__all__ = ["IcarusAgent"]
