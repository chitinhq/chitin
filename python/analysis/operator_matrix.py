"""Auto-detect what (driver, model) combos the OPERATOR has access to,
cross-reference against the compatibility seed db, and emit a
concrete "what you can route to + what each cell can do" report.

This is what the design doc calls `chitin doctor matrix`. It answers:
  - Which CLIs do I have installed + authed?
  - Which models can I actually invoke right now (free under sub or
    paid per-token)?
  - For each (driver, model) cell I have, what do public benchmarks
    say about it?
  - What does it cost per call?

Per the design doc's auto-detection layer:
    operator_matrix = default_matrix
                      ∩ (CLIs the operator has installed + authed)
                      ∩ (models each CLI actually offers)
                      ⊕ operator_overrides_in_chitin.yaml

This module produces the FIRST THREE bullet operands. Operator
overrides land via chitin.yaml (separate concern).

Probing strategy per CLI (updated 2026-05-06 — all four cloud CLIs
now use LIVE catalogs, not curated lists):
  - claude:   `claude auth status` → JSON, definitive. Models from
              api.anthropic.com/v1/models w/ OAuth bearer from
              ~/.claude/.credentials.json.
  - copilot:  Models from api.githubcopilot.com/models w/ `gh auth
              token`. Same catalog the CLI uses internally; filtered
              to picker-enabled chat models.
  - codex:    Auth detected via `codex login status`. Models from
              `codex debug models` (raw JSON catalog the CLI uses).
  - gemini:   OAuth refreshed via the embedded gemini-cli client
              creds; tier confirmed via cloudcode-pa loadCodeAssist.
              Model list scoped by tier (the generativelanguage
              listModels endpoint requires a scope this OAuth
              doesn't carry — curated tier-scoped fallback).
  - hermes:   `hermes profile list` → enumerate profiles + their
              configured models.
  - openclaw: binary present, agents enumerated from chitin's
              DriverIdSchema enum (no per-installation list-agents
              command exposed).
  - ollama:   `ollama list` → 100% accurate local + cloud-sub model
              inventory.

Usage:
    cd python/analysis && uv run python -m analysis.operator_matrix detect
    cd python/analysis && uv run python -m analysis.operator_matrix report
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import sqlite3
import subprocess
import sys
from dataclasses import dataclass, field, asdict
from datetime import datetime, timezone
from pathlib import Path

CHITIN_HOME = Path(os.environ.get("CHITIN_HOME") or os.path.expanduser("~/.chitin"))
DB_PATH = CHITIN_HOME / "compatibility.sqlite"
OUT_DIR = Path(__file__).parent / "out"
MATRIX_JSON = CHITIN_HOME / "operator_matrix.json"


@dataclass
class CLIStatus:
    name: str
    installed: bool
    path: str | None = None
    authed: bool | None = None     # None = couldn't determine; True/False = definitive
    auth_detail: dict | None = None
    models: list[str] = field(default_factory=list)
    notes: str = ""


# ─── Provider model lists ──────────────────────────────────────────────────
#
# Strategy by CLI (probed 2026-05-06):
#   claude  → live via api.anthropic.com/v1/models w/ OAuth from
#             ~/.claude/.credentials.json (works)
#   copilot → live via api.githubcopilot.com/models w/ gh auth token
#             (works — full catalog with per-model capabilities)
#   codex   → live via `codex debug models` (works — JSON catalog from
#             the CLI itself, definitive for the operator's plan)
#   gemini  → CodeAssist OAuth confirms tier; the generativelanguage
#             listModels endpoint requires a scope this OAuth doesn't
#             carry. Fall back to a curated list scoped by the
#             confirmed tier.
#
# Curated fallbacks (used only when the live probe fails):

# Gemini Code Assist's loadCodeAssist endpoint returns tier metadata
# but no model catalog. The Gemini CLI itself ships with a hardcoded
# list it routes against. Pro tier users can also call free-tier
# models — the lists below are union, not exclusive.
#
# Source: gemini-cli's internal model list + Google AI Pro docs.
# Update when Google ships new SKUs (no programmatic enumeration today).
GEMINI_PRO_TIER_MODELS = [
    "gemini-2.5-pro",
    "gemini-2.5-flash",
    "gemini-2.5-flash-lite",
    "gemini-3",
    "gemini-3-pro",
    "gemini-3-pro-image",
    # Pro tier ALSO includes everything from free tier:
    "gemini-2.0-flash",
    "gemini-2.0-flash-lite",
]

GEMINI_FREE_TIER_MODELS = [
    "gemini-2.0-flash-lite",
    "gemini-2.0-flash",
    "gemini-2.5-flash-lite",
]


def _http_get_json(url: str, headers: dict, timeout: int = 10) -> dict | None:
    """Tiny stdlib JSON GET — no requests/httpx dependency."""
    import urllib.request
    import urllib.error
    req = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, urllib.error.HTTPError, json.JSONDecodeError, TimeoutError):
        return None


def _http_post_json(url: str, headers: dict, body: dict, timeout: int = 10) -> dict | None:
    import urllib.request
    import urllib.error
    payload = json.dumps(body).encode("utf-8")
    h = {**headers, "Content-Type": "application/json"}
    req = urllib.request.Request(url, data=payload, headers=h, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, urllib.error.HTTPError, json.JSONDecodeError, TimeoutError):
        return None


def _gemini_refresh_token() -> str | None:
    """Refresh the gemini-cli OAuth bearer using its embedded client
    credentials. Returns None if no creds file or refresh fails."""
    creds_path = Path(os.path.expanduser("~/.gemini/oauth_creds.json"))
    if not creds_path.exists():
        return None
    try:
        creds = json.loads(creds_path.read_text())
    except (OSError, json.JSONDecodeError):
        return None
    refresh = creds.get("refresh_token")
    expiry = creds.get("expiry_date") or 0
    now_ms = int(datetime.now(timezone.utc).timestamp() * 1000)
    if expiry > now_ms + 60_000 and creds.get("access_token"):
        return creds["access_token"]
    if not refresh:
        return None
    # Public client credentials embedded in @google/gemini-cli source.
    import urllib.request
    import urllib.parse
    body = urllib.parse.urlencode({
        "client_id": "REDACTED_PUBLIC_GEMINI_OAUTH_ID",
        "client_secret": "REDACTED_PUBLIC_GEMINI_OAUTH_SECRET",
        "refresh_token": refresh,
        "grant_type": "refresh_token",
    }).encode("utf-8")
    req = urllib.request.Request(
        "https://oauth2.googleapis.com/token",
        data=body,
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=8) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("access_token")
    except Exception:
        return None

# Copilot request multipliers per model. From
# docs.github.com/en/copilot/concepts/billing/copilot-requests (May 2026).
# Multiplier 0 = unmetered for paid Copilot plans; multipliers >0 burn
# the operator's monthly Premium-Request budget at that rate. The
# OpenRouter raw $/token still applies if the operator routes the same
# model OUTSIDE Copilot — these two costs answer different questions.
#
# Keys match Copilot's API model id (lowercased). Update as GitHub
# ships new SKUs / renames; the page is the source of truth.
COPILOT_MULTIPLIERS: dict[str, float] = {
    # Free for Copilot Pro
    "gpt-4.1": 0.0,
    "gpt-4.1-2025-04-14": 0.0,
    "gpt-4o": 0.0,
    "gpt-4o-2024-05-13": 0.0,
    "gpt-4o-2024-08-06": 0.0,
    "gpt-4o-2024-11-20": 0.0,
    "gpt-4o-mini": 0.0,
    "gpt-4o-mini-2024-07-18": 0.0,
    # Cheap (≤0.33)
    "gpt-5.4-nano": 0.25,
    "claude-haiku-4.5": 0.33,
    "gpt-5.4-mini": 0.33,
    "gemini-3-flash": 0.33,
    # Standard (x1)
    "gpt-5.2": 1.0,
    "gpt-5.2-codex": 1.0,
    "gpt-5.3-codex": 1.0,
    "gpt-5.4": 1.0,
    "claude-sonnet-4.5": 1.0,
    "claude-sonnet-4.6": 1.0,
    "gemini-2.5-pro": 1.0,
    "gemini-3.1-pro": 1.0,
    # Premium
    "claude-opus-4.5": 3.0,
    "claude-opus-4.6": 3.0,
    "claude-opus-4.7": 15.0,
    "claude-opus-4.6-1m": 30.0,    # "fast mode" SKU
    "gpt-5.5": 7.5,
    # Legacy / deprecated — billed at host rate; unspecified means assume 0
    "gpt-3.5-turbo": 0.0,
    "gpt-3.5-turbo-0613": 0.0,
    "gpt-4": 0.0,
    "gpt-4-0613": 0.0,
    "gpt-4-o-preview": 0.0,
}


def copilot_multiplier(model_id: str) -> float | None:
    """Return Copilot's per-request multiplier (0 = free, 1 = standard,
    higher = premium). Returns None if we don't have a published
    multiplier for this model."""
    return COPILOT_MULTIPLIERS.get(model_id.lower())


# OpenClaw maps each driver-id to a backing agent + model. Per
# DriverIdSchema in libs/contracts:
OPENCLAW_AGENTS = {
    "openclaw-glm-flash": "glm-4.7-flash (3090, local)",
    "openclaw-glm-cloud": "glm-5.1:cloud (Ollama Cloud sub)",
    "openclaw-deepseek": "deepseek-coder (3090, local)",
}


def _which(cmd: str) -> str | None:
    p = shutil.which(cmd)
    return p


def _run(cmd: list[str], timeout: int = 10) -> tuple[int, str, str]:
    """Run a CLI; return (returncode, stdout, stderr). Catches all errors
    so probes never raise."""
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return proc.returncode, proc.stdout, proc.stderr
    except (FileNotFoundError, subprocess.TimeoutExpired, Exception) as e:
        return -1, "", str(e)


# ─── Per-CLI probes ────────────────────────────────────────────────────────

def probe_claude() -> CLIStatus:
    s = CLIStatus(name="claude", installed=False)
    p = _which("claude")
    if not p:
        s.notes = "binary not on PATH"
        return s
    s.installed = True
    s.path = p
    rc, out, _ = _run(["claude", "auth", "status"], timeout=8)
    if rc == 0 and out:
        try:
            ad = json.loads(out)
            s.authed = bool(ad.get("loggedIn"))
            s.auth_detail = {
                "method": ad.get("authMethod"),
                "provider": ad.get("apiProvider"),
                "subscription": ad.get("subscriptionType"),
            }
        except json.JSONDecodeError:
            s.notes = "auth status returned non-JSON"
    # Live model list from Anthropic's API using the OAuth bearer.
    creds = Path(os.path.expanduser("~/.claude/.credentials.json"))
    if creds.exists():
        try:
            tok = json.loads(creds.read_text()).get("claudeAiOauth", {}).get("accessToken")
        except (OSError, json.JSONDecodeError):
            tok = None
        if tok:
            data = _http_get_json(
                "https://api.anthropic.com/v1/models",
                {"Authorization": f"Bearer {tok}", "anthropic-version": "2023-06-01"},
            )
            if data and isinstance(data.get("data"), list):
                s.models = [m["id"] for m in data["data"] if m.get("id")]
                s.notes = f"live catalog from api.anthropic.com ({len(s.models)} models)"
                if s.authed is None:
                    s.authed = True
    return s


def probe_copilot() -> CLIStatus:
    s = CLIStatus(name="copilot", installed=False)
    p = _which("copilot")
    if not p:
        s.notes = "binary not on PATH"
        return s
    s.installed = True
    s.path = p
    # Copilot CLI uses the same auth as `gh` — pull a token, hit the
    # models endpoint. This is the SAME catalog the CLI sees.
    rc, tok, _ = _run(["gh", "auth", "token"], timeout=4)
    tok = tok.strip() if rc == 0 else ""
    if tok:
        data = _http_get_json(
            "https://api.githubcopilot.com/models",
            {
                "Authorization": f"Bearer {tok}",
                "Editor-Version": "vscode/1.95.0",
                "Copilot-Integration-Id": "vscode-chat",
            },
        )
        if data and isinstance(data.get("data"), list):
            s.authed = True
            # Include EVERY chat-completion model the API exposes.
            # picker_enabled=False just means "not in the Chat picker UI"
            # — the model is still callable via the API. That includes
            # free-tier / legacy models (gpt-4o-mini, gpt-3.5-turbo)
            # operators want to route cheap-T0 work to.
            seen: set[str] = set()
            s.models = []
            for m in data["data"]:
                if (m.get("capabilities") or {}).get("type") != "chat":
                    continue
                mid = m["id"]
                if mid in seen:
                    continue
                seen.add(mid)
                s.models.append(mid)
            n_picker = sum(
                1 for m in data["data"]
                if m.get("model_picker_enabled")
                and (m.get("capabilities") or {}).get("type") == "chat"
            )
            n_other = len(s.models) - n_picker
            s.auth_detail = {
                "vendor_breakdown": _vendor_count(data["data"]),
                "picker_enabled": n_picker,
                "api_only": n_other,
            }
            s.notes = (
                f"live catalog from api.githubcopilot.com "
                f"({len(s.models)} chat models: {n_picker} picker, {n_other} api-only/free)"
            )
            return s
    s.authed = False
    s.notes = "no gh token (run `gh auth login`)"
    return s


def probe_codex() -> CLIStatus:
    s = CLIStatus(name="codex", installed=False)
    p = _which("codex")
    if not p:
        return s
    s.installed = True
    s.path = p
    # codex writes "Logged in using ..." to stderr, not stdout.
    rc, out, err = _run(["codex", "login", "status"], timeout=6)
    combined = (out + "\n" + err).strip()
    s.authed = (rc == 0 and "Logged in" in combined)
    if s.authed and combined:
        s.auth_detail = {"login_status": combined.splitlines()[0]}
    # `codex debug models` renders the raw JSON catalog the CLI uses —
    # this is the source of truth for the operator's plan.
    rc, out, _ = _run(["codex", "debug", "models"], timeout=8)
    if rc == 0 and out.strip().startswith("{"):
        try:
            data = json.loads(out)
            s.models = [
                m["slug"] for m in data.get("models", [])
                if m.get("visibility") == "list" and m.get("slug")
            ]
            s.notes = f"live catalog from `codex debug models` ({len(s.models)} models)"
        except json.JSONDecodeError:
            s.notes = "codex debug models returned non-JSON"
    return s


def probe_gemini() -> CLIStatus:
    s = CLIStatus(name="gemini", installed=False)
    p = _which("gemini")
    if not p:
        return s
    s.installed = True
    s.path = p
    # Refresh OAuth + ask the Code Assist endpoint for the user's tier.
    # Tier confirms the model set; the OAuth scope can't enumerate
    # generativelanguage models so we use a tier-scoped curated list.
    tok = _gemini_refresh_token()
    if tok:
        data = _http_post_json(
            "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
            {"Authorization": f"Bearer {tok}"},
            {"metadata": {"pluginType": "GEMINI", "ideType": "IDE_UNSPECIFIED"}},
        )
        if data:
            s.authed = True
            current_tier = (data.get("currentTier") or {}).get("id") or "unknown"
            paid_tier_id = ((data.get("paidTier") or {}).get("id") or "").lower()
            s.auth_detail = {
                "current_tier": current_tier,
                "paid_tier": paid_tier_id or None,
                "project": data.get("cloudaicompanionProject"),
            }
            # paidTier id "g1-pro-tier" = Google One AI Pro.
            if paid_tier_id == "g1-pro-tier" or current_tier in ("standard-tier", "pro-tier"):
                s.models = list(GEMINI_PRO_TIER_MODELS)
                s.notes = f"AI Pro tier confirmed via CodeAssist ({current_tier})"
            else:
                s.models = list(GEMINI_FREE_TIER_MODELS)
                s.notes = f"free tier ({current_tier})"
            return s
    s.authed = False
    s.notes = "no usable OAuth credentials (run `gemini` to log in / refresh)"
    s.models = list(GEMINI_FREE_TIER_MODELS)
    return s


def _vendor_count(models: list[dict]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for m in models:
        v = m.get("vendor") or "unknown"
        counts[v] = counts.get(v, 0) + 1
    return counts


def probe_hermes() -> CLIStatus:
    s = CLIStatus(name="hermes", installed=False)
    p = _which("hermes") or os.path.expanduser("~/.local/bin/hermes")
    if not os.path.exists(p):
        return s
    s.installed = True
    s.path = p
    rc, out, _ = _run([p, "profile", "list"], timeout=8)
    if rc == 0 and out:
        s.authed = True
        # Each profile line: "  Profile  Model  Gateway  Alias"
        # Parse lazily by splitting on whitespace; first column = profile name.
        for ln in out.splitlines():
            ln = ln.strip()
            if not ln or "Profile" in ln or "──" in ln or ln.startswith("#"):
                continue
            # Strip any leading marker characters like ◆
            tok = ln.split()
            if len(tok) >= 2:
                profile = tok[-4] if len(tok) >= 4 else tok[0]
                model = tok[-3] if len(tok) >= 4 else (tok[1] if len(tok) > 1 else "")
                if profile and model and model not in ("(none)", "—"):
                    s.models.append(f"{profile}:{model}")
        s.notes = f"hermes profiles enumerated; {len(s.models)} configured"
    return s


def probe_openclaw() -> CLIStatus:
    s = CLIStatus(name="openclaw", installed=False)
    p = _which("openclaw")
    if not p:
        return s
    s.installed = True
    s.path = p
    s.authed = None
    s.models = list(OPENCLAW_AGENTS.keys())
    s.notes = "openclaw agents enumerated from chitin DriverIdSchema (no programmatic list-agents)"
    return s


def probe_ollama() -> CLIStatus:
    s = CLIStatus(name="ollama", installed=False)
    p = _which("ollama")
    if not p:
        return s
    s.installed = True
    s.path = p
    rc, out, _ = _run(["ollama", "list"], timeout=8)
    if rc == 0 and out:
        s.authed = True
        # Output: header + "<NAME>  <ID>  <SIZE>  <MODIFIED>" per line
        for ln in out.splitlines()[1:]:
            ln = ln.strip()
            if not ln or ln.startswith("NAME"):
                continue
            name = ln.split()[0]
            if name:
                s.models.append(name)
        s.notes = f"{len(s.models)} models pulled (local + cloud-sub)"
    return s


PROBES = [
    probe_claude, probe_copilot, probe_codex, probe_gemini,
    probe_hermes, probe_openclaw, probe_ollama,
]


# ─── Cross-reference operator's models with seed db ────────────────────────

# OpenClaw drivers wrap a backing model. The seed db doesn't have rows
# for the chitin-specific 'openclaw-*' driver names; lookups must go
# against the actual model names. Per chitin's DriverIdSchema comments.
OPENCLAW_DRIVER_TO_MODEL = {
    "openclaw-glm-flash": "glm-4.7-flash",
    "openclaw-glm-cloud": "glm-5.1:cloud",
    "openclaw-deepseek": "deepseek-coder",
}

# Minimum search-token length before broadening. "gpt" alone matches
# every GPT submission ever made (including SOTA gpt-5 high at 88%
# polyglot, which is NOT what gpt-5.0-mini scored). Refuse to broaden
# below this threshold — better to show "no data" than misleading data
# from a different model.
MIN_TOKEN_CHARS = 6


def _normalize_model_id(s: str) -> str:
    """Strip operator-display cruft so the search-token expansion stays
    on the actual model identifier:
      - 'glm-4.7-flash:latest' → 'glm-4.7-flash' (drop ollama ':latest' tag)
      - 'qwen3-coder:480b-cloud' → 'qwen3-coder-480b-cloud' (collapse ':' to '-')
      - '◆default:qwen3-coder:30b' → 'qwen3-coder-30b' (drop hermes profile prefix)
      - 'openai/gpt-4.1' → 'gpt-4.1' (drop provider prefix)
    The expander then walks tokens of the normalized form.
    """
    s = s.lstrip("◆ ").strip()
    if "/" in s:
        s = s.split("/")[-1]
    # Drop ':latest' tag (ollama default)
    if s.endswith(":latest"):
        s = s[: -len(":latest")]
    # Hermes-style 'profile:model[:variant]' — strip leading profile name
    # if it looks like a single-token identifier (no dashes/dots).
    if ":" in s:
        first, _, rest = s.partition(":")
        if first and rest and first.replace("_", "").isalnum() and "-" not in first and "." not in first:
            s = rest
    # Collapse remaining colons to dashes so the dash-walking token
    # expander treats `qwen3-coder-30b` and `qwen3-coder:30b` the same.
    s = s.replace(":", "-")
    return s


def _resolve_lookup_token(model_token: str) -> str:
    """Translate chitin-specific driver IDs to their backing model name,
    then normalize for search.
    """
    bridged = OPENCLAW_DRIVER_TO_MODEL.get(model_token, model_token)
    return _normalize_model_id(bridged)


def _expand_search_tokens(model_token: str) -> list[str]:
    """Generate progressive search tokens, broadest last.

    Refuses to emit tokens shorter than MIN_TOKEN_CHARS to avoid the
    'gpt-5.0-mini matched gpt-5 (high)' false-family-match bug. For
    `claude-sonnet-4-6` → ['claude-sonnet-4-6', 'claude sonnet 4 6',
    'claude-sonnet-4', 'claude sonnet 4', 'claude-sonnet', 'claude sonnet'].
    For `gpt-5.0-mini` → ['gpt-5.0-mini', 'gpt 5.0 mini', 'gpt-5.0',
    'gpt 5.0'] (stops before 'gpt' because 6-char floor).
    """
    t = model_token.lower().split("/")[-1]  # strip provider prefix like 'qwen/qwen3-coder'
    parts = t.split("-")
    tokens: list[str] = []
    seen: set[str] = set()
    for n in range(len(parts), 0, -1):
        for joiner in ("-", " "):
            s = joiner.join(parts[:n])
            if len(s) >= MIN_TOKEN_CHARS and s not in seen:
                tokens.append(s); seen.add(s)
    return tokens


def _model_seed_lookup(
    conn: sqlite3.Connection, model_token: str
) -> tuple[dict[str, dict[str, tuple[float, str]]], str | None]:
    """For a given model (or driver-id), pull matching seed rows.

    Returns ({source: {metric: (value, source_model_name)}}, matched_token).
    The source_model_name lets the report show WHICH leaderboard row each
    score came from — critical for family-fallback transparency
    ("aider-polyglot 88% (matched 'gpt-5 (high)')" tells the operator the
    score is from a different sibling model, not their actual one).
    """
    resolved = _resolve_lookup_token(model_token)
    out: dict[str, dict[str, tuple[float, str]]] = {}
    matched_token: str | None = None
    for token in _expand_search_tokens(resolved):
        cur = conn.execute(
            "SELECT model, source, metric, value FROM model_seeds "
            "WHERE LOWER(model) LIKE ? "
            "ORDER BY source, metric, value DESC",
            (f"%{token}%",),
        )
        rows = cur.fetchall()
        if rows:
            for matched_model, src, metric, value in rows:
                # Keep the BEST value per (source, metric); record which
                # model row contributed it.
                cur_entry = out.get(src, {}).get(metric)
                if cur_entry is None or value > cur_entry[0]:
                    out.setdefault(src, {})[metric] = (value, matched_model)
            matched_token = token
            break
    return out, matched_token


def detect_matrix() -> dict:
    statuses = [p() for p in PROBES]
    matrix = {
        "detected_at": datetime.now(timezone.utc).isoformat(),
        "clis": [asdict(s) for s in statuses],
        "summary": {
            "n_installed": sum(1 for s in statuses if s.installed),
            "n_authed": sum(1 for s in statuses if s.authed is True),
            "n_models_total": sum(len(s.models) for s in statuses),
        },
    }
    return matrix


def cmd_detect(_args) -> None:
    m = detect_matrix()
    MATRIX_JSON.parent.mkdir(parents=True, exist_ok=True)
    MATRIX_JSON.write_text(json.dumps(m, indent=2))
    print(f"operator matrix → {MATRIX_JSON}")
    for s in m["clis"]:
        marker = "✓" if s["installed"] else "✗"
        auth = ("authed" if s["authed"] else "?") if s["installed"] else "—"
        print(f"  {marker} {s['name']:10} [{auth:>6}]  models={len(s['models'])}  {s.get('notes', '')[:60]}")
    print(f"\ntotal: {m['summary']['n_installed']} CLIs installed, {m['summary']['n_models_total']} models reachable")


def cmd_report(_args) -> None:
    m = detect_matrix()
    if not DB_PATH.exists():
        print(f"compat db not found at {DB_PATH} — run `compatibility_seed mine --source all` first")
        return
    conn = sqlite3.connect(DB_PATH)

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    md_path = OUT_DIR / f"operator-matrix-{datetime.now(timezone.utc).date()}.md"
    lines = [
        f"# Operator matrix — {datetime.now(timezone.utc).date()}",
        "",
        f"Detected at {m['detected_at']}.",
        f"CLIs installed: {m['summary']['n_installed']}/7. Models reachable: {m['summary']['n_models_total']}.",
        "",
        "## Per-CLI status",
        "",
        "| CLI | Installed | Authed | # models | Notes |",
        "|---|---|---|---|---|",
    ]
    for s in m["clis"]:
        inst = "✓" if s["installed"] else "✗"
        auth = ("✓" if s["authed"] else "?") if s["installed"] else "—"
        lines.append(f"| {s['name']} | {inst} | {auth} | {len(s['models'])} | {(s.get('notes') or '')[:60]} |")

    lines += ["", "## Per-(driver, model) capability matrix", "",
              "Cross-references each (driver, model) cell the operator has against the seed db. "
              "Empty cells = no public benchmark data found (or model name didn't fuzzy-match).",
              ""]

    for s in m["clis"]:
        if not s["installed"] or not s["models"]:
            continue
        lines.append(f"### {s['name']}")
        lines.append("")
        for model in s["models"]:
            # Pass the full operator-display name; _model_seed_lookup
            # handles normalization (strip ':latest', collapse colons,
            # bridge openclaw drivers, etc.) consistently. A previous
            # pre-strip of `split(":")[-1]` turned 'gemma4:latest' into
            # 'latest' and matched every chatgpt-4o-latest row.
            seeds, matched_token = _model_seed_lookup(conn, model)
            scores: list[str] = []
            cost_in: float | None = None
            cost_model: str | None = None
            sources_used: set[str] = set()
            for src, metrics in seeds.items():
                if src == "openrouter":
                    if "prompt_token_cost_usd" in metrics:
                        cost_in, cost_model = metrics["prompt_token_cost_usd"]
                    continue
                # Headline metric per source — surface value + which model row contributed it.
                # web-search and huggingface sources: the metric IS the benchmark
                # name (LLM extraction populates it freely), so surface every metric
                # in this source. The trailing tag distinguishes their provenance.
                if src in ("web-search", "huggingface"):
                    tag = "web" if src == "web-search" else "hf"
                    for metric, (v, src_model) in metrics.items():
                        if metric == "extraction_attempted":
                            continue  # failure stub, don't render
                        label = f"{metric}:{v:.1f}"
                        scores.append(f"{label} (`{src_model[:40]}`, {tag})")
                        sources_used.add(src)
                    continue
                metric_key = (
                    "pass_rate_2" if src.startswith("aider")
                    else "resolved_rate" if src.startswith("swebench")
                    else "pass@1_humaneval+" if src == "evalplus"
                    else "score" if src == "arena-hard"
                    else None
                )
                if metric_key and metric_key in metrics:
                    v, src_model = metrics[metric_key]
                    label = f"{src}:{v:.1f}{'%' if src != 'arena-hard' else ''}"
                    scores.append(f"{label} (`{src_model[:40]}`)")
                    sources_used.add(src)
            cost_str = (f"${cost_in:.7f}/in-tok" + (f" (`{cost_model[:30]}`)" if cost_model else "")
                        ) if cost_in is not None else "(no cost data)"
            # For Copilot, surface the request multiplier — that's the
            # operator's REAL marginal cost on a Copilot Pro sub.
            # Multiplier 0 means unmetered (free above the per-month
            # request cap); >1 means burns budget at that rate.
            if s["name"] == "copilot":
                mult = copilot_multiplier(model)
                if mult is not None:
                    if mult == 0.0:
                        cost_str += "  | **Copilot: x0 (free for Pro)**"
                    elif mult < 1.0:
                        cost_str += f"  | Copilot: x{mult:g} (cheap)"
                    elif mult == 1.0:
                        cost_str += f"  | Copilot: x1 (standard)"
                    else:
                        cost_str += f"  | Copilot: **x{mult:g}** (premium)"
            score_str = "; ".join(scores) if scores else "(no benchmark data — too new for any seed source)"
            target = (model.lower().split(":")[-1] if ":" in model else model.lower())
            target = OPENCLAW_DRIVER_TO_MODEL.get(target, target)  # show resolved target for openclaw
            match_note = ""
            if matched_token and matched_token != target:
                match_note = f"  _(family fallback: matched `{matched_token}`)_"
            lines.append(f"- **{model}** — {cost_str}{match_note}")
            lines.append(f"  - {score_str}")
        lines.append("")

    md_path.write_text("\n".join(lines))
    print(f"wrote {md_path}")
    print("\n".join(lines[:30]))
    print("...")


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="cmd")
    sub.add_parser("detect").set_defaults(func=cmd_detect)
    sub.add_parser("report").set_defaults(func=cmd_report)
    args = p.parse_args()
    if not getattr(args, "func", None):
        p.print_help()
        sys.exit(1)
    args.func(args)


if __name__ == "__main__":
    main()
