#!/usr/bin/env python3
"""Pick driver step for kanban-dispatch.lobster.

Hybrid LLM + deterministic routing (Slice 2 of Hermes/Clawta architecture
epic, 2026-05-12). The LLM (Clawta via glm-agent) makes the judgment call
informed by the ticket classification and the available agent cards. The
deterministic capability/cost ranking is preserved as a fallback path
when the LLM output is unparseable or the LLM call fails.

Inputs:
  stdin   — JSON from classify step: {complexity, capabilities, estimated_loc, needs_frontier}
  env     — FORCE_DRIVER (smoke override; if set, skip LLM + deterministic both)
  env     — ROUTER_MODE (default "llm"; set to "deterministic" to skip LLM)
  files   — ~/.openclaw/data/agent-cards/*.json

Output schema (stdout JSON):
  {
    "driver":               <agent id> | "unassigned",
    "model":                <model id> | null,
    "complexity":           <echo>,
    "caps_needed":          <echo>,
    "candidates_considered": <int>,
    "router_mode":          "llm" | "deterministic" | "forced",
    "elo_consulted":        false,    # Slice 5 of architecture epic wires this
    "reasoning":            "<one-line justification>"
  }
"""

import glob
import json
import os
import re
import subprocess
import sys


CARDS_DIR = os.path.expanduser("~/.openclaw/data/agent-cards")
LLM_TIMEOUT_SECONDS = 60


def load_cards() -> list[dict]:
    cards = []
    for f in glob.glob(f"{CARDS_DIR}/*.json"):
        try:
            with open(f) as fh:
                cards.append(json.load(fh))
        except Exception:
            pass
    return cards


def deterministic_pick(classify: dict, cards: list[dict]) -> tuple[dict, list[dict]]:
    """Capability-filter + cheapest-model rank. Returns (picked_card, candidates)."""
    caps_needed = set(classify.get("capabilities", []))
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
    return pick, candidates


def cheapest_model(card: dict) -> str | None:
    models = sorted(
        card.get("models", []),
        key=lambda m: m.get("premium_cost", 99.0),
    )
    return models[0].get("id") if models else None


def cards_summary_for_llm(cards: list[dict]) -> list[dict]:
    """Trim agent cards to the fields the LLM needs to make a routing call.

    Avoids stuffing the LLM context with verbose card metadata.
    """
    out = []
    for c in cards:
        out.append({
            "id": c.get("id"),
            "description": c.get("description", "")[:200],
            "capabilities": [
                {"skill": x.get("skill"), "depth": x.get("depth")}
                for x in c.get("capabilities", [])
                if isinstance(x, dict)
            ],
            "models": [
                {"id": m.get("id"), "tier": m.get("tier"), "premium_cost": m.get("premium_cost")}
                for m in c.get("models", [])
            ],
        })
    return out


def llm_pick(classify: dict, cards: list[dict]) -> dict | None:
    """Call Clawta (glm-agent) for the routing judgment.

    Returns parsed dict {driver, model, reasoning} on success, None on
    any failure (timeout, parse error, missing fields). Caller falls
    back to deterministic.
    """
    cards_brief = cards_summary_for_llm(cards)
    prompt = (
        "You are Clawta, the swarm dispatcher. Route this ticket to the best "
        "frontier-coder agent + model based on the classification and the "
        "available agent cards.\n\n"
        f"Classification: {json.dumps(classify)}\n\n"
        f"Available agents: {json.dumps(cards_brief)}\n\n"
        "Decision rules:\n"
        "- Higher complexity prefers stronger model (higher tier / premium_cost) "
        "even if more expensive\n"
        "- Capability fit beats cost when needs_frontier is true\n"
        "- For low-complexity tasks, pick the cheapest agent that covers "
        "the capabilities\n"
        "- Prefer claude-code for typed-code/refactor work, codex for "
        "OpenAI-strength reasoning on tough bugs, gemini for long-context, "
        "copilot as fallback / second opinion\n\n"
        "Reply with ONLY a JSON object (no prose, no markdown):\n"
        '{"driver": "<agent id>", "model": "<model id>", "reasoning": '
        '"<one-sentence why>"}'
    )

    try:
        result = subprocess.run(
            ["clawta", "--text", prompt],
            capture_output=True,
            text=True,
            timeout=LLM_TIMEOUT_SECONDS,
        )
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return None

    if result.returncode != 0:
        return None

    body = result.stdout or ""
    # The LLM may wrap JSON in prose or markdown fences; extract via regex.
    match = re.search(r"\{[^{}]*\"driver\"[^{}]*\}", body, re.DOTALL)
    if not match:
        return None

    try:
        parsed = json.loads(match.group(0))
    except json.JSONDecodeError:
        return None

    if not isinstance(parsed.get("driver"), str):
        return None
    return parsed


def main() -> None:
    raw = sys.stdin.read()
    start = raw.find("{")
    if start < 0:
        raise SystemExit("classify produced no JSON object")
    classify = json.loads(raw[start:])
    caps_needed = set(classify.get("capabilities", []))
    cards = load_cards()

    # Smoke / test override.
    force = os.environ.get("FORCE_DRIVER", "").strip()
    if force:
        print(
            json.dumps(
                {
                    "driver": force,
                    "model": None,
                    "complexity": classify.get("complexity"),
                    "caps_needed": sorted(caps_needed),
                    "candidates_considered": 1,
                    "router_mode": "forced",
                    "elo_consulted": False,
                    "reasoning": "FORCE_DRIVER env var bypassed routing logic",
                }
            )
        )
        return

    router_mode = os.environ.get("ROUTER_MODE", "llm").strip().lower()

    # LLM-routing path with deterministic fallback.
    llm_result = None
    chosen_card = None
    if router_mode == "llm":
        llm_result = llm_pick(classify, cards)
        if llm_result:
            driver_id = llm_result["driver"]
            chosen_card = next((c for c in cards if c.get("id") == driver_id), None)
            if chosen_card is None:
                llm_result = None  # Drop through to deterministic fallback

    if llm_result and chosen_card:
        model = llm_result.get("model") or cheapest_model(chosen_card)
        print(
            json.dumps(
                {
                    "driver": llm_result["driver"],
                    "model": model,
                    "complexity": classify.get("complexity"),
                    "caps_needed": sorted(caps_needed),
                    "candidates_considered": len(cards),
                    "router_mode": "llm",
                    "elo_consulted": False,  # Slice 5 of architecture epic wires this
                    "reasoning": llm_result.get("reasoning", ""),
                }
            )
        )
        return

    # Deterministic fallback.
    pick, candidates = deterministic_pick(classify, cards)
    model = cheapest_model(pick) if pick.get("id") != "unassigned" else None
    print(
        json.dumps(
            {
                "driver": pick.get("id"),
                "model": model,
                "complexity": classify.get("complexity"),
                "caps_needed": sorted(caps_needed),
                "candidates_considered": len(candidates),
                "router_mode": "deterministic",
                "elo_consulted": False,
                "reasoning": (
                    "deterministic capability-filter + cheapest-cost rank"
                    if pick.get("id") != "unassigned"
                    else "no candidates covered required capabilities"
                ),
            }
        )
    )


if __name__ == "__main__":
    main()
