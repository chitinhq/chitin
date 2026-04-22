# Hermes Dialect Adapter v1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire hermes as the second consumer of chitin's OTEL GenAI ingest workstream (after openclaw) by installing a small hermes plugin that dumps LLM-call events as JSONL and adding a chitin-side translator + CLI subcommand that turns those events into `ModelTurn`s in the event chain.

**Architecture:** Split execution. **Phase A** (hermes-side, owner: **hermes**) — a ~50-line Python plugin at `~/.hermes/plugins/chitin-sink/` registers 5 plugin hooks and appends each event's kwargs to a daily-rotated JSONL file. **Phase B** (chitin-side, owner: **chitin-side / Claude Code**) — a new translator `go/execution-kernel/internal/ingest/hermes.go` parses `post_api_request` lines into `ModelTurn`s (quarantines the rest with `Reason="v1-scope"`) and a new `chitin-kernel ingest-hermes` subcommand drives it. Real-capture handoff in between keeps both sides honest about the actual JSONL shape.

**Tech Stack:** Python 3.11+ (hermes plugin), Go 1.25 (chitin kernel), existing `emit`/`chain`/`event` packages, existing `ModelTurn`/`Quarantine` types from `openclaw.go`.

**Spec:** `docs/superpowers/specs/2026-04-21-hermes-dialect-adapter-v1-design.md`
**Parent workstream:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md`
**Predecessor (same pattern):** `go/execution-kernel/internal/ingest/openclaw.go` (openclaw translator; re-use `ModelTurn`, `Quarantine`, and the openclaw test harness as templates).
**Related probe:** `docs/superpowers/specs/2026-04-20-hermes-probe-design.md` (this plan is the "verdict=yes" follow-up the probe anticipated).

**Executor map:**

| Phase | Tasks | Owner | Where work happens |
|---|---|---|---|
| A — Hermes plugin | 1–5 | **hermes** | `~/.hermes/plugins/chitin-sink/` (outside the chitin repo) |
| B — Real-capture handoff | 6 | **Jared** (1-minute manual step) | `docs/observations/` in chitin repo |
| C — Chitin translator + CLI | 7–12 | **chitin-side (Claude Code)** | `go/execution-kernel/` in chitin repo |
| D — Ship | 13 | **chitin-side** | PR + review cycle |

**Commit / identity reminders (chitin-specific):**
- Git identity: `jpleva91@gmail.com` (chitin OSS identity — do NOT use readybench.io).
- PR flow: non-draft PR → Copilot review → adversarial review (`/review`) → fix all findings → merge on green.
- No content from Readybench / bench-devs — chitin is OSS.
- Chitin kernel owns side effects; TS is read-only. This plan stays in Go + Python (no TS changes).

---

## File Structure

Files this plan creates or modifies:

### Hermes side (Phase A — not in chitin repo)
- **Create:** `~/.hermes/plugins/chitin-sink/__init__.py` — plugin entry point (register, handlers, `_append`, `_scrub`)
- **Create:** `~/.hermes/plugins/chitin-sink/README.md` — one-paragraph operator doc
- **Create:** `~/.hermes/plugins/chitin-sink/test_chitin_sink.py` — pytest unit tests
- **Runtime (auto-created):** `~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl` — daily event file

### Handoff artifact (Phase B — chitin repo)
- **Create:** `docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl` — real captured sample (1 Telegram round-trip)
- **Create:** `docs/observations/2026-04-21-hermes-post-api-request-capture.md` — short characterization

### Chitin side (Phase C — chitin repo)
- **Create:** `go/execution-kernel/internal/ingest/hermes.go` — translator: HermesEvent struct, chain-id helper, synthetic ID helpers, `ParseHermesEvents`, `translatePostAPIRequest`
- **Create:** `go/execution-kernel/internal/ingest/hermes_test.go` — unit tests
- **Create:** `go/execution-kernel/internal/ingest/hermes_integration_test.go` — integration test via subprocess
- **Create:** `go/execution-kernel/internal/ingest/testdata/hermes/` — fixture JSONL files (6 files)
- **Modify:** `go/execution-kernel/cmd/chitin-kernel/main.go` — add `ingest-hermes` subcommand dispatch (line 36 switch) + `cmdIngestHermes` function

---

## Task 0: Create branch

**Files:** None. Repo-level setup.

- [ ] **Step 0.1: Create and check out branch**

```bash
rtk git checkout -b hermes-dialect-adapter-v1
```

Expected: Switched to a new branch 'hermes-dialect-adapter-v1'.

---

# PHASE A — Hermes Plugin [Owner: hermes]

> **Note to hermes:** These tasks run **outside** the chitin repo, in `~/.hermes/plugins/chitin-sink/`. You're writing a Python package that hermes loads as a user plugin. The plugin path is confirmed at `hermes_cli/plugins.py:482`. Each handler is a lambda; the real work is in `_append`. Use pytest. Run tests from the plugin directory.

## Task 1: Scaffold plugin directory + README

**Files:**
- Create: `~/.hermes/plugins/chitin-sink/__init__.py` (minimal stub)
- Create: `~/.hermes/plugins/chitin-sink/README.md`

- [ ] **Step 1.1: Create plugin directory and empty `__init__.py`**

```bash
mkdir -p ~/.hermes/plugins/chitin-sink
touch ~/.hermes/plugins/chitin-sink/__init__.py
```

- [ ] **Step 1.2: Write README.md**

Create `~/.hermes/plugins/chitin-sink/README.md`:

```markdown
# chitin-sink — hermes plugin

Dumps hermes plugin-hook events as JSON Lines so chitin can ingest them.

## What it does

Registers five plugin hooks. For each, appends one line to today's JSONL file:

- `post_api_request` (primary: per-LLM-call telemetry)
- `on_session_start`, `on_session_end` (session boundaries)
- `pre_tool_call`, `post_tool_call` (tool-call visibility)

## Output

`~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl`

Each line:

```json
{"event_type": "...", "ts": "<ISO UTC>", "kwargs": { ... }}
```

The `conversation_history` kwarg is dropped at source — it's large and
recursive, and chitin doesn't need it.

## To disable

Remove this directory. Hermes will pick up the removal on next restart.

## No chitin vocabulary in this plugin

This plugin knows nothing about chitin. It dumps hermes's native hook
kwargs verbatim (minus `conversation_history`). All dialect→canonical
translation lives in chitin's `go/execution-kernel/internal/ingest/hermes.go`.
```

- [ ] **Step 1.3: Commit the scaffolding**

These files are outside the chitin repo. **Do not commit them to chitin.** They live in `~/.hermes/plugins/chitin-sink/` on disk. If you want version history, init a separate git repo there (optional, not required for this plan).

---

## Task 2: TDD — `_append` writes one JSONL line per call

**Files:**
- Create: `~/.hermes/plugins/chitin-sink/test_chitin_sink.py`
- Modify: `~/.hermes/plugins/chitin-sink/__init__.py`

- [ ] **Step 2.1: Write the failing test**

Create `~/.hermes/plugins/chitin-sink/test_chitin_sink.py`:

```python
"""Tests for chitin-sink plugin.

Run from this directory with: python -m pytest test_chitin_sink.py -v
"""
import json
import os
from pathlib import Path

import pytest

import __init__ as chitin_sink  # plugin module


@pytest.fixture
def tmp_home(tmp_path, monkeypatch):
    """Redirect Path.home() so _append writes to a temp dir."""
    monkeypatch.setattr(Path, "home", lambda: tmp_path)
    return tmp_path


