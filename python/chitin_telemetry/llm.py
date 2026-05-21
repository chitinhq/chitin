"""Local LLM client (qwen3.6:27b via ollama HTTP API).

Replaces the per-call `ollama run` subprocess pattern with HTTP calls
against the already-running `ollama serve` daemon. This:

  * keeps the model warm via `keep_alive=5m` (no reload between calls)
  * uses qwen3's `think=false` flag to suppress chain-of-thought output
    at the protocol level (cleaner than post-strip)
  * lets us thread timeouts, retries, and a circuit breaker through one
    chokepoint

Every call is logged to `llm_calls` with retention metadata so the
nightly archive sweep can compress and prune old prompts.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import sqlite3
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from chitin_telemetry import gpu

OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://127.0.0.1:11434")
DEFAULT_MODEL = os.environ.get("ARGUS_MODEL", "qwen3.6:27b")
DEFAULT_KEEPALIVE = os.environ.get("ARGUS_OLLAMA_KEEPALIVE", "5m")
DEFAULT_TIMEOUT_SECONDS = int(os.environ.get("ARGUS_LLM_TIMEOUT", "120"))
DAILY_LLM_CAP = int(os.environ.get("ARGUS_DAILY_LLM_CAP", "800"))
LLM_CALL_RETENTION_DAYS = int(os.environ.get("ARGUS_LLM_RETENTION_DAYS", "30"))


# A think-block stripper for models that ignore think=false. qwen3 wraps
# its reasoning in `Thinking...` lines that end with `...done thinking.`
# followed by a blank line then the actual response.
_DONE_THINKING_RE = re.compile(r"^\s*\.\.\.done thinking\.\s*$", re.MULTILINE)
_LEADING_THINKING_RE = re.compile(r"^\s*Thinking\.\.\.\s*$", re.MULTILINE)
_THINK_TAG_RE = re.compile(r"<think>.*?</think>", re.DOTALL | re.IGNORECASE)


def strip_thinking(text: str) -> str:
    """Strip qwen chain-of-thought from the response.

    Handles three formats observed in the wild:
      1. `<think>...</think>` XML-style tags.
      2. `Thinking...\\n<reasoning>\\n...done thinking.\\n\\n<answer>`
      3. Leading `Thinking:` prefix on the first line.
    """
    if not text:
        return text

    # XML-style first (cheapest regex)
    text = _THINK_TAG_RE.sub("", text)

    # qwen3 sentinel-marker pattern
    done_match = _DONE_THINKING_RE.search(text)
    if done_match:
        text = text[done_match.end():].lstrip("\n").lstrip()

    # Fallback: if the response *starts* with `Thinking...` but we
    # didn't find a `...done thinking.` marker, drop everything up to
    # the first blank line.
    if _LEADING_THINKING_RE.match(text):
        parts = text.split("\n\n", 1)
        if len(parts) == 2:
            text = parts[1]

    # Simple prefix
    if text.lower().startswith("thinking:"):
        text = text.split("\n", 1)[-1] if "\n" in text else text[len("thinking:"):]

    return text.strip()


@dataclass(frozen=True)
class LlmResult:
    ok: bool
    text: Optional[str]
    prompt_hash: str
    duration_ms: int
    tokens_in: Optional[int]
    tokens_out: Optional[int]
    error: Optional[str]
    skipped_reason: Optional[str] = None


def _prompt_hash(system: str, user: str, model: str) -> str:
    h = hashlib.sha256()
    h.update(model.encode())
    h.update(b"\0")
    h.update(system.encode())
    h.update(b"\0")
    h.update(user.encode())
    return h.hexdigest()


def _today_start_unix() -> int:
    """Unix ts at local midnight."""
    now = time.localtime()
    midnight = time.struct_time(
        (now.tm_year, now.tm_mon, now.tm_mday, 0, 0, 0, now.tm_wday, now.tm_yday, now.tm_isdst)
    )
    return int(time.mktime(midnight))


def count_calls_today(conn: sqlite3.Connection) -> int:
    """Count successful LLM calls since local midnight."""
    cur = conn.execute(
        "SELECT COUNT(*) FROM llm_calls WHERE ts_unix >= ? AND response IS NOT NULL",
        (_today_start_unix(),),
    )
    return int(cur.fetchone()[0])


_consecutive_failures = 0
_circuit_open_until = 0.0


def _circuit_open() -> bool:
    return time.time() < _circuit_open_until


def _record_failure() -> None:
    global _consecutive_failures, _circuit_open_until
    _consecutive_failures += 1
    if _consecutive_failures >= 3:
        # Open the circuit for 5 minutes after 3 consecutive failures.
        _circuit_open_until = time.time() + 300.0


def _record_success() -> None:
    global _consecutive_failures, _circuit_open_until
    _consecutive_failures = 0
    _circuit_open_until = 0.0


def _http_chat(
    system: str,
    user: str,
    *,
    model: str,
    timeout: int,
    think: bool = False,
) -> tuple[Optional[str], Optional[int], Optional[int]]:
    """Single HTTP /api/chat call. Returns (text, tokens_in, tokens_out)."""
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "stream": False,
        "keep_alive": DEFAULT_KEEPALIVE,
        "think": think,
        "options": {
            "num_ctx": int(os.environ.get("ARGUS_NUM_CTX", "8192")),
            "temperature": float(os.environ.get("ARGUS_TEMPERATURE", "0.2")),
        },
    }
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        f"{OLLAMA_HOST}/api/chat",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        payload = json.loads(resp.read().decode("utf-8"))
    text = (payload.get("message") or {}).get("content")
    return text, payload.get("prompt_eval_count"), payload.get("eval_count")


def call(
    conn: sqlite3.Connection,
    *,
    purpose: str,
    system: str,
    user: str,
    model: str = DEFAULT_MODEL,
    timeout: int = DEFAULT_TIMEOUT_SECONDS,
    bypass_cap: bool = False,
    respect_gpu: bool = True,
) -> LlmResult:
    """Single LLM call with budgeting, GPU politeness, and logging.

    The connection must be writable. `purpose` is one of 'narrate',
    'judge', 'hypothesis_next', 'policy_propose', 'distill', 'free_form'.
    Set `bypass_cap=True` for judge calls so the cap counts user-facing
    work only, not internal critique. Set `respect_gpu=False` for fast
    deterministic checks that don't actually invoke the LLM (none yet).
    """
    started = time.time()
    phash = _prompt_hash(system, user, model)

    # GPU politeness — let the operator's opencode have the model.
    if respect_gpu:
        gs = gpu.status()
        if not gs.available:
            return LlmResult(
                ok=False,
                text=None,
                prompt_hash=phash,
                duration_ms=0,
                tokens_in=None,
                tokens_out=None,
                error=None,
                skipped_reason=f"gpu:{gs.reason}",
            )

    if _circuit_open():
        return LlmResult(
            ok=False,
            text=None,
            prompt_hash=phash,
            duration_ms=0,
            tokens_in=None,
            tokens_out=None,
            error=None,
            skipped_reason="circuit_open",
        )

    if not bypass_cap and count_calls_today(conn) >= DAILY_LLM_CAP:
        return LlmResult(
            ok=False,
            text=None,
            prompt_hash=phash,
            duration_ms=0,
            tokens_in=None,
            tokens_out=None,
            error=None,
            skipped_reason="daily_cap",
        )

    err: Optional[str] = None
    text: Optional[str] = None
    tokens_in: Optional[int] = None
    tokens_out: Optional[int] = None
    try:
        text, tokens_in, tokens_out = _http_chat(
            system, user, model=model, timeout=timeout
        )
        if text is None:
            err = "empty_response"
            _record_failure()
        else:
            text = strip_thinking(text)
            _record_success()
    except urllib.error.URLError as e:
        err = f"urlerror:{e}"
        _record_failure()
    except TimeoutError as e:
        err = f"timeout:{e}"
        _record_failure()
    except json.JSONDecodeError as e:
        err = f"jsondecode:{e}"
        _record_failure()
    except Exception as e:  # noqa: BLE001 - log everything, never crash kernel
        err = f"unexpected:{type(e).__name__}:{e}"
        _record_failure()

    duration_ms = int((time.time() - started) * 1000)
    retention_ts = int(time.time()) + LLM_CALL_RETENTION_DAYS * 86400
    redacted_prompt = _apply_redactions(f"<<SYSTEM>>\n{system}\n<<USER>>\n{user}")
    try:
        conn.execute(
            """
            INSERT INTO llm_calls (
                ts_unix, purpose, prompt_hash, prompt, response,
                tokens_in, tokens_out, duration_ms, retention_ts,
                redaction_applied
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                int(started),
                purpose,
                phash,
                redacted_prompt,
                text,
                tokens_in,
                tokens_out,
                duration_ms,
                retention_ts,
                1,
            ),
        )
        conn.commit()
    except sqlite3.Error:
        # The kernel must not fail on logging issues.
        pass

    if err:
        return LlmResult(
            ok=False,
            text=None,
            prompt_hash=phash,
            duration_ms=duration_ms,
            tokens_in=tokens_in,
            tokens_out=tokens_out,
            error=err,
        )
    return LlmResult(
        ok=True,
        text=text,
        prompt_hash=phash,
        duration_ms=duration_ms,
        tokens_in=tokens_in,
        tokens_out=tokens_out,
        error=None,
    )


