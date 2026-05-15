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
import hashlib
import json
import os
import re
import sqlite3
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path


CARDS_DIR = os.path.expanduser(
    os.environ.get("OPENCLAW_AGENT_CARDS_DIR", "~/.openclaw/data/agent-cards")
)
DECISIONS_DB = os.path.expanduser(
    os.environ.get("CLAWTA_DECISIONS_DB", "~/.openclaw/data/clawta_decisions.db")
)
LLM_TIMEOUT_SECONDS = 60
DEFAULT_EXPLORATION_PERCENT = 0
DEFAULT_EXPLORATION_MAX_CANDIDATES = 2
HIGH_RISK_LEVELS = {"high", "irreversible"}
DEFAULT_SOUL_MAP = {
    "correctness": "knuth",
    "architecture": "davinci",
    "dispatch": "sun-tzu",
    "research": "socrates",
    "default": "sun-tzu",
}
SOUL_CATEGORY_PATTERNS = {
    "correctness": re.compile(r"(?i)\b(invariant|gate|run ledger|ledger schema|audit|forensic|contract|schema|regression)\b"),
    "architecture": re.compile(r"(?i)\b(architecture|refactor|reshape|redesign|decompose|interface|boundary)\b"),
    "dispatch": re.compile(r"(?i)\b(dispatch|router|routing|poller|scheduler|sequencer|worker pool)\b"),
    "research": re.compile(r"(?i)\b(research|explore|exploration|investigate|spike|analysis|survey)\b"),
}
FAILURE_KINDS = {
    "empty_branch",
    "gh_pr_create_fail",
    "ci_fail",
    "request_changes_timeout",
}


def env_flag(name: str, default: bool = False) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def env_int(name: str, default: int) -> int:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        return int(raw)
    except ValueError:
        return default


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


def deterministic_ranked_candidates(classify: dict, cards: list[dict]) -> list[dict]:
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

    return ranked


def deterministic_pick(classify: dict, cards: list[dict]) -> tuple[dict, list[dict]]:
    ranked = deterministic_ranked_candidates(classify, cards)
    pick = ranked[0] if ranked else {"id": "unassigned"}
    return pick, ranked


def repo_root() -> Path:
    override = os.environ.get("CHITIN_REPO", "").strip()
    if override:
        return Path(override).expanduser()
    return Path(__file__).resolve().parents[2]


def load_board_soul_map() -> dict[str, str]:
    raw_override = os.environ.get("KANBAN_BOARD_SOUL_MAP", "").strip()
    raw = raw_override
    if not raw:
        board = os.environ.get("KANBAN_BOARD", "chitin").strip() or "chitin"
        kernel_bin = os.environ.get("CHITIN_KERNEL_BIN", "chitin-kernel").strip() or "chitin-kernel"
        try:
            result = subprocess.run(
                [kernel_bin, "board-config", board, "soul_map"],
                capture_output=True,
                text=True,
                timeout=10,
                check=False,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired):
            result = None
        if result and result.returncode == 0:
            raw = result.stdout.strip()

    mapping = dict(DEFAULT_SOUL_MAP)
    if not raw:
        return mapping
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return mapping
    if not isinstance(parsed, dict):
        return mapping
    for key, default in DEFAULT_SOUL_MAP.items():
        value = str(parsed.get(key, default)).strip()
        mapping[key] = value or default
    return mapping


def classify_soul_category(classify: dict) -> str:
    explicit = str(classify.get("soul_category", "")).strip().lower()
    if explicit in DEFAULT_SOUL_MAP:
        return explicit
    haystack = "\n".join(
        str(classify.get(key, ""))
        for key in ("ticket_title", "ticket_body", "notes", "task_class")
    )
    for category, pattern in SOUL_CATEGORY_PATTERNS.items():
        if pattern.search(haystack):
            return category
    caps = {str(cap).strip().lower() for cap in classify.get("capabilities", [])}
    if "refactor" in caps:
        return "architecture"
    if "review" in caps and classify.get("governance_critical"):
        return "correctness"
    return "default"


