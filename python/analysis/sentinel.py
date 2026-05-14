"""Sentinel entrypoint for chain-mined invariant authoring.

This is a thin wrapper around the decisions stream so swarm tickets can
invoke a named `analysis.sentinel` command while reusing the canonical
decision-pattern detection and candidate-rule drafting surface.
"""
from __future__ import annotations

import sys

from analysis.decisions import main as decisions_main


def main(argv: list[str] | None = None) -> int:
    return decisions_main(argv)


if __name__ == "__main__":
    raise SystemExit(main())