def test_append_writes_one_jsonl_line(tmp_home):
    chitin_sink._append("post_api_request", {"session_id": "s1", "api_call_count": 1})

    files = list((tmp_home / "chitin-sink").glob("events-*.jsonl"))
    assert len(files) == 1, f"expected 1 jsonl file, got {files}"

    lines = files[0].read_text().splitlines()
    assert len(lines) == 1
    obj = json.loads(lines[0])
    assert obj["event_type"] == "post_api_request"
    assert "ts" in obj and obj["ts"].endswith("+00:00")  # ISO UTC
    assert obj["kwargs"] == {"session_id": "s1", "api_call_count": 1}


def test_append_filename_is_today(tmp_home):
    from datetime import datetime, timezone
    chitin_sink._append("x", {})
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    expected = tmp_home / "chitin-sink" / f"events-{today}.jsonl"
    assert expected.exists(), f"expected {expected} to exist"


def test_append_scrubs_conversation_history(tmp_home):
    chitin_sink._append("pre_llm_call", {
        "session_id": "s1",
        "conversation_history": [{"role": "user", "content": "x"}],
        "model": "qwen3-coder:30b",
    })
    files = list((tmp_home / "chitin-sink").glob("events-*.jsonl"))
    obj = json.loads(files[0].read_text().splitlines()[0])
    assert "conversation_history" not in obj["kwargs"]
    assert obj["kwargs"]["session_id"] == "s1"
    assert obj["kwargs"]["model"] == "qwen3-coder:30b"


def test_append_handles_non_serializable(tmp_home):
    """Non-JSON-serializable values (like Path objects) must not crash _append."""
    chitin_sink._append("x", {"some_path": Path("/tmp/foo")})
    files = list((tmp_home / "chitin-sink").glob("events-*.jsonl"))
    # Must not raise; the value becomes a string via default=str.
    obj = json.loads(files[0].read_text().splitlines()[0])
    assert isinstance(obj["kwargs"]["some_path"], str)


def test_append_appends_does_not_overwrite(tmp_home):
    chitin_sink._append("a", {"n": 1})
    chitin_sink._append("b", {"n": 2})
    files = list((tmp_home / "chitin-sink").glob("events-*.jsonl"))
    lines = files[0].read_text().splitlines()
    assert len(lines) == 2
    assert json.loads(lines[0])["event_type"] == "a"
    assert json.loads(lines[1])["event_type"] == "b"
```

- [ ] **Step 2.2: Run the test — verify it fails**

```bash
cd ~/.hermes/plugins/chitin-sink
python -m pytest test_chitin_sink.py -v
```

Expected: FAIL with `AttributeError: module '__init__' has no attribute '_append'` (or similar — `_append` is not defined yet).

- [ ] **Step 2.3: Implement `_append` to make tests pass**

Write `~/.hermes/plugins/chitin-sink/__init__.py`:

```python
"""chitin-sink — hermes plugin that dumps plugin-hook events to JSONL.

See README.md for what this plugin captures and why. Handlers are
registered in register(); all writes go through _append().
"""
from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict


# Kwargs dropped before serialization. Reason: these are too large or
# recursive for line-oriented ingest, and chitin doesn't need them.
_SCRUB_KEYS = frozenset({"conversation_history"})


def _today_filename() -> Path:
    today = datetime.now(timezone.utc).strftime("%Y-%m-%d")
    return Path.home() / "chitin-sink" / f"events-{today}.jsonl"


def _scrub(kwargs: Dict[str, Any]) -> Dict[str, Any]:
    return {k: v for k, v in kwargs.items() if k not in _SCRUB_KEYS}


def _append(event_type: str, kwargs: Dict[str, Any]) -> None:
    """Append one {event_type, ts, kwargs} line to today's JSONL file.

    Best-effort: IOError is caught and logged to stderr. The plugin-hook
    dispatcher already catches callback exceptions, so this function is
    free to raise on programmer errors — but I/O is swallowed because
    agent flow must continue even if the disk is full.
    """
    path = _today_filename()
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        line = json.dumps(
            {
                "event_type": event_type,
                "ts": datetime.now(timezone.utc).isoformat(),
                "kwargs": _scrub(kwargs),
            },
            default=str,
        )
        with path.open("a") as f:
            f.write(line + "\n")
    except OSError as exc:
        # Best-effort. The agent loop is worth more than this event.
        import sys
        print(f"[chitin-sink] append failed: {exc}", file=sys.stderr)
```

- [ ] **Step 2.4: Run tests — verify they pass**

```bash
cd ~/.hermes/plugins/chitin-sink
python -m pytest test_chitin_sink.py -v
```

Expected: 5 passed.

---

## Task 3: TDD — `register()` wires five hooks, each calls `_append`

**Files:**
- Modify: `~/.hermes/plugins/chitin-sink/test_chitin_sink.py`
- Modify: `~/.hermes/plugins/chitin-sink/__init__.py`

- [ ] **Step 3.1: Add failing test for `register()`**

Append to `test_chitin_sink.py`:

```python
class FakeCtx:
    """Mimics the register_hook surface of hermes's PluginContext."""
    def __init__(self):
        self.hooks = {}

    def register_hook(self, name, callback):
        self.hooks[name] = callback


def test_register_wires_five_hooks():
    ctx = FakeCtx()
    chitin_sink.register(ctx)
    assert set(ctx.hooks.keys()) == {
        "post_api_request",
        "on_session_start",
        "on_session_end",
        "pre_tool_call",
        "post_tool_call",
    }


def test_registered_hook_calls_append(tmp_home):
    ctx = FakeCtx()
    chitin_sink.register(ctx)
    ctx.hooks["post_api_request"](
        session_id="s1",
        api_call_count=1,
        model="qwen3-coder:30b",
    )
    files = list((tmp_home / "chitin-sink").glob("events-*.jsonl"))
    obj = json.loads(files[0].read_text().splitlines()[0])
    assert obj["event_type"] == "post_api_request"
    assert obj["kwargs"]["session_id"] == "s1"
    assert obj["kwargs"]["api_call_count"] == 1
    assert obj["kwargs"]["model"] == "qwen3-coder:30b"
```

- [ ] **Step 3.2: Run tests — verify the two new tests fail**

```bash
python -m pytest test_chitin_sink.py -v
```

Expected: `test_register_wires_five_hooks` and `test_registered_hook_calls_append` FAIL with `AttributeError: module '__init__' has no attribute 'register'`.

- [ ] **Step 3.3: Implement `register()`**

Append to `~/.hermes/plugins/chitin-sink/__init__.py`:

```python
_HOOK_NAMES = (
    "post_api_request",
    "on_session_start",
    "on_session_end",
    "pre_tool_call",
    "post_tool_call",
)


def _make_handler(event_type: str):
    """Return a lambda that packs kwargs and appends. Bind event_type via closure."""
    def _handler(**kwargs):
        _append(event_type, kwargs)
    return _handler


def register(ctx) -> None:
    """Hermes plugin entry point.

    Called once by hermes's plugin loader at startup. Registers one
    handler per event type; each handler packs its kwargs and appends
    one JSONL line to today's events file.
    """
    for name in _HOOK_NAMES:
        ctx.register_hook(name, _make_handler(name))