def souls_dir_candidates() -> list[Path]:
    """Ordered list of directories that may hold ``<soul_id>.md`` files.

    ``CHITIN_SOULS_DIR`` wins when set; both the bare directory and its
    ``canonical``/``experimental`` children are probed so callers can point
    at either layout. Otherwise we fall back to the souls tree under the
    resolved repo root (which honors ``CHITIN_REPO``).
    """
    candidates: list[Path] = []
    override = os.environ.get("CHITIN_SOULS_DIR", "").strip()
    if override:
        base = Path(override).expanduser()
        candidates += [base, base / "canonical", base / "experimental"]
    root = repo_root()
    candidates += [root / "souls" / "canonical", root / "souls" / "experimental"]
    # De-duplicate while preserving order.
    seen: set[Path] = set()
    ordered: list[Path] = []
    for path in candidates:
        if path not in seen:
            seen.add(path)
            ordered.append(path)
    return ordered


def find_soul_file(soul_id: str) -> Path | None:
    """Return the path to ``<soul_id>.md`` if it can be located, else None.

    Returns ``None`` rather than raising — the installed workflow runs from
    ``~/.openclaw/workflows/`` without ``CHITIN_REPO``/``CHITIN_SOULS_DIR``,
    so the souls tree is legitimately absent there and dispatch must still
    proceed.
    """
    for base in souls_dir_candidates():
        candidate = base / f"{soul_id}.md"
        if candidate.is_file():
            return candidate
    return None


def resolve_soul(classify: dict) -> tuple[str, str, str]:
    """Resolve (soul_id, soul_hash, category) without crashing.

    If the soul markdown file genuinely cannot be located on disk — the
    common case for the installed workflow, which runs without
    ``CHITIN_REPO``/``CHITIN_SOULS_DIR`` — fall back to the resolved
    soul_id with an empty hash instead of raising ``FileNotFoundError``.
    Dispatch can still proceed; the soul fingerprint is simply unstamped.
    """
    soul_map = load_board_soul_map()
    category = classify_soul_category(classify)
    soul_id = soul_map.get(category) or soul_map["default"]
    soul_path = find_soul_file(soul_id)
    if soul_path is None:
        return soul_id, "", category
    try:
        content = soul_path.read_text(encoding="utf-8")
    except OSError:
        return soul_id, "", category
    soul_hash = hashlib.sha256(content.encode("utf-8")).hexdigest()
    return soul_id, soul_hash, category


def composite_fingerprint(driver: str | None, model: str | None, soul_id: str, soul_hash: str) -> str:
    payload = f"{driver or ''}{model or ''}{soul_id}{soul_hash}"
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


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


def stronger_model(card: dict, classify: dict) -> str | None:
    models = sorted_models(card)
    if not models:
        return None
    bucket = complexity_bucket(classify)
    if bucket == "medium":
        return models[-1].get("id")
    if len(models) >= 2:
        return models[1].get("id")
    return models[0].get("id")


def decisions_db_path() -> str:
    return os.path.expanduser(os.environ.get("CLAWTA_DECISIONS_DB", DECISIONS_DB))


def shape_bucket(classify: dict) -> str:
    bucket = complexity_bucket(classify)
    caps = sorted(str(cap) for cap in classify.get("capabilities", []))
    caps_part = "+".join(caps) if caps else "none"
    return f"{bucket}|{caps_part}"


def parse_shape_overrides(raw: str) -> dict[str, int]:
    text = raw.strip()
    if not text:
        return {}
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError:
        overrides = {}
        for part in text.split(","):
            key, sep, value = part.partition("=")
            if not sep:
                continue
            try:
                overrides[key.strip()] = int(value.strip())
            except ValueError:
                continue
        return overrides
    if not isinstance(parsed, dict):
        return {}
    overrides = {}
    for key, value in parsed.items():
        try:
            overrides[str(key)] = int(value)
        except (TypeError, ValueError):
            continue
    return overrides


