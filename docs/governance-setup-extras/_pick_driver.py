#!/usr/bin/env python3
"""Pick driver step for kanban-dispatch.lobster.

Reads classify output from stdin (JSON with capabilities, complexity,
needs_frontier). Reads all agent capability cards from
~/.openclaw/data/agent-cards/*.json. Filters candidates that cover the
required capabilities. Ranks by cheapest model cost. Outputs JSON.

Output shape:
  {
    "driver": "<agent id>" | "unassigned",
    "model": "<model id>" | "",
    "complexity": "<echo of classify>",
    "caps_needed": ["<echo of classify>"],
    "candidates_considered": <int>
  }
"""

import glob
import json
import os
import sys


def cheapest_model(card: dict) -> str:
    models = card.get("models", [])
    if not isinstance(models, list) or not models:
        return ""
    pick = min(
        (m for m in models if isinstance(m, dict)),
        key=lambda m: m.get("premium_cost", 99.0),
        default={},
    )
    return str(pick.get("id", ""))


def main() -> None:
    # Tolerate prefix garbage from openclaw's plugin loader (e.g.,
    # "[plugins] chitin-governance registering: ...") which gets routed to
    # stdout by openclaw's subsystem logger and lands ahead of clawta's
    # JSON reply when this step's stdin is wired to classify.stdout. Read
    # all of stdin and locate the first '{' before handing to json.loads.
    raw = sys.stdin.read()
    start = raw.find("{")
    if start < 0:
        raise SystemExit("classify produced no JSON object")
    classify = json.loads(raw[start:])
    caps_needed = set(classify.get("capabilities", []))
    cards_dir = os.path.expanduser("~/.openclaw/data/agent-cards")

    # Smoke / test override: when FORCE_DRIVER is non-empty, skip capability
    # matching and return the named driver verbatim. Used by
    # scripts/smoke-hermes-clawta-chain.sh to exercise each frontier-coder
    # card deterministically without depending on the classifier.
    force = os.environ.get("FORCE_DRIVER", "").strip()
    if force:
        forced_card = {}
        forced_path = os.path.join(cards_dir, f"{force}.json")
        try:
            with open(forced_path, encoding="utf-8") as f:
                forced_card = json.load(f)
        except Exception:
            pass
        print(
            json.dumps(
                {
                    "driver": force,
                    "model": cheapest_model(forced_card),
                    "complexity": classify.get("complexity"),
                    "caps_needed": sorted(caps_needed),
                    "candidates_considered": 1,
                }
            )
        )
        return

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
                "model": cheapest_model(pick),
                "complexity": classify.get("complexity"),
                "caps_needed": sorted(caps_needed),
                "candidates_considered": len(candidates),
            }
        )
    )


if __name__ == "__main__":
    main()