# ---- Secret redaction -----------------------------------------------------

# Common secret-shape regexes. Best-effort; not comprehensive. Each match
# is replaced by a redaction marker so the raw secret never lands in the
# llm_calls.prompt column. The flag `redaction_applied=1` makes operator
# review obvious.
_REDACTIONS: list[tuple[re.Pattern[str], str]] = [
    (re.compile(r"\bsk-[A-Za-z0-9]{20,}\b"), "[REDACTED:openai_key]"),
    (re.compile(r"\bsk-ant-[A-Za-z0-9_-]{20,}\b"), "[REDACTED:anthropic_key]"),
    (re.compile(r"\bghp_[A-Za-z0-9]{20,}\b"), "[REDACTED:github_token]"),
    (re.compile(r"\bgithub_pat_[A-Za-z0-9_]{20,}\b"), "[REDACTED:github_pat]"),
    (re.compile(r"\bAIza[A-Za-z0-9_-]{20,}\b"), "[REDACTED:google_key]"),
    (re.compile(r"\bAKIA[A-Z0-9]{16}\b"), "[REDACTED:aws_key]"),
    (re.compile(r"\bxox[bpoa]-[A-Za-z0-9-]{20,}\b"), "[REDACTED:slack_token]"),
    (re.compile(r"https://discord(?:app)?\.com/api/webhooks/\d+/[A-Za-z0-9_-]+"), "[REDACTED:discord_webhook]"),
    # Generic high-entropy hex tokens (e.g. 64-char API keys)
    (re.compile(r"\b[A-Fa-f0-9]{64}\b"), "[REDACTED:hex64]"),
]


def _apply_redactions(text: str) -> str:
    for pattern, marker in _REDACTIONS:
        text = pattern.sub(marker, text)
    return text


# ---- Sweep helpers --------------------------------------------------------


def sweep_expired_calls(conn: sqlite3.Connection) -> int:
    """Delete llm_calls rows whose retention_ts is in the past.

    Called by the nightly dream pass. Returns number of rows deleted.
    """
    now = int(time.time())
    cur = conn.execute("DELETE FROM llm_calls WHERE retention_ts < ?", (now,))
    conn.commit()
    return cur.rowcount or 0


def keep_warm(model: str = DEFAULT_MODEL) -> bool:
    """Best-effort ping to keep the model loaded in VRAM.

    Sends a 1-token chat request so ollama refreshes the keepalive timer.
    Returns True on 2xx response.
    """
    try:
        body = {
            "model": model,
            "messages": [{"role": "user", "content": "."}],
            "stream": False,
            "keep_alive": DEFAULT_KEEPALIVE,
            "think": False,
            "options": {"num_predict": 1},
        }
        data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            f"{OLLAMA_HOST}/api/chat",
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return 200 <= resp.status < 300
    except Exception:  # noqa: BLE001
        return False