def failure_filter_settings() -> dict[str, object]:
    return {
        "enabled": env_flag("CLAWTA_FAILURE_FILTER_ENABLED", True),
        "threshold": max(0, env_int("CLAWTA_FAILURE_FILTER_THRESHOLD", 3)),
        "window_hours": max(1, env_int("CLAWTA_FAILURE_FILTER_WINDOW_HOURS", 6)),
        "lookback_hours": max(1, env_int("CLAWTA_FAILURE_FILTER_LOOKBACK_HOURS", 24)),
        "threshold_overrides": parse_shape_overrides(
            os.environ.get("CLAWTA_FAILURE_FILTER_THRESHOLDS", "")
        ),
        "window_overrides": parse_shape_overrides(
            os.environ.get("CLAWTA_FAILURE_FILTER_WINDOWS", "")
        ),
    }


def failure_filter_limits(classify: dict, settings: dict[str, object]) -> tuple[int, int]:
    bucket = complexity_bucket(classify)
    shape = shape_bucket(classify)
    threshold = int(settings["threshold"])
    window_hours = int(settings["window_hours"])
    threshold_overrides = dict(settings["threshold_overrides"])
    window_overrides = dict(settings["window_overrides"])
    threshold = threshold_overrides.get(shape, threshold_overrides.get(bucket, threshold))
    window_hours = window_overrides.get(shape, window_overrides.get(bucket, window_hours))
    return max(0, threshold), max(1, window_hours)


def iso_utc(ts: datetime) -> str:
    return ts.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def recent_shape_failures(classify: dict, cards: list[dict]) -> dict[str, int]:
    settings = failure_filter_settings()
    if not bool(settings["enabled"]):
        return {}
    threshold, window_hours = failure_filter_limits(classify, settings)
    if threshold <= 0:
        return {}

    db_path = decisions_db_path()
    if not os.path.exists(db_path):
        return {}

    now = datetime.now(timezone.utc)
    window_floor = iso_utc(now - timedelta(hours=window_hours))
    lookback_floor = iso_utc(now - timedelta(hours=int(settings["lookback_hours"])))
    shape = shape_bucket(classify)
    candidate_ids = [str(card.get("id")) for card in cards if card.get("id")]
    if not candidate_ids:
        return {}

    placeholders = ",".join("?" for _ in FAILURE_KINDS)
    candidate_placeholders = ",".join("?" for _ in candidate_ids)
    query = f"""
        SELECT driver, COUNT(*)
        FROM clawta_decisions
        WHERE shape_bucket = ?
          AND driver IN ({candidate_placeholders})
          AND failure_kind IN ({placeholders})
          AND COALESCE(outcome_ts, ts) >= ?
          AND COALESCE(outcome_ts, ts) >= ?
        GROUP BY driver
    """
    params = [
        shape,
        *candidate_ids,
        *sorted(FAILURE_KINDS),
        lookback_floor,
        window_floor,
    ]
    try:
        with sqlite3.connect(db_path) as conn:
            rows = conn.execute(query, params).fetchall()
    except sqlite3.Error:
        return {}

    return {
        str(driver): int(count)
        for driver, count in rows
        if driver and int(count) >= threshold
    }


def apply_failure_filter(
    classify: dict, ranked_candidates: list[dict]
) -> tuple[list[dict], dict[str, int], str | None]:
    failures = recent_shape_failures(classify, ranked_candidates)
    if not failures:
        return ranked_candidates, {}, None

    filtered = [card for card in ranked_candidates if str(card.get("id")) not in failures]
    if filtered:
        parts = [f"{driver}={count}" for driver, count in sorted(failures.items())]
        return (
            filtered,
            failures,
            "failure-aware demotion removed recent same-shape failures: " + ", ".join(parts),
        )
    return (
        ranked_candidates,
        failures,
        "failure-aware demotion matched every candidate; using full candidate set",
    )


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


def normalize_risk_level(classify: dict) -> str:
    raw = str(classify.get("risk_level", "medium")).strip().lower()
    if raw in {"low", "medium", "high", "irreversible"}:
        return raw
    return "medium"