```

- [ ] **Step 3.4: Run tests — verify all pass**

```bash
python -m pytest test_chitin_sink.py -v
```

Expected: 7 passed.

---

## Task 4: Install plugin and verify hermes loads it

**Files:** None. Operational step.

- [ ] **Step 4.1: Confirm plugin discoverable**

The plugin is already at `~/.hermes/plugins/chitin-sink/`, which matches the user-plugin discovery path (`hermes_cli/plugins.py:482`). No install step — hermes discovers it at restart.

- [ ] **Step 4.2: Restart the hermes gateway**

```bash
hermes status
# If gateway is running, stop and restart:
hermes gateway run --replace &
```

Alternative, if `gateway run --replace` isn't available:

```bash
pkill -f "hermes_cli.main gateway run" && sleep 1
hermes gateway run &
```

- [ ] **Step 4.3: Verify plugin loaded**

```bash
hermes plugins --help
hermes plugins list 2>&1 | grep -i chitin-sink
```

Expected: `chitin-sink` appears in the list. If not, check `~/.hermes/logs/agent.log` for plugin-loader errors (`hermes logs errors -n 50`).

- [ ] **Step 4.4: Trigger a single real round-trip**

From Telegram (or whatever gateway channel you use): send one message to hermes — e.g., `what is 2 + 2?`. Wait for the reply.

- [ ] **Step 4.5: Verify events file was written**

```bash
ls -la ~/.hermes/chitin-sink/
cat ~/.hermes/chitin-sink/events-$(date -u +%Y-%m-%d).jsonl
```

Expected: at least one `post_api_request` line plus one `on_session_start` (if it was a new session). If empty: check `hermes logs errors -n 50` for chitin-sink errors; debug `_append`.

---

## Task 5: Hand off plugin completion

**Files:** None. Coordination step.

- [ ] **Step 5.1: Confirm phase A done**

Tell Jared: "Phase A is done. The plugin is installed, registered, and emitting events to `~/.hermes/chitin-sink/events-<DATE>.jsonl`. Handing off for the real-capture step (Phase B / Task 6)."

---

# PHASE B — Real-capture handoff [Owner: Jared, 1-minute manual step]

## Task 6: Commit the real capture to chitin repo

**Files:**
- Create: `docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl`
- Create: `docs/observations/2026-04-21-hermes-post-api-request-capture.md`

- [ ] **Step 6.1: Copy today's JSONL into the chitin repo**

```bash
cd /home/red/workspace/chitin
cp ~/.hermes/chitin-sink/events-$(date -u +%Y-%m-%d).jsonl \
   docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl
```

- [ ] **Step 6.2: Write the one-page characterization**

Create `docs/observations/2026-04-21-hermes-post-api-request-capture.md`:

```markdown
# Hermes post_api_request Capture — 2026-04-21

Captured during the hermes dialect adapter v1 implementation, Phase B
of `docs/superpowers/plans/2026-04-21-hermes-dialect-adapter-v1.md`.

## Source

