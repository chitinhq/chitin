#!/usr/bin/env python3
"""Pick driver step for kanban-dispatch.lobster.

Hybrid LLM + deterministic routing. The LLM makes the first routing
judgment when ROUTER_MODE is unset or "llm"; deterministic
capability/cost ranking remains the fallback whenever the LLM call fails
or returns an unsafe pair.

Inputs:
  stdin   - JSON from classify step: {complexity, capabilities, estimated_loc, needs_frontier}
  env     - FORCE_DRIVER (smoke override; if set, skip LLM + deterministic both)
  env     - ROUTER_MODE (default "llm"; set to "deterministic" to skip LLM)
  env     - OPENCLAW_AGENT_CARDS_DIR (test/operator override for card directory)
  files   - ~/.openclaw/data/agent-cards/*.json

Output schema (stdout JSON):
  {
    "driver":               <agent id> | "unassigned",
    "model":                <model id> | null,
    "complexity":           <echo>,
    "caps_needed":          <echo>,
    "candidates_considered": <int>,
    "router_mode":          "llm" | "deterministic" | "forced",
    "elo_consulted":        false,
    "reasoning":            "<one-line justification>",
    "card_load_errors":      <int>
  }
"""

import glob
import json
import os
import re
import subprocess
import sys


CARDS_DIR = os.path.expanduser(
    os.environ.get("OPENCLAW_AGENT_CARDS_DIR", "~/.openclaw/data/agent-cards")
)
LLM_TIMEOUT_SECONDS = 60


def load_cards(cards_dir: str = CARDS_DIR) -> tuple[list[dict], list[str]]:
    cards = []
    errors = []
    paths = sorted(glob.glob(f"{cards_dir}/*.json"))
    if not paths:
        errors.append(f"{cards_dir}: no agent card JSON files found")
    for path in paths:
        try:
            with open(path, encoding="utf-8") as f:
                card = json.load(f)
        except Exception as e:
            errors.append(f"{path}: {e}")
            continue
        if not isinstance(card, dict):
            errors.append(f"{path}: card root must be a JSON object")
            continue
        cards.append(card)
    for error in errors:
        print(f"_pick_driver: failed to load agent card: {error}", file=sys.stderr)
    return cards, errors


def card_capabilities(card: dict) -> set[str]:
    return {
        x["skill"]
        for x in card.get("capabilities", [])
        if isinstance(x, dict) and isinstance(x.get("skill"), str)
    }


def model_ids(card: dict) -> set[str]:
    return {
        m["id"]
        for m in card.get("models", [])
        if isinstance(m, dict) and isinstance(m.get("id"), str)
    }


DEPTH_SCORE = {"expert": 4, "strong": 3, "moderate": 2, "basic": 1}


def capability_depth_score(card: dict, caps_needed: set[str]) -> int:
    depths = {
        x.get("skill"): DEPTH_SCORE.get(str(x.get("depth", "")).lower(), 0)
        for x in card.get("capabilities", [])
        if isinstance(x, dict)
    }
    return sum(depths.get(cap, 0) for cap in caps_needed)


def min_model_cost(card: dict) -> float:
    return min(
        (
            m.get("premium_cost", 99.0)
            for m in card.get("models", [])
            if isinstance(m, dict)
        ),
        default=99.0,
    )


def max_model_cost(card: dict) -> float:
    return max(
        (
            m.get("premium_cost", -1.0)
            for m in card.get("models", [])
            if isinstance(m, dict)
        ),
        default=-1.0,
    )


def deterministic_pick(classify: dict, cards: list[dict]) -> tuple[dict, list[dict]]:
    """Capability-filter plus complexity-aware driver rank."""
    caps_needed = set(classify.get("capabilities", []))
    candidates = [c for c in cards if caps_needed.issubset(card_capabilities(c))]
    bucket = complexity_bucket(classify)

    if bucket == "high":
        ranked = sorted(
            candidates,
            key=lambda c: (
                -capability_depth_score(c, caps_needed),
                -max_model_cost(c),
                min_model_cost(c),
            ),
        )
    elif bucket == "medium":
        ranked = sorted(
            candidates,
            key=lambda c: (
                -capability_depth_score(c, caps_needed),
                min_model_cost(c),
            ),
        )
    else:
        ranked = sorted(candidates, key=min_model_cost)

    pick = ranked[0] if ranked else {"id": "unassigned"}
    return pick, candidates