def stable_roll(classify: dict, salt: str) -> int:
    payload = json.dumps(classify, sort_keys=True, separators=(",", ":"))
    digest = hashlib.sha256(f"{salt}:{payload}".encode("utf-8")).digest()
    return int.from_bytes(digest[:8], "big")


def exploration_settings() -> dict[str, object]:
    percent = env_int("CLAWTA_EXPLORATION_PERCENT", DEFAULT_EXPLORATION_PERCENT)
    percent = max(0, min(percent, 100))
    max_candidates = env_int(
        "CLAWTA_EXPLORATION_MAX_CANDIDATES", DEFAULT_EXPLORATION_MAX_CANDIDATES
    )
    max_candidates = max(0, max_candidates)
    return {
        "percent": percent,
        "max_candidates": max_candidates,
        "allow_high_risk": env_flag("CLAWTA_EXPLORATION_ALLOW_HIGH_RISK", False),
        "allow_governance_critical": env_flag(
            "CLAWTA_EXPLORATION_ALLOW_GOVERNANCE_CRITICAL", False
        ),
    }


def exploration_eligibility(
    classify: dict, settings: dict[str, object]
) -> tuple[bool, str | None]:
    percent = int(settings["percent"])
    if percent <= 0:
        return False, "exploration disabled"
    if complexity_bucket(classify) not in {"low", "medium"}:
        return False, "exploration restricted to low/medium complexity"
    risk_level = normalize_risk_level(classify)
    if risk_level in HIGH_RISK_LEVELS and not bool(settings["allow_high_risk"]):
        return False, f"risk_level={risk_level} excluded from exploration"
    if bool(classify.get("governance_critical")) and not bool(
        settings["allow_governance_critical"]
    ):
        return False, "governance-critical work excluded from exploration"
    return True, None


def choose_exploration_candidate(
    classify: dict, ranked_candidates: list[dict], settings: dict[str, object]
) -> tuple[dict | None, list[dict], str | None]:
    eligible, reason = exploration_eligibility(classify, settings)
    if not eligible:
        return None, [], reason
    if len(ranked_candidates) <= 1:
        return None, [], "no alternate capability-matching candidates"

    max_candidates = int(settings["max_candidates"])
    if max_candidates <= 0:
        return None, [], "exploration candidate pool is disabled"

    pool = ranked_candidates[1 : 1 + max_candidates]
    if not pool:
        return None, [], "no bounded exploration candidates available"

    roll = stable_roll(classify, "exploration-percent") % 100
    if roll >= int(settings["percent"]):
        return None, pool, f"stable roll {roll} >= exploration percent {settings['percent']}"

    choice = pool[stable_roll(classify, "exploration-choice") % len(pool)]
    return choice, pool, None


def emit_result(
    *,
    driver: str | None,
    model: str | None,
    classify: dict,
    caps_needed: set[str],
    candidates_considered: int,
    router_mode: str,
    selection_mode: str,
    reasoning: str,
    card_load_errors: int,
    exploration_candidates_considered: int,
    soul_id: str,
    soul_hash: str,
    soul_category: str,
    shape: str,
) -> None:
    agent_fingerprint = composite_fingerprint(driver, model, soul_id, soul_hash)
    print(
        json.dumps(
            {
                "driver": driver,
                "model": model,
                "soul_id": soul_id,
                "soul_hash": soul_hash,
                "soul_category": soul_category,
                "agent_fingerprint": agent_fingerprint,
                "complexity": classify.get("complexity"),
                "caps_needed": sorted(caps_needed),
                "candidates_considered": candidates_considered,
                "router_mode": router_mode,
                "selection_mode": selection_mode,
                "elo_consulted": False,
                "reasoning": reasoning,
                "card_load_errors": card_load_errors,
                "exploration_candidates_considered": exploration_candidates_considered,
                "shape_bucket": shape,
            }
        )
    )


