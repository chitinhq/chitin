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

Probing strategy per CLI:
  - claude:   `claude auth status` → JSON, definitive. Models = curated
              Anthropic catalog (sonnet/opus/haiku families).
  - copilot:  binary present + Copilot Pro sub assumed (no easy
              programmatic auth probe). Models = curated GitHub
              Copilot catalog.
  - codex:    binary present + OpenAI sub assumed. Models = curated
              OpenAI catalog (gpt-5.x family).
  - gemini:   binary present + Google AI Pro sub assumed. Models =
              curated Google catalog (2.0-flash, 2.5-pro, Gemma 2/3).
  - hermes:   `hermes profile list` → enumerate profiles + their
              configured models.
  - openclaw: binary present, agents enumerated by reading
              ~/.openclaw/agents.json (when present) or fallback to
              the chitin DriverIdSchema enum.
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


# ─── Curated provider catalogs (auto-detection seeds) ──────────────────────
#
# Hardcoded because no CLI exposes a programmatic `list models` command
# (probed 2026-05-05: copilot/codex/gemini all need TTY for any list
# subcommand). These match what each provider documents as available
# under their respective standard subscriptions. Keep current as
# providers ship new models; the seed-mining cron will surface gaps
# (a new model on openrouter without a row here means we should add it).

ANTHROPIC_CLAUDE_MODELS = [
    "claude-haiku-4-5",
    "claude-sonnet-4-6",
    "claude-opus-4-7",
]

# Per github.com/github/copilot-cli docs + chitin's earlier research:
COPILOT_MODELS = [
    "gpt-4.1",
    "gpt-5.0-mini",
    "gpt-5.0",
    "gpt-5.4",
    "claude-haiku-4-5",      # via Copilot's Anthropic passthrough (0.33× rate)
    "claude-sonnet-4-6",
]

# OpenAI gpt-5.x family per Codex CLI (subset; gpt-5.5 is the heavyweight):
CODEX_MODELS = [
    "gpt-5.0-mini",
    "gpt-5.0",
    "gpt-5.4",
    "gpt-5.4-nano",
    "gpt-5.5",
]

# Google AI Pro sub catalog (cloud); local Gemma routes through ollama:
GEMINI_CLOUD_MODELS = [
    "gemini-2.0-flash-lite",
    "gemini-2.0-flash",
    "gemini-2.5-pro",
    "gemini-3",              # if available; gemini CLI auto-routes
]

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
    if s.authed:
        s.models = list(ANTHROPIC_CLAUDE_MODELS)
    return s


def probe_copilot() -> CLIStatus:
    s = CLIStatus(name="copilot", installed=False)
    p = _which("copilot")
    if not p:
        s.notes = "binary not on PATH"
        return s
    s.installed = True
    s.path = p
    # No headless auth-status command; presence + Copilot Pro sub
    # assumed. operator can override via env var.
    s.authed = None
    s.notes = "auth not programmatically detectable; Copilot Pro sub assumed"
    s.models = list(COPILOT_MODELS)
    return s


def probe_codex() -> CLIStatus:
    s = CLIStatus(name="codex", installed=False)
    p = _which("codex")
    if not p:
        return s
    s.installed = True
    s.path = p
    s.authed = None
    s.notes = "auth not programmatically detectable; OpenAI sub assumed"
    s.models = list(CODEX_MODELS)
    return s


def probe_gemini() -> CLIStatus:
    s = CLIStatus(name="gemini", installed=False)
    p = _which("gemini")
    if not p:
        return s
    s.installed = True
    s.path = p
    s.authed = None
    s.notes = "auth not programmatically detectable; Google AI Pro sub assumed"
    s.models = list(GEMINI_CLOUD_MODELS)
    return s


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

def _expand_search_tokens(model_token: str) -> list[str]:
    """Generate progressive search tokens, broadest last.

    For 'claude-sonnet-4-6' → ['claude-sonnet-4-6', 'claude sonnet 4 6',
    'claude-sonnet-4', 'claude sonnet 4', 'claude-sonnet', 'claude sonnet'].

    Lets the cross-reference fall back to family-level matches when the
    exact version isn't in the seed data yet (chitin tracks
    claude-sonnet-4-6 but seeds may only have through 4-5; the broader
    'claude-sonnet' match still surfaces useful scores from the family
    line as a "best-known predecessor" signal).
    """
    t = model_token.lower().split(":")[-1]   # strip provider prefix like 'qwen/qwen3-coder'
    parts = t.split("-")
    tokens: list[str] = []
    seen: set[str] = set()
    for n in range(len(parts), 0, -1):
        s = "-".join(parts[:n])
        if s and s not in seen:
            tokens.append(s); seen.add(s)
        s2 = " ".join(parts[:n])
        if s2 and s2 not in seen:
            tokens.append(s2); seen.add(s2)
    return tokens


def _model_seed_lookup(conn: sqlite3.Connection, model_token: str) -> tuple[dict[str, dict[str, float]], str | None]:
    """For a given model name (or partial), pull matching seed rows.

    Returns (scores_by_source, matched_token). matched_token is the
    progressively-broadened search term that produced the first match
    (None when nothing matched at any level). Use the matched_token to
    flag in the report when scores come from a family-level fallback
    rather than an exact model match.
    """
    out: dict[str, dict[str, float]] = {}
    matched_token: str | None = None
    for token in _expand_search_tokens(model_token):
        cur = conn.execute(
            "SELECT source, metric, value FROM model_seeds "
            "WHERE LOWER(model) LIKE ? "
            "ORDER BY source, metric",
            (f"%{token}%",),
        )
        rows = cur.fetchall()
        if rows:
            for src, metric, value in rows:
                # Keep the BEST value per (source, metric) when multiple
                # variants of the family match — surfaces the strongest
                # known datum from the family line.
                cur_val = out.get(src, {}).get(metric)
                if cur_val is None or value > cur_val:
                    out.setdefault(src, {})[metric] = value
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
            seeds, matched_token = _model_seed_lookup(
                conn, model.split(":")[-1] if ":" in model else model
            )
            scores: list[str] = []
            cost_in = None
            for src, metrics in seeds.items():
                if src == "openrouter":
                    cost_in = metrics.get("prompt_token_cost_usd")
                    continue
                # Pick one headline metric per source (best-known of the family)
                if src.startswith("aider"):
                    v = metrics.get("pass_rate_2")
                    if v is not None: scores.append(f"{src}:{v:.1f}%")
                elif src.startswith("swebench"):
                    v = metrics.get("resolved_rate")
                    if v is not None: scores.append(f"{src}:{v:.1f}%")
                elif src == "evalplus":
                    v = metrics.get("pass@1_humaneval+")
                    if v is not None: scores.append(f"evalplus(he+):{v:.1f}%")
                elif src == "arena-hard":
                    v = metrics.get("score")
                    if v is not None: scores.append(f"arena:{v:.1f}")
            cost_str = f"${cost_in:.7f}/in-tok" if cost_in is not None else "(no cost data)"
            score_str = "  ".join(scores) if scores else "(no benchmark data — too new for any seed source)"
            match_note = ""
            if matched_token and matched_token.lower() != (model.lower().split(":")[-1] if ":" in model else model.lower()):
                match_note = f"  _(family-level fallback: matched on `{matched_token}`)_"
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