def sorted_models(card: dict) -> list[dict]:
    return sorted(
        (m for m in card.get("models", []) if isinstance(m, dict)),
        key=lambda m: m.get("premium_cost", 99.0),
    )


def complexity_bucket(classify: dict) -> str:
    raw = str(classify.get("complexity", "")).strip().lower()
    if raw in {"hi", "high", "hard", "complex"}:
        return "high"
    if raw in {"med", "medium", "moderate"}:
        return "medium"
    if classify.get("needs_frontier") is True:
        return "high"
    return "low"


def select_model(card: dict, classify: dict) -> str | None:
    """Pick a model within the selected driver based on task complexity.

    Low complexity gets the cheapest model. Medium gets a middle-cost model
    when available. High/needs_frontier gets the strongest listed model. The
    card order is not trusted; premium_cost is the operator-maintained strength
    proxy used elsewhere by the dispatch stack.
    """
    models = sorted_models(card)
    if not models:
        return None
    bucket = complexity_bucket(classify)
    if bucket == "high":
        return models[-1].get("id")
    if bucket == "medium":
        return models[len(models) // 2].get("id")
    return models[0].get("id")


def cheapest_model(card: dict) -> str | None:
    models = sorted_models(card)
    return models[0].get("id") if models else None


def cards_summary_for_llm(cards: list[dict]) -> list[dict]:
    """Trim agent cards to the fields the LLM needs to make a routing call."""
    out = []
    for c in cards:
        out.append(
            {
                "id": c.get("id"),
                "description": c.get("description", "")[:200],
                "capabilities": [
                    {"skill": x.get("skill"), "depth": x.get("depth")}
                    for x in c.get("capabilities", [])
                    if isinstance(x, dict)
                ],
                "models": [
                    {
                        "id": m.get("id"),
                        "tier": m.get("tier"),
                        "premium_cost": m.get("premium_cost"),
                    }
                    for m in c.get("models", [])
                    if isinstance(m, dict)
                ],
            }
        )
    return out


def llm_pick(classify: dict, cards: list[dict]) -> dict | None:
    """Call Clawta for routing judgment; return parsed result or None."""
    cards_brief = cards_summary_for_llm(cards)
    prompt = (
        "You are Clawta, the swarm dispatcher. Route this ticket to the best "
        "frontier-coder agent + model based on the classification and the "
        "available agent cards.\n\n"
        f"Classification: {json.dumps(classify)}\n\n"
        f"Available agents: {json.dumps(cards_brief)}\n\n"
        "Decision rules:\n"
        "- Choose only an agent id present in Available agents.\n"
        "- Choose only a model id present on that agent card.\n"
        "- Capability fit is mandatory; never choose an agent missing a required capability.\n"
        "- Higher complexity prefers stronger model (higher tier / premium_cost) even if more expensive.\n"
        "- For low-complexity tasks, pick the cheapest agent that covers the capabilities.\n\n"
        "Reply with ONLY a JSON object (no prose, no markdown):\n"
        '{"driver": "<agent id>", "model": "<model id>", "reasoning": "<one-sentence why>"}'
    )

    try:
        result = subprocess.run(
            ["clawta", "--text", prompt],
            capture_output=True,
            text=True,
            timeout=LLM_TIMEOUT_SECONDS,
            check=False,
        )
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return None

    if result.returncode != 0:
        return None

    body = result.stdout or ""
    match = re.search(r"\{[^{}]*\"driver\"[^{}]*\}", body, re.DOTALL)
    if not match:
        return None

    try:
        parsed = json.loads(match.group(0))
    except json.JSONDecodeError:
        return None

    if not isinstance(parsed.get("driver"), str):
        return None
    if parsed.get("model") is not None and not isinstance(parsed.get("model"), str):
        return None
    return parsed


def validate_llm_pick(
    llm_result: dict | None, cards: list[dict], caps_needed: set[str]
) -> tuple[dict | None, str | None]:
    if not llm_result:
        return None, "LLM routing unavailable or returned unparsable output"

    driver_id = llm_result["driver"]
    chosen_card = next((c for c in cards if c.get("id") == driver_id), None)
    if chosen_card is None:
        return None, f"LLM chose unknown driver {driver_id!r}"

    missing_caps = sorted(caps_needed - card_capabilities(chosen_card))
    if missing_caps:
        return None, (
            f"LLM chose {driver_id!r} but it lacks required capabilities: "
            f"{', '.join(missing_caps)}"
        )

    model = llm_result.get("model")
    if model and model not in model_ids(chosen_card):
        return None, (
            f"LLM chose invalid model {model!r} for driver {driver_id!r}"
        )

    return chosen_card, None


def emit_result(
    *,
    driver: str | None,
    model: str | None,
    classify: dict,
    caps_needed: set[str],
    candidates_considered: int,
    router_mode: str,
    reasoning: str,
    card_load_errors: int,
) -> None:
    print(
        json.dumps(
            {
                "driver": driver,
                "model": model,
                "complexity": classify.get("complexity"),
                "caps_needed": sorted(caps_needed),
                "candidates_considered": candidates_considered,
                "router_mode": router_mode,
                "elo_consulted": False,
                "reasoning": reasoning,
                "card_load_errors": card_load_errors,
            }
        )
    )


def main() -> None:
    raw = sys.stdin.read()
    start = raw.find("{")
    if start < 0:
        raise SystemExit("classify produced no JSON object")
    classify = json.loads(raw[start:])
    caps_needed = set(classify.get("capabilities", []))
    cards, load_errors = load_cards()

    # Smoke / test override.
    force = os.environ.get("FORCE_DRIVER", "").strip()
    if force:
        forced_card = next((c for c in cards if c.get("id") == force), {})
        emit_result(
            driver=force,
            model=select_model(forced_card, classify),
            classify=classify,
            caps_needed=caps_needed,
            candidates_considered=1,
            router_mode="forced",
            reasoning="FORCE_DRIVER env var bypassed routing logic",
            card_load_errors=len(load_errors),
        )
        return

    router_mode = os.environ.get("ROUTER_MODE", "llm").strip().lower()

    llm_rejection = None
    if router_mode == "llm":
        llm_result = llm_pick(classify, cards)
        chosen_card, llm_rejection = validate_llm_pick(llm_result, cards, caps_needed)
        if chosen_card is not None:
            emit_result(
                driver=llm_result["driver"],
                model=llm_result.get("model") or select_model(chosen_card, classify),
                classify=classify,
                caps_needed=caps_needed,
                candidates_considered=len(cards),
                router_mode="llm",
                reasoning=llm_result.get("reasoning", ""),
                card_load_errors=len(load_errors),
            )
            return

    pick, candidates = deterministic_pick(classify, cards)
    picked = pick.get("id")
    model = select_model(pick, classify) if picked != "unassigned" else None
    if picked == "unassigned":
        reasoning = "no candidates covered required capabilities"
    elif llm_rejection:
        reasoning = f"LLM fallback: {llm_rejection}; deterministic capability-filter + complexity-aware model rank"
    else:
        reasoning = "deterministic capability-filter + complexity-aware model rank"
    if load_errors:
        reasoning = f"{reasoning}; {len(load_errors)} card load error(s)"

    emit_result(
        driver=picked,
        model=model,
        classify=classify,
        caps_needed=caps_needed,
        candidates_considered=len(candidates),
        router_mode="deterministic",
        reasoning=reasoning,
        card_load_errors=len(load_errors),
    )


if __name__ == "__main__":
    main()