def main() -> None:
    raw = sys.stdin.read()
    if not raw.strip():
        raise SystemExit("classify produced no JSON object")
    classify = json.loads(raw)
    caps_needed = set(classify.get("capabilities", []))
    cards, load_errors = load_cards()
    soul_id, soul_hash, soul_category = resolve_soul(classify)
    shape = shape_bucket(classify)

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
            selection_mode="exploitation",
            reasoning="FORCE_DRIVER env var bypassed routing logic",
            card_load_errors=len(load_errors),
            exploration_candidates_considered=0,
            soul_id=soul_id,
            soul_hash=soul_hash,
            soul_category=soul_category,
            shape=shape,
        )
        return

    router_mode = os.environ.get("ROUTER_MODE", "llm").strip().lower()
    ranked_candidates = deterministic_ranked_candidates(classify, cards)
    ranked_candidates, demoted_failures, failure_filter_reason = apply_failure_filter(
        classify, ranked_candidates
    )
    exploration_choice, exploration_pool, exploration_reason = choose_exploration_candidate(
        classify, ranked_candidates, exploration_settings()
    )
    if exploration_choice is not None:
        driver_id = str(exploration_choice.get("id") or "unassigned")
        model = stronger_model(exploration_choice, classify)
        reasoning = (
            f"controlled exploration: chose {driver_id} from "
            f"{len(exploration_pool)} bounded alternate capability-matching candidates"
        )
        if failure_filter_reason:
            reasoning = f"{reasoning}; {failure_filter_reason}"
        if load_errors:
            reasoning = f"{reasoning}; {len(load_errors)} card load error(s)"
        emit_result(
            driver=driver_id,
            model=model,
            classify=classify,
            caps_needed=caps_needed,
            candidates_considered=len(ranked_candidates),
            router_mode="deterministic",
            selection_mode="exploration",
            reasoning=reasoning,
            card_load_errors=len(load_errors),
            exploration_candidates_considered=len(exploration_pool),
            soul_id=soul_id,
            soul_hash=soul_hash,
            soul_category=soul_category,
            shape=shape,
        )
        return

    llm_rejection = None
    if router_mode == "llm":
        llm_result = llm_pick(classify, ranked_candidates)
        chosen_card, llm_rejection = validate_llm_pick(
            llm_result, ranked_candidates, caps_needed
        )
        if chosen_card is not None:
            emit_result(
                driver=llm_result["driver"],
                model=llm_result.get("model") or select_model(chosen_card, classify),
                classify=classify,
                caps_needed=caps_needed,
                candidates_considered=len(ranked_candidates),
                router_mode="llm",
                selection_mode="exploitation",
                reasoning=llm_result.get("reasoning", ""),
                card_load_errors=len(load_errors),
                exploration_candidates_considered=len(exploration_pool),
                soul_id=soul_id,
                soul_hash=soul_hash,
                soul_category=soul_category,
                shape=shape,
            )
            return

    pick, candidates = deterministic_pick(classify, cards)
    if demoted_failures:
        pick, candidates = deterministic_pick(classify, ranked_candidates)
    picked = pick.get("id")
    model = select_model(pick, classify) if picked != "unassigned" else None
    if picked == "unassigned":
        reasoning = "no candidates covered required capabilities"
    elif llm_rejection:
        reasoning = f"LLM fallback: {llm_rejection}; deterministic capability-filter + complexity-aware model rank"
        if failure_filter_reason:
            reasoning = f"{reasoning}; {failure_filter_reason}"
    elif failure_filter_reason:
        reasoning = (
            "deterministic capability-filter + complexity-aware model rank; "
            f"{failure_filter_reason}"
        )
    elif exploration_reason:
        reasoning = (
            f"deterministic capability-filter + complexity-aware model rank; "
            f"{exploration_reason}"
        )
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
        selection_mode="exploitation",
        reasoning=reasoning,
        card_load_errors=len(load_errors),
        exploration_candidates_considered=len(exploration_pool),
        soul_id=soul_id,
        soul_hash=soul_hash,
        soul_category=soul_category,
        shape=shape,
    )


if __name__ == "__main__":
    main()
