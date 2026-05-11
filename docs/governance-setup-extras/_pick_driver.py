#!/usr/bin/env python3
"""Pick driver step for kanban-dispatch.lobster.

Reads classify output from stdin (JSON with capabilities, complexity,
needs_frontier). Reads all agent capability cards from
~/.openclaw/data/agent-cards/*.json. Filters candidates that cover the
required capabilities. Ranks by cheapest model cost. Outputs JSON.

Output shape:
  {
    "driver": "<agent id>" | "unassigned",
    "complexity": "<echo of classify>",
    "caps_needed": ["<echo of classify>"],
    "candidates_considered": <int>
  }
"""

import glob
import json
import os
import sys


def main() -> None:
    classify = json.load(sys.stdin)
    caps_needed = set(classify.get("capabilities", []))
    cards_dir = os.path.expanduser("~/.openclaw/data/agent-cards")

    cards = []
    for f in glob.glob(f"{cards_dir}/*.json"):
        try:
            cards.append(json.load(open(f)))
        except Exception:
            pass

    candidates = []
    for c in cards:
        card_caps = {
            x["skill"]
            for x in c.get("capabilities", [])
            if isinstance(x, dict) and "skill" in x
        }
        if caps_needed.issubset(card_caps):
            candidates.append(c)

    ranked = sorted(
        candidates,
        key=lambda c: min(
            (m.get("premium_cost", 99.0) for m in c.get("models", [])),
            default=99.0,
        ),
    )
    pick = ranked[0] if ranked else {"id": "unassigned"}

    print(
        json.dumps(
            {
                "driver": pick.get("id"),
                "complexity": classify.get("complexity"),
                "caps_needed": sorted(caps_needed),
                "candidates_considered": len(candidates),
            }
        )
    )


if __name__ == "__main__":
    main()