- Hermes version: <paste output of `hermes --version`>
- Model: qwen3-coder:30b (from `~/.hermes/config.yaml`)
- Gateway: Telegram
- Plugin: `~/.hermes/plugins/chitin-sink/` (this plan's Phase A)
- Trigger: one user message + reply round-trip.

## Event-type counts

<paste output of: `jq -r '.event_type' 2026-04-21-hermes-post-api-request-capture.jsonl | sort | uniq -c`>

## One sample `post_api_request` line (pretty-printed)

<paste one `post_api_request` line from the file, pretty-printed via
`jq` — this is the authoritative sample for chitin-side fixtures>

## Notes

- Any kwargs seen that aren't in the `run_agent.py:10919` list?
- Any nesting in `usage` worth calling out (prompt_tokens_details, cached_tokens)?
```

- [ ] **Step 6.3: Commit the capture**

```bash
rtk git add docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl \
             docs/observations/2026-04-21-hermes-post-api-request-capture.md
rtk git commit -m "observations: hermes post_api_request real-capture sample"
```

- [ ] **Step 6.4: Signal Phase C can start**

Tell Claude: "Phase B done. Real sample at `docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl`. Go."

---

# PHASE C — Chitin Translator + CLI [Owner: chitin-side (Claude Code)]

> **Note to Claude:** All code lands in the chitin repo on branch `hermes-dialect-adapter-v1`. Mirror the openclaw conventions: `ModelTurn` struct is re-used (do **not** redefine it); `Quarantine` struct is re-used; `sort.SliceStable` for deterministic order; `exitErr` for CLI errors. The openclaw translator is at `go/execution-kernel/internal/ingest/openclaw.go` — reference it freely but keep files focused on hermes.

## Task 7: Scaffold `hermes.go` — HermesEvent + chain-id helpers

**Files:**
- Create: `go/execution-kernel/internal/ingest/hermes.go`
- Create: `go/execution-kernel/internal/ingest/hermes_test.go`

- [ ] **Step 7.1: Write failing test for `buildHermesChainID`**

Create `go/execution-kernel/internal/ingest/hermes_test.go`:

```go
package ingest

import (
	"testing"
)

func TestBuildHermesChainID_UniformFormat(t *testing.T) {
	traceHex := "00112233445566778899aabbccddeeff"
	spanHex := "0102030405060708"
	got := buildHermesChainID(traceHex, spanHex)
	want := "hermes:00112233445566778899aabbccddeeff:0102030405060708"
	if got != want {
		t.Fatalf("buildHermesChainID mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestHermesSyntheticIDs_DeterministicFromSessionAndCall(t *testing.T) {
	trace1 := hermesSyntheticTraceID("session-abc")
	trace2 := hermesSyntheticTraceID("session-abc")
	if trace1 != trace2 {
		t.Fatalf("trace IDs should be deterministic; got %q vs %q", trace1, trace2)
	}
	if len(trace1) != 32 {
		t.Fatalf("trace ID should be 32 hex chars (128 bits); got len=%d", len(trace1))
	}

	span1 := hermesSyntheticSpanID("session-abc", 1)
	span2 := hermesSyntheticSpanID("session-abc", 1)
	if span1 != span2 {
		t.Fatalf("span IDs should be deterministic; got %q vs %q", span1, span2)
	}
	if len(span1) != 16 {
		t.Fatalf("span ID should be 16 hex chars (64 bits); got len=%d", len(span1))
	}

	// Different call counts must produce different spans.
	spanCall2 := hermesSyntheticSpanID("session-abc", 2)
	if span1 == spanCall2 {
		t.Fatalf("different api_call_count should give different span IDs")
	}

	// Different sessions must produce different traces.
	traceOther := hermesSyntheticTraceID("session-xyz")
	if trace1 == traceOther {
		t.Fatalf("different session_id should give different trace IDs")
	}
}
```

- [ ] **Step 7.2: Run the tests — verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestBuildHermesChainID_UniformFormat|TestHermesSyntheticIDs_DeterministicFromSessionAndCall'
```

Expected: FAIL with `undefined: buildHermesChainID` (and the synthetic-ID helpers).

- [ ] **Step 7.3: Create `hermes.go` with chain-id and synthetic-ID helpers**

Create `go/execution-kernel/internal/ingest/hermes.go`:

```go
// Package ingest — hermes.go is the hermes-dialect translator.
//
// Hermes emits no OTEL telemetry. Its plugin-hook API (`post_api_request`
// in run_agent.py:10919) exposes per-LLM-call data with model, provider,
// token usage, and session correlation — strictly superior to what the
// openclaw OTLP capture provides for per-call observability.
//
// The source-side plugin (`~/.hermes/plugins/chitin-sink/`, outside this
// repo) dumps each hook event as one JSON line to a daily-rotated file.
// This translator parses that JSONL; v1 maps only `post_api_request` to
// ModelTurn and quarantines every other event_type with Reason="v1-scope".
//
// Spec: docs/superpowers/specs/2026-04-21-hermes-dialect-adapter-v1-design.md
package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// buildHermesChainID mirrors the tripartite shape SP-2 adopted for OTEL
// ingest ("otel:<trace>:<span>"), with "hermes:" as an honest-about-source
// prefix. The chain_id is deterministic from (session_id, api_call_count)
// via the synthetic-ID helpers below — so re-ingest of the same JSONL is
// idempotent at the emit layer.
func buildHermesChainID(traceHex, spanHex string) string {
	return "hermes:" + traceHex + ":" + spanHex
}

// hermesSyntheticTraceID derives a deterministic 128-bit (32 hex char)
// trace ID from the hermes session_id. All API calls within one session
// share a trace ID — consistent with OTEL trace semantics (a trace is a
// logical session of work).
func hermesSyntheticTraceID(sessionID string) string {
	sum := sha256.Sum256([]byte("hermes-trace:" + sessionID))
	return hex.EncodeToString(sum[:16]) // 128 bits = 32 hex chars
}

// hermesSyntheticSpanID derives a deterministic 64-bit (16 hex char) span
// ID from (session_id, api_call_count). Unique per API call within a
// session; stable across re-ingests of the same JSONL.
func hermesSyntheticSpanID(sessionID string, apiCallCount int64) string {
	key := fmt.Sprintf("hermes-span:%s:%d", sessionID, apiCallCount)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8]) // 64 bits = 16 hex chars
}
```

- [ ] **Step 7.4: Run the tests — verify they pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestBuildHermesChainID_UniformFormat|TestHermesSyntheticIDs_DeterministicFromSessionAndCall' -v
```

Expected: 2 passed.

- [ ] **Step 7.5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/hermes.go \
             go/execution-kernel/internal/ingest/hermes_test.go
rtk git commit -m "ingest/hermes: chain-id + synthetic trace/span helpers"
```

---

## Task 8: TDD — `ParseHermesEvents` skeleton (v1-scope quarantine + parse_error)

**Files:**
- Modify: `go/execution-kernel/internal/ingest/hermes.go`
- Modify: `go/execution-kernel/internal/ingest/hermes_test.go`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/v1_scope_quarantine.jsonl`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/malformed_line.jsonl`

- [ ] **Step 8.1: Create v1-scope fixture**

Create `go/execution-kernel/internal/ingest/testdata/hermes/v1_scope_quarantine.jsonl`:

```jsonl
{"event_type": "on_session_start", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"session_id": "s1", "model": "qwen3-coder:30b", "platform": "telegram"}}
{"event_type": "pre_tool_call", "ts": "2026-04-21T19:00:01+00:00", "kwargs": {"tool_name": "terminal", "args": {"cmd": "ls"}, "task_id": "t1"}}
{"event_type": "post_tool_call", "ts": "2026-04-21T19:00:02+00:00", "kwargs": {"tool_name": "terminal", "result": "foo\nbar\n", "task_id": "t1"}}
{"event_type": "on_session_end", "ts": "2026-04-21T19:00:03+00:00", "kwargs": {"session_id": "s1", "completed": true, "interrupted": false}}
```

- [ ] **Step 8.2: Create malformed-line fixture**

Create `go/execution-kernel/internal/ingest/testdata/hermes/malformed_line.jsonl`:

```jsonl
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"session_id": "s1", "api_call_count": 1, "usage": {"prompt_tokens": 10, "completion_tokens": 5}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 1.2}}
{not valid json at all
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:02+00:00", "kwargs": {"session_id": "s1", "api_call_count": 2, "usage": {"prompt_tokens": 20, "completion_tokens": 8}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 0.9}}
```

- [ ] **Step 8.3: Write failing tests**

Append to `hermes_test.go`:

```go
import (
	"os"
	"path/filepath"
	"strings"
)

func loadHermesFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hermes", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseHermesEvents_V1ScopeQuarantine(t *testing.T) {
	raw := loadHermesFixture(t, "v1_scope_quarantine.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("want 0 turns (no post_api_request in fixture), got %d", len(turns))
	}
	if len(quarantined) != 4 {
		t.Fatalf("want 4 quarantined (one per non-primary event), got %d", len(quarantined))
	}
	for _, q := range quarantined {
		if q.Reason != "v1-scope" {
			t.Errorf("every line should quarantine with v1-scope, got Reason=%q", q.Reason)
		}
	}
}

func TestParseHermesEvents_MalformedLineQuarantined(t *testing.T) {
	raw := loadHermesFixture(t, "malformed_line.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	// Two valid post_api_request lines + one malformed line.
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
	if len(quarantined) != 1 {
		t.Fatalf("want 1 quarantined, got %d", len(quarantined))
	}
	if quarantined[0].Reason != "parse_error" {
		t.Errorf("malformed line should have Reason=parse_error, got %q", quarantined[0].Reason)
	}
	if !strings.Contains(string(quarantined[0].SpanRaw), "not valid json") {
		t.Errorf("quarantined SpanRaw should preserve the malformed line")
	}
}
```

- [ ] **Step 8.4: Run tests — verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents' -v
```

Expected: FAIL with `undefined: ParseHermesEvents`.

- [ ] **Step 8.5: Implement `ParseHermesEvents` skeleton**

Append to `hermes.go`:

```go
import (
	"bufio"
	"bytes"
	"encoding/json"
)

// HermesEvent is one line in a chitin-sink JSONL stream. Kwargs is
// intentionally a generic map — the translator inspects it per event_type.
type HermesEvent struct {
	EventType string                 `json:"event_type"`
	Ts        string                 `json:"ts"`
	Kwargs    map[string]interface{} `json:"kwargs"`
}

// ParseHermesEvents classifies every line of a chitin-sink JSONL stream
// into parseable ModelTurns (v1: only post_api_request) and Quarantine
// records (v1-scope for other event_types, parse_error for malformed JSON,
// missing_fields:<list> for required-attr failures).
//
// Never errors mid-walk: returned error is reserved for structural failures
// like a nil input. Blank lines are skipped.
func ParseHermesEvents(raw []byte) ([]ModelTurn, []Quarantine, error) {
	var turns []ModelTurn
	var quarantined []Quarantine

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	// Some post_api_request lines can be large (usage dict + reply stats);
	// raise the scanner buffer from the 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		// Copy the line — scanner buffer is reused on next Scan().
		lineCopy := append([]byte(nil), line...)

		var ev HermesEvent
		if err := json.Unmarshal(lineCopy, &ev); err != nil {
			quarantined = append(quarantined, Quarantine{
				Reason:  "parse_error",
				SpanRaw: json.RawMessage(lineCopy),
			})
			continue
		}
		if ev.EventType != "post_api_request" {
			quarantined = append(quarantined, Quarantine{
				Reason:   "v1-scope",
				SpanName: ev.EventType,
				SpanRaw:  json.RawMessage(lineCopy),
			})
			continue
		}
		// post_api_request translation arrives in Task 9.
		// For now, quarantine it so the skeleton compiles and the
		// quarantine paths are exercised.
		quarantined = append(quarantined, Quarantine{
			Reason:   "not_yet_implemented",
			SpanName: ev.EventType,
			SpanRaw:  json.RawMessage(lineCopy),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan: %w", err)
	}
	return turns, quarantined, nil
}
```

- [ ] **Step 8.6: Run tests — verify v1-scope and malformed tests pass, malformed-line test will fail because the valid lines get "not_yet_implemented" not turns**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents_V1ScopeQuarantine' -v
```

Expected: PASS.

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents_MalformedLineQuarantined' -v
```

Expected: **still FAIL** — the two valid lines get Reason="not_yet_implemented", not parsed into ModelTurns. Task 9 fixes this. This is expected red at the end of Task 8.

- [ ] **Step 8.7: Commit (TDD red→green partial)**

```bash
rtk git add go/execution-kernel/internal/ingest/hermes.go \
             go/execution-kernel/internal/ingest/hermes_test.go \
             go/execution-kernel/internal/ingest/testdata/hermes/v1_scope_quarantine.jsonl \
             go/execution-kernel/internal/ingest/testdata/hermes/malformed_line.jsonl
rtk git commit -m "ingest/hermes: ParseHermesEvents skeleton — v1-scope + parse_error quarantine"
```

---

## Task 9: TDD — `post_api_request` → `ModelTurn` translation

**Files:**
- Modify: `go/execution-kernel/internal/ingest/hermes.go`
- Modify: `go/execution-kernel/internal/ingest/hermes_test.go`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/post_api_request_happy.jsonl`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/missing_session_id.jsonl`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/missing_usage.jsonl`

- [ ] **Step 9.1: Create happy-path fixture** (derived from the real-capture sample at `docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl` — copy one clean `post_api_request` line and trim to the essential fields)

Create `testdata/hermes/post_api_request_happy.jsonl`:

```jsonl
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"task_id": "t1", "session_id": "s1", "platform": "telegram", "model": "qwen3-coder:30b", "provider": "ollama-launch", "base_url": "http://127.0.0.1:11434/v1", "api_mode": "chat_completions", "api_call_count": 1, "api_duration": 2.345, "finish_reason": "stop", "message_count": 4, "response_model": "qwen3-coder:30b", "usage": {"prompt_tokens": 1024, "completion_tokens": 256, "total_tokens": 1280}, "assistant_content_chars": 1720, "assistant_tool_call_count": 0}}
```

- [ ] **Step 9.2: Create missing-session-id fixture**

Create `testdata/hermes/missing_session_id.jsonl`:

```jsonl
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"api_call_count": 1, "usage": {"prompt_tokens": 10, "completion_tokens": 5}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 1.0}}
```

- [ ] **Step 9.3: Create missing-usage fixture** (`usage` nil is NOT a missing-field — keep as ModelTurn with 0 tokens; spec § Error handling)

Create `testdata/hermes/missing_usage.jsonl`:

```jsonl
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:00+00:00", "kwargs": {"task_id": "t1", "session_id": "s1", "platform": "telegram", "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_call_count": 1, "api_duration": 1.0, "finish_reason": "stop", "response_model": "qwen3-coder:30b", "usage": null, "assistant_content_chars": 300, "assistant_tool_call_count": 0}}
```

- [ ] **Step 9.4: Write failing tests**

Append to `hermes_test.go`:

```go
func TestParseHermesEvents_HappyPath(t *testing.T) {
	raw := loadHermesFixture(t, "post_api_request_happy.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined, got %d: %+v", len(quarantined), quarantined)
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	mt := turns[0]
	if mt.Surface != "hermes" {
		t.Errorf("Surface: got %q want \"hermes\"", mt.Surface)
	}
	if mt.Provider != "ollama-launch" {
		t.Errorf("Provider: got %q", mt.Provider)
	}
	if mt.ModelName != "qwen3-coder:30b" {
		t.Errorf("ModelName: got %q", mt.ModelName)
	}
	if mt.InputTokens != 1024 {
		t.Errorf("InputTokens: got %d", mt.InputTokens)
	}
	if mt.OutputTokens != 256 {
		t.Errorf("OutputTokens: got %d", mt.OutputTokens)
	}
	if mt.SessionIDExternal != "s1" {
		t.Errorf("SessionIDExternal: got %q", mt.SessionIDExternal)
	}
	if mt.DurationMs != 2345 {
		t.Errorf("DurationMs: got %d (want 2345 from api_duration=2.345)", mt.DurationMs)
	}
	if mt.Ts != "2026-04-21T19:00:00+00:00" {
		t.Errorf("Ts: got %q (line-level ts passthrough)", mt.Ts)
	}
	// TraceID / SpanID should be the synthetic hex from session_id + api_call_count.
	wantTrace := hermesSyntheticTraceID("s1")
	wantSpan := hermesSyntheticSpanID("s1", 1)
	if mt.TraceID != wantTrace {
		t.Errorf("TraceID: got %q want %q", mt.TraceID, wantTrace)
	}
	if mt.SpanID != wantSpan {
		t.Errorf("SpanID: got %q want %q", mt.SpanID, wantSpan)
	}
}

func TestParseHermesEvents_MissingSessionID_Quarantined(t *testing.T) {
	raw := loadHermesFixture(t, "missing_session_id.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("want 0 turns, got %d", len(turns))
	}
	if len(quarantined) != 1 {
		t.Fatalf("want 1 quarantined, got %d", len(quarantined))
	}
	if !strings.HasPrefix(quarantined[0].Reason, "missing_fields:") {
		t.Errorf("want Reason to start with 'missing_fields:', got %q", quarantined[0].Reason)
	}
	if !strings.Contains(quarantined[0].Reason, "session_id") {
		t.Errorf("Reason should name session_id, got %q", quarantined[0].Reason)
	}
}

func TestParseHermesEvents_MissingUsage_KeepsTurn(t *testing.T) {
	raw := loadHermesFixture(t, "missing_usage.jsonl")
	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	// usage=null is honest "model didn't emit usage" — keep the turn with 0 tokens.
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined (usage=null is kept), got %d", len(quarantined))
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	if turns[0].InputTokens != 0 || turns[0].OutputTokens != 0 {
		t.Errorf("tokens should be 0 when usage is nil, got in=%d out=%d",
			turns[0].InputTokens, turns[0].OutputTokens)
	}
}
```

- [ ] **Step 9.5: Run tests — verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents_HappyPath|TestParseHermesEvents_MissingSessionID_Quarantined|TestParseHermesEvents_MissingUsage_KeepsTurn' -v
```

Expected: FAIL — the skeleton still quarantines post_api_request with `Reason="not_yet_implemented"`.

- [ ] **Step 9.6: Implement `translatePostAPIRequest`**

Replace the `"not_yet_implemented"` branch in `ParseHermesEvents` with a call to `translatePostAPIRequest`, and add the function. In `hermes.go`:

```go
// Replace the "not_yet_implemented" block in ParseHermesEvents with:
		mt, reason := translatePostAPIRequest(&ev)
		if reason != "" {
			quarantined = append(quarantined, Quarantine{
				Reason:   reason,
				SpanName: ev.EventType,
				SpanRaw:  json.RawMessage(lineCopy),
			})
			continue
		}
		turns = append(turns, mt)
```

Then append the helper:

```go
// translatePostAPIRequest extracts a ModelTurn from one post_api_request
// event. Returns (ModelTurn, "") on success, or (zero, reason) where reason
// is either "missing_fields:<comma-list>" or a typed error.
//
// Required kwargs (quarantine if missing): session_id, api_call_count.
// Optional-but-valuable kwargs (kept as zero if missing): usage (→ tokens),
// model/response_model (→ ModelName), provider, api_duration, usage.prompt_tokens_details.cached_tokens.
func translatePostAPIRequest(ev *HermesEvent) (ModelTurn, string) {
	sessionID, sOK := getKwargString(ev.Kwargs, "session_id")
	callCount, cOK := getKwargInt(ev.Kwargs, "api_call_count")

	var missing []string
	if !sOK || sessionID == "" {
		missing = append(missing, "session_id")
	}
	if !cOK {
		missing = append(missing, "api_call_count")
	}
	if len(missing) > 0 {
		return ModelTurn{}, "missing_fields:" + strings.Join(missing, ",")
	}

	traceHex := hermesSyntheticTraceID(sessionID)
	spanHex := hermesSyntheticSpanID(sessionID, callCount)

	// ModelName: prefer response_model (what the LLM actually ran),
	// fall back to requested model, else empty string (kept).
	modelName, _ := getKwargString(ev.Kwargs, "response_model")
	if modelName == "" {
		modelName, _ = getKwargString(ev.Kwargs, "model")
	}

	provider, _ := getKwargString(ev.Kwargs, "provider")

	// Duration: api_duration is a float (seconds) in the plugin output.
	var durationMs int64
	if dur, ok := getKwargFloat(ev.Kwargs, "api_duration"); ok {
		durationMs = int64(dur*1000 + 0.5) // round-half-up
	}

	// Tokens from usage dict — may be nil.
	var inputTokens, outputTokens, cacheRead int64
	if usage, ok := ev.Kwargs["usage"].(map[string]interface{}); ok && usage != nil {
		inputTokens, _ = getKwargInt(usage, "prompt_tokens")
		outputTokens, _ = getKwargInt(usage, "completion_tokens")
		if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok && details != nil {
			cacheRead, _ = getKwargInt(details, "cached_tokens")
		}
	}

	return ModelTurn{
		TraceID:           traceHex,
		SpanID:            spanHex,
		Ts:                ev.Ts,
		Surface:           "hermes",
		Provider:          provider,
		ModelName:         modelName,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		SessionIDExternal: sessionID,
		DurationMs:        durationMs,
		CacheReadTokens:   cacheRead,
		// CacheWriteTokens: 0 — hermes/ollama don't expose this today.
	}, ""
}

// --- kwarg helpers (JSON-unmarshal types: string / float64 / map / nil) ---

func getKwargString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// getKwargInt accepts JSON numbers (float64) and returns as int64 by truncation
// (the values we look at — api_call_count, prompt_tokens — are always integers
// over the wire; JSON's number is float64 by default in encoding/json).
func getKwargInt(m map[string]interface{}, key string) (int64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func getKwargFloat(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	if f, ok := v.(float64); ok {
		return f, true
	}
	return 0, false
}
```

Also add the missing import: `"strings"`.

- [ ] **Step 9.7: Run tests — verify all pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents' -v
```

Expected: 5 passed (V1ScopeQuarantine, MalformedLineQuarantined, HappyPath, MissingSessionID_Quarantined, MissingUsage_KeepsTurn). The malformed-line test now passes too because the two valid post_api_request lines around the malformed one produce real ModelTurns.

- [ ] **Step 9.8: Run the full ingest test package to verify no openclaw regressions**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -v
```

Expected: all hermes tests pass + all openclaw tests still pass.

- [ ] **Step 9.9: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/hermes.go \
             go/execution-kernel/internal/ingest/hermes_test.go \
             go/execution-kernel/internal/ingest/testdata/hermes/post_api_request_happy.jsonl \
             go/execution-kernel/internal/ingest/testdata/hermes/missing_session_id.jsonl \
             go/execution-kernel/internal/ingest/testdata/hermes/missing_usage.jsonl
rtk git commit -m "ingest/hermes: translate post_api_request → ModelTurn"
```

---

## Task 10: TDD — deterministic order + re-ingest idempotency check

**Files:**
- Modify: `go/execution-kernel/internal/ingest/hermes.go`
- Modify: `go/execution-kernel/internal/ingest/hermes_test.go`
- Create: `go/execution-kernel/internal/ingest/testdata/hermes/multi_call_session.jsonl`

- [ ] **Step 10.1: Create multi-call fixture with intentionally shuffled order**

Create `testdata/hermes/multi_call_session.jsonl`:

```jsonl
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:05+00:00", "kwargs": {"session_id": "s1", "api_call_count": 3, "usage": {"prompt_tokens": 30, "completion_tokens": 10}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 0.8}}
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:01+00:00", "kwargs": {"session_id": "s1", "api_call_count": 1, "usage": {"prompt_tokens": 10, "completion_tokens": 5}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 0.5}}
{"event_type": "post_api_request", "ts": "2026-04-21T19:00:03+00:00", "kwargs": {"session_id": "s1", "api_call_count": 2, "usage": {"prompt_tokens": 20, "completion_tokens": 7}, "model": "qwen3-coder:30b", "provider": "ollama-launch", "api_duration": 0.6}}
```

- [ ] **Step 10.2: Write failing tests for deterministic order and distinct spans**

Append to `hermes_test.go`:

```go
func TestParseHermesEvents_DeterministicOrder_TsAscending(t *testing.T) {
	raw := loadHermesFixture(t, "multi_call_session.jsonl")
	turns, _, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("want 3 turns, got %d", len(turns))
	}
	// Turns must be sorted ascending by Ts regardless of file order.
	for i := 1; i < len(turns); i++ {
		if turns[i-1].Ts > turns[i].Ts {
			t.Errorf("turns not ts-ordered: [%d].Ts=%q > [%d].Ts=%q",
				i-1, turns[i-1].Ts, i, turns[i].Ts)
		}
	}
}

func TestParseHermesEvents_MultiCallSession_DistinctSpanIDs(t *testing.T) {
	raw := loadHermesFixture(t, "multi_call_session.jsonl")
	turns, _, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}
	// All three turns share a trace (same session_id).
	trace := turns[0].TraceID
	for _, mt := range turns {
		if mt.TraceID != trace {
			t.Errorf("same session should share trace; got %q vs %q", mt.TraceID, trace)
		}
	}
	// All three spans must be distinct.
	seen := map[string]bool{}
	for _, mt := range turns {
		if seen[mt.SpanID] {
			t.Errorf("span collision within a session: %q", mt.SpanID)
		}
		seen[mt.SpanID] = true
	}
}
```

- [ ] **Step 10.3: Run tests — verify they fail**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run 'TestParseHermesEvents_DeterministicOrder_TsAscending|TestParseHermesEvents_MultiCallSession_DistinctSpanIDs' -v
```

Expected: `TestParseHermesEvents_DeterministicOrder_TsAscending` FAILS (turns come out in file order, not Ts order). `TestParseHermesEvents_MultiCallSession_DistinctSpanIDs` should PASS already (synthetic spans are unique per api_call_count).

- [ ] **Step 10.4: Add sort to `ParseHermesEvents`**

At the end of `ParseHermesEvents`, before `return turns, quarantined, nil`, add:

```go
	// Deterministic order: Ts ascending, SpanID tie-break. Mirrors
	// openclaw.ParseOpenClawSpans (openclaw.go:76-82). Required so that
	// the event chain emits in reproducible order across re-ingest runs.
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].Ts != turns[j].Ts {
			return turns[i].Ts < turns[j].Ts
		}
		return turns[i].SpanID < turns[j].SpanID
	})
```

Add the `"sort"` import.

- [ ] **Step 10.5: Run tests — verify all pass**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -v
```

Expected: all hermes + openclaw tests pass.

- [ ] **Step 10.6: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/hermes.go \
             go/execution-kernel/internal/ingest/hermes_test.go \
             go/execution-kernel/internal/ingest/testdata/hermes/multi_call_session.jsonl
rtk git commit -m "ingest/hermes: deterministic turn order (Ts asc, SpanID tie-break)"
```

---

## Task 11: `ingest-hermes` CLI subcommand

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/main.go`

- [ ] **Step 11.1: Read the existing `cmdIngestOTEL` for shape reference**

```bash
sed -n '275,400p' go/execution-kernel/cmd/chitin-kernel/main.go
```

Note the flag parsing, dir-default `.chitin`, error reporting via `exitErr`, and the "emit each ModelTurn via emit.Emit" pattern. `cmdIngestHermes` mirrors this.

- [ ] **Step 11.2: Add `ingest-hermes` case to the subcommand switch**

In `go/execution-kernel/cmd/chitin-kernel/main.go`, after the `case "ingest-otel":` block (around line 36-37):

```go
	case "ingest-hermes":
		cmdIngestHermes(args)
```

- [ ] **Step 11.3: Implement `cmdIngestHermes`**

Append to `main.go` (after `cmdIngestOTEL` for locality):

```go
// cmdIngestHermes reads a chitin-sink JSONL file, translates
// post_api_request events into ModelTurn events, and emits each turn
// into the chitin event chain. Matches ingest-otel's CLI shape.
//
// On success, writes one JSON line to stdout:
//
//	{"turns": N, "quarantined": M, "quarantined_by_reason": {"v1-scope": 4, ...}}
func cmdIngestHermes(args []string) {
	fs := flag.NewFlagSet("ingest-hermes", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel ingest-hermes --file <events-YYYY-MM-DD.jsonl> [--dir <dir>]")
		fs.PrintDefaults()
	}
	file := fs.String("file", "", "path to a chitin-sink events JSONL file")
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	fs.Parse(args)

	if *file == "" {
		exitErr("missing_file", "--file is required")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		exitErr("read_file", err.Error())
	}

	turns, quarantined, err := ingest.ParseHermesEvents(raw)
	if err != nil {
		exitErr("parse", err.Error())
	}

	// Init chitin state dir + open chain index (mirrors cmdEmit / cmdIngestOTEL).
	absDir, _ := filepath.Abs(*dir)
	if err := kstate.Init(absDir, false); err != nil {
		exitErr("init", err.Error())
	}
	idx, err := chain.OpenIndex(filepath.Join(absDir, "chain_index.sqlite"))
	if err != nil {
		exitErr("open_index", err.Error())
	}
	defer idx.Close()
	if err := idx.RebuildFromJSONL(absDir); err != nil {
		exitErr("rebuild_index", err.Error())
	}

	emitter := emit.NewEmitter(absDir, idx)
	emittedCount := 0
	for _, mt := range turns {
		chainID := ingest.BuildHermesChainIDFromTurn(mt) // exported helper; adds in Task 11.4
		ev := event.Event{
			SchemaVersion: "2",
			EventType:     "model_turn",
			ChainID:       chainID,
			Ts:            mt.Ts,
			Payload:       ingest.ModelTurnToPayload(mt), // exported helper; adds in Task 11.4
			Labels: map[string]string{
				"source":  "hermes",
				"dialect": "hermes",
			},
		}
		if emitted, err := emitter.EmitIfNew(ev); err != nil {
			exitErr("emit", err.Error())
		} else if emitted {
			emittedCount++
		}
	}

	byReason := map[string]int{}
	for _, q := range quarantined {
		byReason[q.Reason]++
	}
	summary := map[string]interface{}{
		"turns":                 len(turns),
		"emitted":               emittedCount,
		"quarantined":           len(quarantined),
		"quarantined_by_reason": byReason,
	}
	out, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(out))
}
```

- [ ] **Step 11.4: Export the two helpers ingest needs**

`ingest.BuildHermesChainIDFromTurn` and `ingest.ModelTurnToPayload` don't exist yet. Add to `go/execution-kernel/internal/ingest/hermes.go` (export them — capital letters):

```go
// BuildHermesChainIDFromTurn is the exported accessor for the CLI;
// takes an already-translated ModelTurn and returns its chain_id.
// Kept as a single source of truth so the CLI can't drift from the
// translator's chain-id scheme.
func BuildHermesChainIDFromTurn(mt ModelTurn) string {
	return buildHermesChainID(mt.TraceID, mt.SpanID)
}
```

For `ModelTurnToPayload`: check if openclaw already exports something similar. If openclaw's `cmdIngestOTEL` calls a `ModelTurnToPayload` or equivalent, re-use it — do **not** add a parallel hermes version. If it's inline in cmdIngestOTEL, extract it into `openclaw.go` (or a new `model_turn.go`) as an exported helper `ModelTurnToPayload(mt ModelTurn) map[string]interface{}` and have both cmdIngestOTEL and cmdIngestHermes call it. This is a refactor; keep it scoped — don't change openclaw's semantics.

- [ ] **Step 11.5: Verify the build compiles**

```bash
cd go/execution-kernel && go build ./...
```

Expected: clean build. If `emit.NewEmitter` / `emit.EmitIfNew` / `event.Event` field names don't match what I wrote, look at `cmdIngestOTEL` for the actual names — it emits ModelTurns today, so the API it uses is the one to mirror. Update the code in Step 11.3 to match.

- [ ] **Step 11.6: Smoke-test the CLI against the happy fixture**

```bash
cd go/execution-kernel
mkdir -p /tmp/chitin-hermes-smoke
go run ./cmd/chitin-kernel ingest-hermes \
  --file ./internal/ingest/testdata/hermes/post_api_request_happy.jsonl \
  --dir /tmp/chitin-hermes-smoke
```

Expected output (JSON):

```json
{
  "turns": 1,
  "emitted": 1,
  "quarantined": 0,
  "quarantined_by_reason": {}
}
```

Re-run the exact same command. Expected output:

```json
{
  "turns": 1,
  "emitted": 0,         <-- idempotent
  "quarantined": 0,
  "quarantined_by_reason": {}
}
```

- [ ] **Step 11.7: Commit**

```bash
rtk git add go/execution-kernel/cmd/chitin-kernel/main.go \
             go/execution-kernel/internal/ingest/hermes.go
rtk git commit -m "kernel: ingest-hermes subcommand — reads chitin-sink JSONL, emits ModelTurns"
```

---

## Task 12: Integration test

**Files:**
- Create: `go/execution-kernel/internal/ingest/hermes_integration_test.go`

- [ ] **Step 12.1: Skim `openclaw_integration_test.go` to mirror the subprocess pattern**

```bash
sed -n '1,100p' go/execution-kernel/internal/ingest/openclaw_integration_test.go
```

Note how it:
- Builds the kernel binary in a temp dir.
- Runs it as a subprocess with the ingest subcommand.
- Asserts stdout matches expected JSON.
- Re-runs to assert idempotency.

- [ ] **Step 12.2: Write the integration test**

Create `hermes_integration_test.go`:

```go
package ingest

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestIngestHermes_E2E runs the built kernel binary against the happy fixture
// and asserts one ModelTurn is emitted, then re-runs and asserts zero new
// emissions (idempotency). Mirrors openclaw_integration_test.go's pattern.
func TestIngestHermes_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "chitin-kernel")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../../cmd/chitin-kernel")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build kernel: %v\n%s", err, out)
	}

	fixture := filepath.Join("testdata", "hermes", "post_api_request_happy.jsonl")

	// Run 1: first ingest → 1 turn emitted.
	out1, err := exec.Command(binPath, "ingest-hermes",
		"--file", fixture, "--dir", tmpDir).Output()
	if err != nil {
		t.Fatalf("ingest-hermes run 1: %v", err)
	}
	var summary1 struct {
		Turns                int            `json:"turns"`
		Emitted              int            `json:"emitted"`
		Quarantined          int            `json:"quarantined"`
		QuarantinedByReason  map[string]int `json:"quarantined_by_reason"`
	}
	if err := json.Unmarshal(out1, &summary1); err != nil {
		t.Fatalf("parse run 1 output: %v\nraw: %s", err, out1)
	}
	if summary1.Turns != 1 || summary1.Emitted != 1 || summary1.Quarantined != 0 {
		t.Errorf("run 1 summary unexpected: %+v", summary1)
	}

	// Run 2: same file, same dir → 1 turn, 0 emitted (idempotent).
	out2, err := exec.Command(binPath, "ingest-hermes",
		"--file", fixture, "--dir", tmpDir).Output()
	if err != nil {
		t.Fatalf("ingest-hermes run 2: %v", err)
	}
	var summary2 struct {
		Turns   int `json:"turns"`
		Emitted int `json:"emitted"`
	}
	if err := json.Unmarshal(out2, &summary2); err != nil {
		t.Fatalf("parse run 2 output: %v\nraw: %s", err, out2)
	}
	if summary2.Turns != 1 {
		t.Errorf("run 2 turns: got %d want 1", summary2.Turns)
	}
	if summary2.Emitted != 0 {
		t.Errorf("run 2 emitted: got %d want 0 (idempotency)", summary2.Emitted)
	}
}
```

- [ ] **Step 12.3: Run the integration test**

```bash
cd go/execution-kernel && go test ./internal/ingest/ -run TestIngestHermes_E2E -v
```

Expected: PASS.

- [ ] **Step 12.4: Run the full test suite**

```bash
cd go/execution-kernel && go test ./... -v
```

Expected: all tests pass — no regressions in openclaw, event, chain, etc.

- [ ] **Step 12.5: Commit**

```bash
rtk git add go/execution-kernel/internal/ingest/hermes_integration_test.go
rtk git commit -m "ingest/hermes: integration test — E2E subprocess + idempotency"
```

---

## Task 13: End-to-end verification + PR

**Files:** None (release task).

- [ ] **Step 13.1: Full manual end-to-end run**

Use the real capture that Phase B committed. Also use a live hermes run to prove the whole pipeline:

```bash
# Verify we still have today's events:
cat ~/.hermes/chitin-sink/events-$(date -u +%Y-%m-%d).jsonl | wc -l

# Run the kernel against today's live hermes output:
cd /home/red/workspace/chitin
mkdir -p /tmp/chitin-hermes-e2e
go run ./go/execution-kernel/cmd/chitin-kernel ingest-hermes \
  --file ~/.hermes/chitin-sink/events-$(date -u +%Y-%m-%d).jsonl \
  --dir /tmp/chitin-hermes-e2e
```

Expected: `turns` = count of post_api_request lines in the file. `quarantined` = count of non-primary event types + any malformed lines. Zero lines lost.

- [ ] **Step 13.2: Inspect the chain JSONL**

```bash
ls /tmp/chitin-hermes-e2e/
# Find the event chain JSONL file and check one emitted ModelTurn:
find /tmp/chitin-hermes-e2e -name "*.jsonl" -print0 | xargs -0 head -1 | python3 -m json.tool
```

Expected: `{"schema_version": "2", "event_type": "model_turn", "chain_id": "hermes:<32hex>:<16hex>", "labels": {"source": "hermes", "dialect": "hermes"}, ...}`.

- [ ] **Step 13.3: Push and open PR**

```bash
rtk git push -u origin hermes-dialect-adapter-v1
rtk gh pr create --title "hermes dialect adapter v1 — post_api_request → ModelTurn" --body "$(cat <<'EOF'
## Summary
- Installs a ~50-line hermes plugin (`~/.hermes/plugins/chitin-sink/`) that dumps LLM-call events as daily-rotated JSONL — second consumer of the OTEL GenAI ingest workstream after openclaw.
- Adds `go/execution-kernel/internal/ingest/hermes.go` translator: parses `post_api_request` → `ModelTurn`, quarantines other event types with `Reason="v1-scope"` for future expansion without hermes-side redeploy.
- Adds `chitin-kernel ingest-hermes` subcommand matching the existing `ingest-otel`/`ingest-transcript` shape.

## Test plan
- [ ] `cd go/execution-kernel && go test ./...` — full suite passes, no openclaw regressions.
- [ ] Manual E2E: run `chitin-kernel ingest-hermes --file ~/.hermes/chitin-sink/events-YYYY-MM-DD.jsonl --dir /tmp/…` — turns emitted, re-run shows 0 new emissions (idempotency).
- [ ] Verify emitted `model_turn` event has `labels.source="hermes"`, `chain_id="hermes:<trace>:<span>"`.

## Spec + plan
- Spec: `docs/superpowers/specs/2026-04-21-hermes-dialect-adapter-v1-design.md`
- Plan: `docs/superpowers/plans/2026-04-21-hermes-dialect-adapter-v1.md`
- Probe origin: `docs/superpowers/specs/2026-04-20-hermes-probe-design.md` (this is the verdict=yes follow-up)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 13.4: Run the review cycle**

Per `memory/project_review_process.md` and `memory/feedback_review_cycle_is_part_of_ship.md`:

1. Wait for Copilot review; address findings inline.
2. Run `/review` (adversarial review skill).
3. Apply patches.
4. Merge only on all-green (no open review threads, all checks pass).

Do **not** count the PR as done at create-time. Review cycle is part of ship.

- [ ] **Step 13.5: Post-merge — update observation doc**

After merge, add one line to `docs/observations/2026-04-21-hermes-post-api-request-capture.md` noting "merged into main as PR #<N>, `<commit-hash>`". Commit directly to main (doc-only change).

---

## Self-review

### Spec coverage

| Spec section | Task |
|---|---|
| § In scope — Hermes plugin (5 hooks, kwargs dump, daily JSONL) | Tasks 1–4 |
| § In scope — JSONL file layout | Task 2 (`_today_filename`, `_append`) |
| § In scope — Chitin translator (`ParseHermesEvents`, quarantine) | Tasks 7–10 |
| § In scope — Chain-id scheme (`"hermes:" + hex(trace) + ":" + hex(span)`) | Task 7 (`buildHermesChainID`, synthetic ID helpers) |
| § In scope — CLI subcommand `ingest-hermes` | Task 11 |
| § In scope — Fixture-driven unit tests | Tasks 8, 9, 10 (6 fixture files created across these tasks) |
| § In scope — Integration test mirroring `openclaw_integration_test.go` | Task 12 |
| § Data flow — real capture artifact | Task 6 |
| § Error handling — parse_error, v1-scope, missing_fields, usage=null-kept | Tasks 8 (parse_error, v1-scope), 9 (missing_fields, usage=null) |
| § Testing — manual verification | Task 13 Step 1 |
| § Execution handoff — 10-step breakdown | Expanded across Tasks 1–13 |

### Placeholder scan

- No `TBD`, `TODO`, or `fill-in` literals. `<version>`, `<N>`, `<commit-hash>` inside manual-verification steps are "write this value when you have it" — acceptable for runbook-style manual tasks where the value is determined at execution time.
- One caveat in Task 11 Step 4: "if openclaw's ingest CLI already exposes a `ModelTurnToPayload` helper, re-use it; if not, extract it." This is a "look at the code and decide" instruction, not a placeholder — the executor has everything they need to make the call. If the helper doesn't exist, extract into a shared file; if it does, import.

### Type / name consistency

- `ParseHermesEvents` — used identically in Tasks 8, 9, 10, 11, 12.
- `HermesEvent` struct — defined Task 8, consumed by `translatePostAPIRequest` in Task 9.
- `ModelTurn`, `Quarantine` — existing types; never redefined.
- `buildHermesChainID(traceHex, spanHex string) string` — defined Task 7, consumed by `BuildHermesChainIDFromTurn` in Task 11.
- `hermesSyntheticTraceID(sessionID)` / `hermesSyntheticSpanID(sessionID, callCount)` — defined Task 7, consumed by `translatePostAPIRequest` in Task 9.
- `getKwargString` / `getKwargInt` / `getKwargFloat` — all defined Task 9, only used inside `hermes.go`.
- `_append(event_type, kwargs)` — defined Task 2, consumed by `_make_handler` in Task 3.
- `_SCRUB_KEYS` / `_scrub` — defined Task 2, consumed inside `_append`.
- `register(ctx)` — hermes-plugin entry point, defined Task 3.

### Scope

One spec, one plan, one PR. Phase A (hermes plugin) lives outside the chitin repo and is operationally isolated; Phase C (chitin kernel) is a single PR on branch `hermes-dialect-adapter-v1`. No multi-subsystem bundling.
