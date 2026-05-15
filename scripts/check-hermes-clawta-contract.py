#!/usr/bin/env python3
"""Check the Hermes↔Clawta collaboration contract stays wired.

This is intentionally static/source-level so CI and the regression gate can run
without a live Hermes DB. Live board schema is still surfaced by the Control
Tower report when present.
"""
from __future__ import annotations

import ast
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPORT = ROOT / "swarm" / "bin" / "clawta-report"

REQUIRED_BLOCK_REASONS = {
    "needs-fix",
    "needs-rebase",
    "no-pr",
    "retry-exhausted",
    "explicit-failure",
    "silent-death",
    "ci-fail",
    "pr-rejected",
    "deploy-drift",
    "operator-decision",
    "dep-gate",
    "poller-oscillation",
}


def load_block_reason_meta() -> dict:
    tree = ast.parse(REPORT.read_text())
    for node in tree.body:
        if isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == "BLOCK_REASON_META":
                    return ast.literal_eval(node.value)
    raise SystemExit("BLOCK_REASON_META not found in clawta-report")


def main() -> int:
    meta = load_block_reason_meta()
    missing = sorted(REQUIRED_BLOCK_REASONS - set(meta))
    extra_without_owner = sorted(reason for reason, spec in meta.items() if not spec.get("owner"))
    if missing or extra_without_owner:
        if missing:
            print("missing block_reason(s): " + ", ".join(missing))
        if extra_without_owner:
            print("block_reason(s) without owner: " + ", ".join(extra_without_owner))
        return 1
    print(f"hermes-clawta-contract: {len(REQUIRED_BLOCK_REASONS)} block_reason(s) mapped with owners")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
