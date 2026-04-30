# Analysis: Decisions Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the decisions analysis stream end-to-end so `python -m analysis.decisions --window 7d` reads `$HOME/.chitin/gov-decisions-*.jsonl`, ranks deny patterns, drafts candidate rules, and writes JSON + markdown for the 2026-05-07 talk demo.

**Architecture:** Layered Python package at `python/analysis/`. Layer 1 (deterministic detection + heuristic templates) is the demo path. Layer 2 (LLM-drafted rules) is opt-in. JSON canonical / markdown projection mirrors F4's chain-canonical / OTEL-projection shape. Foundation (loaders, types, writers) extracted so debt + soul-routing streams plug in post-talk as ~1-day additions.

**Tech Stack:** Python 3.11+, stdlib only for Layer 1, `pyyaml` for rule emission. Layer 2 uses ollama (qwen3-coder) via HTTP. No frameworks, no DB.

**Spec:** `docs/superpowers/specs/2026-04-30-analysis-decisions-design.md`.

---

## File Structure

```
python/analysis/
├── pyproject.toml              # Task 1
├── __init__.py                 # Task 1
├── types.py                    # Task 2
├── loaders.py                  # Task 3
├── detect.py                   # Task 4
├── draft.py                    # Task 5  (registry + dispatch)
├── templates/
│   ├── __init__.py             # Task 5  (registry impl)
│   ├── no_destructive_rm.py    # Task 6
│   ├── bounds_max_files_changed.py  # Task 7
│   ├── no_curl_pipe_bash.py    # Task 8
│   └── no_force_push.py        # Task 9
├── impact.py                   # Task 10  (predicted_impact computation)
├── writers.py                  # Tasks 11+12  (JSON, markdown)
├── decisions.py                # Task 13  (main entry / CLI)
├── debt.py                     # Task 14  (foundation-generalization stub)
├── souls.py                    # Task 14  (foundation-generalization stub)
├── llm_draft.py                # Task 15  (Layer 2, opt-in)
└── tests/
    ├── __init__.py
    ├── conftest.py             # Task 1
    ├── fixtures/
    │   └── gov-decisions-fixture.jsonl  # Task 2
    ├── test_types.py           # Task 2
    ├── test_loaders.py         # Task 3
    ├── test_detect.py          # Task 4
    ├── test_draft_registry.py  # Task 5
    ├── test_template_no_destructive_rm.py  # Task 6
    ├── test_template_bounds_max_files.py   # Task 7
    ├── test_template_no_curl_pipe_bash.py  # Task 8
    ├── test_template_no_force_push.py      # Task 9
    ├── test_impact.py          # Task 10
    ├── test_writers_json.py    # Task 11
    ├── test_writers_markdown.py  # Task 12
    ├── test_cli.py             # Task 13
    ├── test_stubs.py           # Task 14
    ├── test_llm_draft.py       # Task 15
    └── test_e2e.py             # Task 16
```

Output goes to `python/analysis/out/` (gitignored).

---

## Task 1: Scaffold the package

**Files:**
- Create: `python/analysis/pyproject.toml`
- Create: `python/analysis/__init__.py`
- Create: `python/analysis/tests/__init__.py`
- Create: `python/analysis/tests/conftest.py`
- Modify: `.gitignore` (add `python/analysis/out/`)

- [ ] **Step 1: Create the package directory and pyproject.toml**

```toml
# python/analysis/pyproject.toml
[project]
name = "chitin-analysis"
version = "0.1.0"
description = "Analysis layer for chitin gov-decisions and event chains"
requires-python = ">=3.11"
dependencies = [
    "pyyaml>=6.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0",
]

[build-system]
requires = ["setuptools>=64"]
build-backend = "setuptools.build_meta"

[tool.setuptools.packages.find]
where = ["."]
include = ["analysis*"]

[tool.pytest.ini_options]
testpaths = ["tests"]
python_files = ["test_*.py"]
```

- [ ] **Step 2: Create empty package init**

```python
# python/analysis/__init__.py
"""chitin analysis layer — decisions, debt, soul-routing streams."""
__version__ = "0.1.0"
```

- [ ] **Step 3: Create test conftest**

```python
# python/analysis/tests/conftest.py
"""Shared pytest fixtures for analysis tests."""
from pathlib import Path

import pytest


@pytest.fixture
def fixtures_dir() -> Path:
    """Path to the test fixtures directory."""
    return Path(__file__).parent / "fixtures"
```

- [ ] **Step 4: Add gitignore entry**

Modify `.gitignore` — add at end:

```
# Analysis stream outputs
python/analysis/out/
```

- [ ] **Step 5: Verify package installs**

Run: `cd python/analysis && pip install -e ".[dev]"`
Expected: install succeeds, `pytest --collect-only` reports 0 tests collected.

- [ ] **Step 6: Commit**

```bash
git add python/analysis/pyproject.toml python/analysis/__init__.py python/analysis/tests/__init__.py python/analysis/tests/conftest.py .gitignore
git commit -m "feat(analysis): scaffold python/analysis/ package"
```

---

## Task 2: Decision dataclass + line parser

**Files:**
- Create: `python/analysis/types.py`
- Create: `python/analysis/tests/fixtures/gov-decisions-fixture.jsonl`
- Create: `python/analysis/tests/test_types.py`

The Decision dataclass mirrors the gov-decisions JSONL schema observed in `$HOME/.chitin/gov-decisions-*.jsonl`. Sample line:
```json
{"ts":"2026-04-29T10:15:32Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","reason":"...","escalation":false,"agent":"copilot-cli","action_type":"shell.exec","action_target":"rm -rf /tmp/foo","envelope_id":"env_abc123","tier":"T0","cost_usd":0.0,"input_bytes":42,"tool_calls":1}
```

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_types.py
"""Tests for Decision dataclass + parsing."""
from datetime import datetime, timezone

from analysis.types import Decision, parse_decision_line


def test_parse_full_decision_line():
    line = (
        '{"ts":"2026-04-29T10:15:32Z","allowed":false,"mode":"enforce",'
        '"rule_id":"no-destructive-rm","reason":"matched destructive pattern",'
        '"escalation":false,"agent":"copilot-cli","action_type":"shell.exec",'
        '"action_target":"rm -rf /tmp/foo","envelope_id":"env_abc123",'
        '"tier":"T0","cost_usd":0.0,"input_bytes":42,"tool_calls":1}'
    )
    d = parse_decision_line(line)
    assert d is not None
    assert d.ts == datetime(2026, 4, 29, 10, 15, 32, tzinfo=timezone.utc)
    assert d.allowed is False
    assert d.rule_id == "no-destructive-rm"
    assert d.agent == "copilot-cli"
    assert d.action_type == "shell.exec"
    assert d.action_target == "rm -rf /tmp/foo"
    assert d.envelope_id == "env_abc123"


def test_parse_decision_with_missing_optional_fields():
    line = '{"ts":"2026-04-29T10:15:32Z","allowed":true,"mode":"enforce"}'
    d = parse_decision_line(line)
    assert d is not None
    assert d.allowed is True
    assert d.rule_id is None
    assert d.agent is None
    assert d.action_type is None


def test_parse_malformed_json_returns_none():
    assert parse_decision_line("not valid json") is None
    assert parse_decision_line("") is None
    assert parse_decision_line("{") is None


def test_parse_missing_ts_returns_none():
    line = '{"allowed":false,"rule_id":"x"}'
    assert parse_decision_line(line) is None


def test_parse_malformed_ts_returns_none():
    line = '{"ts":"not-a-timestamp","allowed":false}'
    assert parse_decision_line(line) is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_types.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.types'`

- [ ] **Step 3: Implement Decision + parser**

```python
# python/analysis/types.py
"""Core types for the analysis layer."""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional


@dataclass(frozen=True)
class Decision:
    """One row from gov-decisions-*.jsonl."""

    ts: datetime
    allowed: bool
    mode: Optional[str] = None
    rule_id: Optional[str] = None
    reason: Optional[str] = None
    escalation: bool = False
    agent: Optional[str] = None
    action_type: Optional[str] = None
    action_target: Optional[str] = None
    envelope_id: Optional[str] = None
    tier: Optional[str] = None
    cost_usd: float = 0.0
    input_bytes: int = 0
    tool_calls: int = 0


def parse_decision_line(line: str) -> Optional[Decision]:
    """Parse a single JSONL line into a Decision. Returns None on any error.

    Bad input never raises — analysis tolerates audit-log corruption.
    """
    line = line.strip()
    if not line:
        return None
    try:
        raw = json.loads(line)
    except json.JSONDecodeError:
        return None
    if not isinstance(raw, dict):
        return None
    ts_str = raw.get("ts")
    if not isinstance(ts_str, str):
        return None
    try:
        ts = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
    except (ValueError, TypeError):
        return None
    return Decision(
        ts=ts,
        allowed=bool(raw.get("allowed", False)),
        mode=raw.get("mode"),
        rule_id=raw.get("rule_id"),
        reason=raw.get("reason"),
        escalation=bool(raw.get("escalation", False)),
        agent=raw.get("agent"),
        action_type=raw.get("action_type"),
        action_target=raw.get("action_target"),
        envelope_id=raw.get("envelope_id"),
        tier=raw.get("tier"),
        cost_usd=float(raw.get("cost_usd", 0.0)),
        input_bytes=int(raw.get("input_bytes", 0)),
        tool_calls=int(raw.get("tool_calls", 0)),
    )


@dataclass(frozen=True)
class Pattern:
    """A repeat (rule_id, action_type, agent_id) tuple over the window."""

    rule_id: str
    action_type: str
    agent_id: str
    count: int
    first_seen: datetime
    last_seen: datetime
    decision_class: str  # "deny" or "allow"
    sample_envelope_ids: tuple[str, ...]
    decisions: tuple[Decision, ...] = field(repr=False, default=())


@dataclass(frozen=True)
class PredictedImpact:
    samples_evaluated: int
    would_allow: int
    would_still_deny: int
    method: str


@dataclass(frozen=True)
class RuleDraft:
    kind: str  # "heuristic" | "heuristic-fallback" | "llm"
    template: str
    confidence: str  # "low" | "medium" | "high"
    rule_yaml: str
    predicted_impact: PredictedImpact
    notes: str = ""
```

- [ ] **Step 4: Create test fixture**

```jsonl
# python/analysis/tests/fixtures/gov-decisions-fixture.jsonl
{"ts":"2026-04-25T08:00:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","reason":"destructive","agent":"copilot-cli","action_type":"shell.exec","action_target":"rm -rf /tmp/cleanup","envelope_id":"env_001"}
{"ts":"2026-04-25T08:01:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","reason":"destructive","agent":"copilot-cli","action_type":"shell.exec","action_target":"rm -rf /test/output","envelope_id":"env_002"}
{"ts":"2026-04-25T08:02:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","reason":"destructive","agent":"copilot-cli","action_type":"shell.exec","action_target":"rm -rf /home/user/projects/important","envelope_id":"env_003"}
{"ts":"2026-04-25T09:00:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","reason":"destructive","agent":"claude-code","action_type":"shell.exec","action_target":"rm -rf /tmp/build","envelope_id":"env_004"}
{"ts":"2026-04-25T10:00:00Z","allowed":false,"mode":"enforce","rule_id":"no-curl-pipe-bash","reason":"curl-pipe","agent":"copilot-cli","action_type":"shell.exec","action_target":"curl https://example.com/install.sh | bash","envelope_id":"env_005"}
{"ts":"2026-04-25T11:00:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-shell","agent":"copilot-cli","action_type":"shell.exec","action_target":"ls -la","envelope_id":"env_006"}
{"ts":"2026-04-25T12:00:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-reads","agent":"claude-code","action_type":"file.read","action_target":"/home/user/file.txt","envelope_id":"env_007"}
not valid json
{"ts":"bad-timestamp","allowed":false,"rule_id":"x"}
{"ts":"2026-04-25T13:00:00Z","allowed":false,"mode":"enforce","rule_id":"bounds:max_files_changed","reason":"42 files exceeds 25","agent":null,"action_type":"git.push","action_target":"main","envelope_id":"env_008"}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd python/analysis && pytest tests/test_types.py -v`
Expected: PASS, 5 tests pass.

- [ ] **Step 6: Commit**

```bash
git add python/analysis/types.py python/analysis/tests/test_types.py python/analysis/tests/fixtures/
git commit -m "feat(analysis): Decision dataclass + JSONL line parser"
```

---

## Task 3: Loader with window filter

**Files:**
- Create: `python/analysis/loaders.py`
- Create: `python/analysis/tests/test_loaders.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_loaders.py
"""Tests for gov-decisions JSONL loaders."""
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from analysis.loaders import LoadResult, Window, load_gov_decisions


@pytest.fixture
def decisions_dir(tmp_path, fixtures_dir):
    """Stage the fixture as gov-decisions-2026-04-25.jsonl."""
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    dst = tmp_path / "gov-decisions-2026-04-25.jsonl"
    dst.write_text(src.read_text())
    return tmp_path


def test_load_with_full_window(decisions_dir):
    window = Window(
        since=datetime(2026, 4, 25, 0, 0, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, 0, 0, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(decisions_dir, window)
    assert isinstance(result, LoadResult)
    assert result.files_read == 1
    assert result.parse_errors == 2  # one bad json line, one bad ts
    assert len(result.decisions) == 8


def test_load_with_narrow_window_excludes_outside(decisions_dir):
    window = Window(
        since=datetime(2026, 4, 25, 9, 0, tzinfo=timezone.utc),
        until=datetime(2026, 4, 25, 11, 0, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(decisions_dir, window)
    # ts >= since AND ts < until → 09:00 (in), 10:00 (in), 11:00 (out)
    envelope_ids = {d.envelope_id for d in result.decisions}
    assert envelope_ids == {"env_004", "env_005"}


def test_load_with_empty_dir(tmp_path):
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path, window)
    assert result.files_read == 0
    assert result.decisions == []
    assert result.parse_errors == 0


def test_load_skips_non_matching_filenames(tmp_path, fixtures_dir):
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    (tmp_path / "gov-decisions-2026-04-25.jsonl").write_text(src.read_text())
    (tmp_path / "other-file.jsonl").write_text("ignored\n")
    (tmp_path / "gov-decisions.txt").write_text("ignored\n")
    window = Window(
        since=datetime(2026, 4, 25, tzinfo=timezone.utc),
        until=datetime(2026, 4, 26, tzinfo=timezone.utc),
    )
    result = load_gov_decisions(tmp_path, window)
    assert result.files_read == 1
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_loaders.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.loaders'`

- [ ] **Step 3: Implement loader**

```python
# python/analysis/loaders.py
"""Load gov-decisions JSONL files with window filtering."""
from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from analysis.types import Decision, parse_decision_line

GOV_DECISIONS_PATTERN = re.compile(r"^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$")


@dataclass(frozen=True)
class Window:
    """Half-open time window: ts >= since AND ts < until."""

    since: datetime
    until: datetime


@dataclass(frozen=True)
class LoadResult:
    decisions: list[Decision]
    files_read: int
    parse_errors: int


def load_gov_decisions(decisions_dir: Path, window: Window) -> LoadResult:
    """Load all gov-decisions-*.jsonl files in dir, filter to window.

    Bad lines are counted in parse_errors and skipped — never raise.
    """
    decisions_dir = Path(decisions_dir)
    if not decisions_dir.exists():
        return LoadResult(decisions=[], files_read=0, parse_errors=0)

    decisions: list[Decision] = []
    parse_errors = 0
    files_read = 0

    for path in sorted(decisions_dir.iterdir()):
        if not GOV_DECISIONS_PATTERN.match(path.name):
            continue
        files_read += 1
        with path.open("r") as f:
            for line in f:
                d = parse_decision_line(line)
                if d is None:
                    if line.strip():
                        parse_errors += 1
                    continue
                if window.since <= d.ts < window.until:
                    decisions.append(d)

    return LoadResult(
        decisions=decisions,
        files_read=files_read,
        parse_errors=parse_errors,
    )


def parse_window_str(s: str, now: datetime) -> Window:
    """Parse '7d' / '24h' / '60m' as a window ending at now."""
    if s.endswith("d"):
        days = int(s[:-1])
        from datetime import timedelta
        return Window(since=now - timedelta(days=days), until=now)
    if s.endswith("h"):
        hours = int(s[:-1])
        from datetime import timedelta
        return Window(since=now - timedelta(hours=hours), until=now)
    if s.endswith("m"):
        minutes = int(s[:-1])
        from datetime import timedelta
        return Window(since=now - timedelta(minutes=minutes), until=now)
    raise ValueError(f"Unrecognized window: {s!r}. Use Nd, Nh, or Nm.")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd python/analysis && pytest tests/test_loaders.py -v`
Expected: PASS, 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/loaders.py python/analysis/tests/test_loaders.py
git commit -m "feat(analysis): JSONL loader with half-open window filtering"
```

---

## Task 4: Pattern detection (group + rank + tie-break)

**Files:**
- Create: `python/analysis/detect.py`
- Create: `python/analysis/tests/test_detect.py`

- [ ] **Step 1: Write the failing tests**

```python
# python/analysis/tests/test_detect.py
"""Tests for pattern detection."""
from datetime import datetime, timezone

import pytest

from analysis.detect import detect_patterns
from analysis.types import Decision


def _decision(ts="2026-04-25T08:00:00Z", allowed=False, rule_id="r1",
              action_type="shell.exec", agent="copilot-cli", envelope_id="e1"):
    return Decision(
        ts=datetime.fromisoformat(ts.replace("Z", "+00:00")),
        allowed=allowed,
        rule_id=rule_id,
        action_type=action_type,
        agent=agent,
        envelope_id=envelope_id,
    )


def test_empty_input_returns_empty():
    assert detect_patterns([]) == []


def test_single_decision_one_pattern():
    patterns = detect_patterns([_decision()])
    assert len(patterns) == 1
    assert patterns[0].count == 1
    assert patterns[0].rule_id == "r1"


def test_all_allows_returns_empty():
    """v1 ranks denies only."""
    decisions = [_decision(allowed=True, envelope_id=f"e{i}") for i in range(5)]
    assert detect_patterns(decisions) == []


def test_three_same_pattern():
    decisions = [_decision(envelope_id=f"e{i}") for i in range(3)]
    patterns = detect_patterns(decisions)
    assert len(patterns) == 1
    assert patterns[0].count == 3


def test_count_descending_ranking():
    decisions = [
        # 3x rule_a
        _decision(rule_id="rule_a", envelope_id="e1"),
        _decision(rule_id="rule_a", envelope_id="e2"),
        _decision(rule_id="rule_a", envelope_id="e3"),
        # 5x rule_b
        *[_decision(rule_id="rule_b", envelope_id=f"eb{i}") for i in range(5)],
        # 1x rule_c
        _decision(rule_id="rule_c", envelope_id="ec1"),
    ]
    patterns = detect_patterns(decisions)
    counts = [p.count for p in patterns]
    assert counts == [5, 3, 1]
    rules = [p.rule_id for p in patterns]
    assert rules == ["rule_b", "rule_a", "rule_c"]


def test_tie_breaker_alphabetic_on_rule_id():
    decisions = [
        _decision(rule_id="zeta", envelope_id="e1"),
        _decision(rule_id="zeta", envelope_id="e2"),
        _decision(rule_id="alpha", envelope_id="ea1"),
        _decision(rule_id="alpha", envelope_id="ea2"),
    ]
    patterns = detect_patterns(decisions)
    # Both have count=2, alpha sorts before zeta
    assert [p.rule_id for p in patterns] == ["alpha", "zeta"]


def test_tie_breaker_secondary_on_action_type():
    decisions = [
        _decision(rule_id="r", action_type="zzz", envelope_id="e1"),
        _decision(rule_id="r", action_type="aaa", envelope_id="e2"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.action_type for p in patterns] == ["aaa", "zzz"]


def test_tie_breaker_tertiary_on_agent():
    decisions = [
        _decision(rule_id="r", action_type="t", agent="zzz", envelope_id="e1"),
        _decision(rule_id="r", action_type="t", agent="aaa", envelope_id="e2"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.agent_id for p in patterns] == ["aaa", "zzz"]


def test_null_rule_id_bucketed_as_none():
    decisions = [_decision(rule_id=None, envelope_id="e1")]
    patterns = detect_patterns(decisions)
    assert patterns[0].rule_id == "<none>"


def test_null_agent_bucketed_as_unknown():
    decisions = [_decision(agent=None, envelope_id="e1")]
    patterns = detect_patterns(decisions)
    assert patterns[0].agent_id == "<unknown>"


def test_first_last_seen_correct():
    decisions = [
        _decision(ts="2026-04-25T12:00:00Z", envelope_id="e1"),
        _decision(ts="2026-04-25T08:00:00Z", envelope_id="e2"),
        _decision(ts="2026-04-25T15:00:00Z", envelope_id="e3"),
    ]
    patterns = detect_patterns(decisions)
    assert patterns[0].first_seen == datetime(2026, 4, 25, 8, 0, tzinfo=timezone.utc)
    assert patterns[0].last_seen == datetime(2026, 4, 25, 15, 0, tzinfo=timezone.utc)


def test_sample_envelope_ids_are_first_three():
    decisions = [_decision(envelope_id=f"env_{i:03d}") for i in range(5)]
    patterns = detect_patterns(decisions)
    assert patterns[0].sample_envelope_ids == ("env_000", "env_001", "env_002")


def test_determinism_two_runs_byte_equal():
    decisions = [
        _decision(rule_id="b", envelope_id="e1"),
        _decision(rule_id="a", envelope_id="e2"),
        _decision(rule_id="b", envelope_id="e3"),
    ]
    a = detect_patterns(decisions)
    b = detect_patterns(decisions)
    assert a == b
    assert [(p.rule_id, p.count) for p in a] == [(p.rule_id, p.count) for p in b]
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_detect.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.detect'`

- [ ] **Step 3: Implement detect_patterns**

```python
# python/analysis/detect.py
"""Pattern detection: group decisions by (rule_id, action_type, agent), rank."""
from __future__ import annotations

from collections import defaultdict
from typing import Iterable

from analysis.types import Decision, Pattern


def detect_patterns(decisions: Iterable[Decision]) -> list[Pattern]:
    """Group decisions by (rule_id, action_type, agent_id) and rank by count desc.

    v1 invariants:
      - Only denies are ranked (allowed=False).
      - Tie-breaker: alphabetic on rule_id → action_type → agent_id (stable).
      - Null/missing fields bucket as '<none>' / '<unknown>'.
    """
    buckets: dict[tuple[str, str, str], list[Decision]] = defaultdict(list)
    for d in decisions:
        if d.allowed:
            continue
        key = (
            d.rule_id or "<none>",
            d.action_type or "<none>",
            d.agent or "<unknown>",
        )
        buckets[key].append(d)

    keys_sorted = sorted(
        buckets.keys(),
        key=lambda k: (-len(buckets[k]), k[0], k[1], k[2]),
    )

    patterns: list[Pattern] = []
    for key in keys_sorted:
        bucket = buckets[key]
        bucket_sorted = sorted(bucket, key=lambda d: (d.ts, d.envelope_id or ""))
        first_seen = bucket_sorted[0].ts
        last_seen = bucket_sorted[-1].ts
        sample_ids = tuple(
            d.envelope_id for d in bucket_sorted[:3] if d.envelope_id
        )
        patterns.append(Pattern(
            rule_id=key[0],
            action_type=key[1],
            agent_id=key[2],
            count=len(bucket),
            first_seen=first_seen,
            last_seen=last_seen,
            decision_class="deny",
            sample_envelope_ids=sample_ids,
            decisions=tuple(bucket_sorted),
        ))
    return patterns
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_detect.py -v`
Expected: PASS, 12 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/detect.py python/analysis/tests/test_detect.py
git commit -m "feat(analysis): pattern detection with stable tie-breaker"
```

---

## Task 5: Template registry + dispatch

**Files:**
- Create: `python/analysis/templates/__init__.py`
- Create: `python/analysis/draft.py`
- Create: `python/analysis/tests/test_draft_registry.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_draft_registry.py
"""Tests for the template registry and draft dispatch."""
from datetime import datetime, timezone

from analysis.draft import draft_for_pattern
from analysis.types import Pattern


def _pattern(rule_id="unknown_rule", count=5):
    return Pattern(
        rule_id=rule_id,
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=count,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=("e1", "e2", "e3"),
        decisions=(),
    )


def test_unknown_rule_id_returns_none():
    assert draft_for_pattern(_pattern(rule_id="totally_unknown")) is None


def test_registry_lookup_by_rule_id():
    """Templates are registered keyed on rule_id."""
    from analysis.templates import REGISTRY
    assert isinstance(REGISTRY, dict)
    # At minimum the registry exists; specific entries added per task.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_draft_registry.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.draft'`

- [ ] **Step 3: Implement registry + dispatch**

```python
# python/analysis/templates/__init__.py
"""Template registry. Each entry maps a rule_id to a draft function.

A template function takes a Pattern and returns a RuleDraft, or None if the
template can't draft for this specific pattern.
"""
from __future__ import annotations

from typing import Callable, Optional

from analysis.types import Pattern, RuleDraft

TemplateFunc = Callable[[Pattern], Optional[RuleDraft]]

REGISTRY: dict[str, TemplateFunc] = {}


def register(rule_id: str, fn: TemplateFunc) -> None:
    """Register a template function for a rule_id."""
    REGISTRY[rule_id] = fn
```

```python
# python/analysis/draft.py
"""Dispatch a Pattern to its template, returning a RuleDraft or None."""
from __future__ import annotations

from typing import Optional

from analysis.templates import REGISTRY
from analysis.types import Pattern, RuleDraft


def draft_for_pattern(pattern: Pattern) -> Optional[RuleDraft]:
    """Look up a template by rule_id and produce a draft.

    Returns None if no template is registered for this rule_id, OR if the
    template returns None (e.g., the pattern doesn't match the template's
    heuristic).
    """
    fn = REGISTRY.get(pattern.rule_id)
    if fn is None:
        return None
    return fn(pattern)


def reason_no_template(pattern: Pattern) -> str:
    """Human-readable explanation for why no draft was generated."""
    if pattern.rule_id not in REGISTRY:
        return f"no template registered for rule_id={pattern.rule_id!r}"
    return f"template for {pattern.rule_id!r} declined this pattern"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_draft_registry.py -v`
Expected: PASS, 2 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/__init__.py python/analysis/draft.py python/analysis/tests/test_draft_registry.py
git commit -m "feat(analysis): template registry + draft dispatch"
```

---

## Task 6: Template — `no-destructive-rm`

The headline pattern. 30 denies in real data. Template proposes an exemption for known-safe directories (`/tmp/`, `/test/`, `out/`, `graphify-out/`).

**Files:**
- Create: `python/analysis/templates/no_destructive_rm.py`
- Create: `python/analysis/tests/test_template_no_destructive_rm.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_template_no_destructive_rm.py
"""Tests for the no-destructive-rm heuristic template."""
from datetime import datetime, timezone

from analysis.templates.no_destructive_rm import draft
from analysis.types import Decision, Pattern


def _pattern_with_targets(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-destructive-rm",
            action_type="shell.exec",
            action_target=t,
            agent="copilot-cli",
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-destructive-rm",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_safe_dirs_are_drafted_as_allow_exception():
    p = _pattern_with_targets(
        "rm -rf /tmp/cleanup",
        "rm -rf /test/output",
        "rm -rf out/build",
        "rm -rf graphify-out/wiki",
    )
    d = draft(p)
    assert d is not None
    assert d.kind == "heuristic"
    assert d.template == "no_destructive_rm"
    assert "no-destructive-rm-safe-dirs" in d.rule_yaml
    assert d.predicted_impact.samples_evaluated == 4
    assert d.predicted_impact.would_allow == 4
    assert d.predicted_impact.would_still_deny == 0


def test_mixed_targets_split_correctly():
    p = _pattern_with_targets(
        "rm -rf /tmp/cleanup",         # safe
        "rm -rf /etc/passwd",           # NOT safe
        "rm -rf /home/user/important",  # NOT safe
    )
    d = draft(p)
    assert d is not None
    assert d.predicted_impact.would_allow == 1
    assert d.predicted_impact.would_still_deny == 2


def test_pattern_with_no_safe_targets_returns_none():
    p = _pattern_with_targets(
        "rm -rf /etc/passwd",
        "rm -rf /home/user/important",
    )
    d = draft(p)
    assert d is None  # No exception worth proposing.


def test_pattern_with_no_decisions_returns_none():
    p = Pattern(
        rule_id="no-destructive-rm",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_template_no_destructive_rm.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement template**

```python
# python/analysis/templates/no_destructive_rm.py
"""Template for the `no-destructive-rm` rule.

Proposes an exemption for known-safe directories. Tested against real data
(19 denies for copilot-cli, 8 unknown agent, 3 claude-code in week 2026-04-23).
"""
from __future__ import annotations

import re
from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

# Patterns considered safe to exempt. Conservative — extend only with evidence.
SAFE_PATTERNS = [
    re.compile(r"\brm\s+-rf?\s+(?:[^/]*?/)?(?:tmp|test|out|graphify-out|build|dist|node_modules)(?:/|$)"),
    re.compile(r"\brm\s+-rf?\s+/tmp/"),
    re.compile(r"\brm\s+-rf?\s+\.\./?(?:tmp|test|out)/"),
]


def _matches_safe(target: str) -> bool:
    if not target:
        return False
    return any(p.search(target) for p in SAFE_PATTERNS)


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    safe_count = sum(1 for d in pattern.decisions if _matches_safe(d.action_target or ""))
    if safe_count == 0:
        return None  # No exemption worth proposing.

    rule_yaml = (
        "rules:\n"
        "  - id: no-destructive-rm-safe-dirs\n"
        "    when:\n"
        "      action_type: shell.exec\n"
        "      action_target_regex: '\\brm\\s+-rf?\\s+(?:[^/]*?/)?(?:tmp|test|out|graphify-out|build|dist|node_modules)(?:/|$)'\n"
        "    decide: allow\n"
        "    reason: 'cleanup of known-temp dirs (analysis-suggested 2026-04-30)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="regex-match-on-action_target",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_destructive_rm",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Proposes exemption for cleanup in /tmp, /test, out/, graphify-out/, build/, dist/, node_modules/.",
    )


register("no-destructive-rm", draft)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_template_no_destructive_rm.py -v`
Expected: PASS, 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/no_destructive_rm.py python/analysis/tests/test_template_no_destructive_rm.py
git commit -m "feat(analysis): no-destructive-rm template with safe-dirs exemption"
```

---

## Task 7: Template — `bounds:max_files_changed`

Doc-batch detection. If all changed paths share `docs/` prefix, propose context-aware ceiling. (This is literally Issue #70.)

**Files:**
- Create: `python/analysis/templates/bounds_max_files_changed.py`
- Create: `python/analysis/tests/test_template_bounds_max_files.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_template_bounds_max_files.py
"""Tests for the bounds:max_files_changed heuristic template."""
from datetime import datetime, timezone

from analysis.templates.bounds_max_files_changed import draft
from analysis.types import Decision, Pattern


def _pattern_with_reason(*reasons):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="bounds:max_files_changed",
            action_type="git.push",
            reason=r,
            envelope_id=f"e{i}",
        )
        for i, r in enumerate(reasons)
    )
    return Pattern(
        rule_id="bounds:max_files_changed",
        action_type="git.push",
        agent_id="<unknown>",
        count=len(reasons),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_doc_batch_drafted():
    p = _pattern_with_reason(
        "41 files changed exceeds ceiling of 25 (all under docs/)",
        "30 files changed exceeds ceiling of 25 (all under docs/)",
    )
    d = draft(p)
    assert d is not None
    assert d.template == "bounds_max_files_changed"
    assert "doc-batch" in d.rule_yaml
    assert d.predicted_impact.would_allow == 2


def test_no_doc_signal_returns_none():
    p = _pattern_with_reason(
        "60 files changed exceeds ceiling of 25",
        "100 files changed exceeds ceiling of 25",
    )
    d = draft(p)
    assert d is None


def test_empty_pattern_returns_none():
    p = Pattern(
        rule_id="bounds:max_files_changed",
        action_type="git.push",
        agent_id="<unknown>",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_template_bounds_max_files.py -v`
Expected: FAIL.

- [ ] **Step 3: Implement template**

```python
# python/analysis/templates/bounds_max_files_changed.py
"""Template for `bounds:max_files_changed`.

The kernel-level bounds rule fires for any push exceeding the ceiling. The
common false-positive is doc batches (per Issue #70). Heuristic: if the
deny reason mentions docs/, propose a context-aware ceiling.
"""
from __future__ import annotations

from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

DOC_KEYWORDS = ("docs/", "wiki/", "README", "graphify-out/")


def _is_doc_batch(reason: str) -> bool:
    if not reason:
        return False
    return any(k in reason for k in DOC_KEYWORDS)


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    doc_count = sum(1 for d in pattern.decisions if _is_doc_batch(d.reason or ""))
    if doc_count == 0:
        return None

    rule_yaml = (
        "rules:\n"
        "  - id: bounds-max-files-doc-batch\n"
        "    when:\n"
        "      action_type: git.push\n"
        "      changed_paths_all_match: '^(docs/|wiki/|graphify-out/)'\n"
        "    bounds:\n"
        "      max_files_changed: 200\n"
        "      max_lines_changed: 10000\n"
        "    reason: 'doc-batch ceiling override (analysis-suggested 2026-04-30)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=doc_count,
        would_still_deny=pattern.count - doc_count,
        method="reason-mentions-doc-keyword",
    )
    return RuleDraft(
        kind="heuristic",
        template="bounds_max_files_changed",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Doc-batch detected via reason text; tighten with changed_paths inspection in v2.",
    )


register("bounds:max_files_changed", draft)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_template_bounds_max_files.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/bounds_max_files_changed.py python/analysis/tests/test_template_bounds_max_files.py
git commit -m "feat(analysis): bounds:max_files_changed template with doc-batch detection"
```

---

## Task 8: Template — `no-curl-pipe-bash`

Extract URL host from action_target. Propose host-allowlist exception for known-trusted hosts.

**Files:**
- Create: `python/analysis/templates/no_curl_pipe_bash.py`
- Create: `python/analysis/tests/test_template_no_curl_pipe_bash.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_template_no_curl_pipe_bash.py
"""Tests for the no-curl-pipe-bash heuristic template."""
from datetime import datetime, timezone

from analysis.templates.no_curl_pipe_bash import draft, extract_host
from analysis.types import Decision, Pattern


def _pattern(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-curl-pipe-bash",
            action_type="shell.exec",
            action_target=t,
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-curl-pipe-bash",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_extract_host_basic():
    assert extract_host("curl https://example.com/x.sh | bash") == "example.com"
    assert extract_host("curl http://foo.bar.baz/a | sh") == "foo.bar.baz"
    assert extract_host("curl -L https://get.deno.land/install.sh | sh") == "get.deno.land"


def test_extract_host_returns_none_for_no_url():
    assert extract_host("") is None
    assert extract_host("rm -rf /") is None
    assert extract_host("curl | bash") is None


def test_trusted_hosts_drafted():
    p = _pattern(
        "curl https://get.deno.land/install.sh | sh",
        "curl https://sh.rustup.rs | sh",
    )
    d = draft(p)
    assert d is not None
    assert "trusted-curl-hosts" in d.rule_yaml
    assert d.predicted_impact.would_allow == 2


def test_unknown_host_returns_none():
    p = _pattern("curl https://random-blog.example/script.sh | bash")
    assert draft(p) is None


def test_empty_pattern_returns_none():
    p = Pattern(
        rule_id="no-curl-pipe-bash",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_template_no_curl_pipe_bash.py -v`
Expected: FAIL.

- [ ] **Step 3: Implement template**

```python
# python/analysis/templates/no_curl_pipe_bash.py
"""Template for `no-curl-pipe-bash`.

Heuristic: extract URL host from the curl command. If the host is on a
known-trusted list (rustup, deno, ...), propose host-allowlist exemption.
"""
from __future__ import annotations

import re
from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

# Hosts known to publish official installer scripts. Conservative — only add
# with evidence (e.g., reviewer-blessed before merge).
TRUSTED_HOSTS = frozenset({
    "get.deno.land",
    "sh.rustup.rs",
    "install.python-poetry.org",
    "raw.githubusercontent.com",  # GitHub-hosted; relies on path-based judgment
})

URL_RE = re.compile(r"https?://([^/\s|]+)")


def extract_host(target: str) -> Optional[str]:
    if not target:
        return None
    m = URL_RE.search(target)
    return m.group(1) if m else None


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    hosts = [extract_host(d.action_target or "") for d in pattern.decisions]
    trusted = sum(1 for h in hosts if h and h in TRUSTED_HOSTS)
    if trusted == 0:
        return None

    rule_yaml = (
        "rules:\n"
        "  - id: trusted-curl-hosts\n"
        "    when:\n"
        "      action_type: shell.exec\n"
        "      action_target_regex: 'curl[^|]*https?://(get\\.deno\\.land|sh\\.rustup\\.rs|install\\.python-poetry\\.org)/'\n"
        "    decide: allow\n"
        "    reason: 'official installer from trusted host (analysis-suggested 2026-04-30)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=trusted,
        would_still_deny=pattern.count - trusted,
        method="host-extracted-from-url-allowlist",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_curl_pipe_bash",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Trusted-host list is conservative; extend with reviewer approval.",
    )


register("no-curl-pipe-bash", draft)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_template_no_curl_pipe_bash.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/no_curl_pipe_bash.py python/analysis/tests/test_template_no_curl_pipe_bash.py
git commit -m "feat(analysis): no-curl-pipe-bash template with trusted-host allowlist"
```

---

## Task 9: Template — `no-force-push`

Branch detection. Allow on personal/feature branches, keep deny on main/master.

**Files:**
- Create: `python/analysis/templates/no_force_push.py`
- Create: `python/analysis/tests/test_template_no_force_push.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_template_no_force_push.py
"""Tests for the no-force-push heuristic template."""
from datetime import datetime, timezone

from analysis.templates.no_force_push import draft
from analysis.types import Decision, Pattern


def _pattern(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-force-push",
            action_type="git.force-push",
            action_target=t,
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-force-push",
        action_type="git.force-push",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_personal_branches_drafted():
    p = _pattern("feat/foo", "fix/bar", "spike/x")
    d = draft(p)
    assert d is not None
    assert "no-force-push-feature-branches" in d.rule_yaml
    assert d.predicted_impact.would_allow == 3


def test_main_still_denied():
    p = _pattern("main", "master")
    d = draft(p)
    assert d is None


def test_mixed():
    p = _pattern("feat/foo", "main", "fix/bar")
    d = draft(p)
    assert d is not None
    assert d.predicted_impact.would_allow == 2
    assert d.predicted_impact.would_still_deny == 1


def test_empty_pattern_returns_none():
    p = Pattern(
        rule_id="no-force-push",
        action_type="git.force-push",
        agent_id="copilot-cli",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_template_no_force_push.py -v`
Expected: FAIL.

- [ ] **Step 3: Implement template**

```python
# python/analysis/templates/no_force_push.py
"""Template for `no-force-push`.

Heuristic: keep deny on main/master, propose allow on feat/fix/spike branches.
"""
from __future__ import annotations

from typing import Optional

from analysis.templates import register
from analysis.types import Pattern, PredictedImpact, RuleDraft

PROTECTED_BRANCHES = frozenset({"main", "master", "production", "release"})
FEATURE_PREFIXES = ("feat/", "fix/", "spike/", "feature/", "bugfix/", "wip/", "draft/")


def _is_safe_branch(target: str) -> bool:
    if not target:
        return False
    branch = target.strip()
    if branch in PROTECTED_BRANCHES:
        return False
    return any(branch.startswith(p) for p in FEATURE_PREFIXES)


def draft(pattern: Pattern) -> Optional[RuleDraft]:
    if not pattern.decisions:
        return None

    safe_count = sum(1 for d in pattern.decisions if _is_safe_branch(d.action_target or ""))
    if safe_count == 0:
        return None

    rule_yaml = (
        "rules:\n"
        "  - id: no-force-push-feature-branches\n"
        "    when:\n"
        "      action_type: git.force-push\n"
        "      action_target_regex: '^(feat|fix|spike|feature|bugfix|wip|draft)/'\n"
        "    decide: allow\n"
        "    reason: 'force-push allowed on personal/feature branches (analysis-suggested 2026-04-30)'\n"
    )
    impact = PredictedImpact(
        samples_evaluated=pattern.count,
        would_allow=safe_count,
        would_still_deny=pattern.count - safe_count,
        method="branch-prefix-match",
    )
    return RuleDraft(
        kind="heuristic",
        template="no_force_push",
        confidence="medium",
        rule_yaml=rule_yaml,
        predicted_impact=impact,
        notes="Protected branches (main/master/production/release) keep the deny.",
    )


register("no-force-push", draft)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_template_no_force_push.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/no_force_push.py python/analysis/tests/test_template_no_force_push.py
git commit -m "feat(analysis): no-force-push template — feature branches allowed, main protected"
```

---

## Task 10: Template auto-import + integration test

Templates self-register via module import side-effects. Make sure they import.

**Files:**
- Modify: `python/analysis/templates/__init__.py`
- Create: `python/analysis/tests/test_templates_auto_register.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_templates_auto_register.py
"""Tests that all v1 templates self-register on import."""
from analysis.templates import REGISTRY
import analysis.templates.all  # noqa: F401  (triggers registration)


def test_all_v1_templates_registered():
    expected = {
        "no-destructive-rm",
        "bounds:max_files_changed",
        "no-curl-pipe-bash",
        "no-force-push",
    }
    assert expected.issubset(REGISTRY.keys())
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_templates_auto_register.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.templates.all'`

- [ ] **Step 3: Create the all-importer**

```python
# python/analysis/templates/all.py
"""Single import to load every v1 template (triggers registration).

Order is alphabetical for predictability; registration order does not matter
for correctness, only for which template wins on conflicting rule_id (none in v1).
"""
from analysis.templates import bounds_max_files_changed  # noqa: F401
from analysis.templates import no_curl_pipe_bash         # noqa: F401
from analysis.templates import no_destructive_rm         # noqa: F401
from analysis.templates import no_force_push             # noqa: F401
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd python/analysis && pytest tests/test_templates_auto_register.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/templates/all.py python/analysis/tests/test_templates_auto_register.py
git commit -m "feat(analysis): single-import all-templates loader for registration"
```

---

## Task 11: JSON writer with deterministic output

**Files:**
- Create: `python/analysis/writers.py`
- Create: `python/analysis/tests/test_writers_json.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_writers_json.py
"""Tests for JSON canonical writer."""
import json
from datetime import datetime, timezone

import pytest

from analysis.types import Pattern, PredictedImpact, RuleDraft
from analysis.writers import Finding, build_finding, write_json


def _pattern():
    return Pattern(
        rule_id="r1",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=3,
        first_seen=datetime(2026, 4, 25, 8, 0, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, 9, 0, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=("e1", "e2", "e3"),
        decisions=(),
    )


def _draft():
    return RuleDraft(
        kind="heuristic",
        template="t1",
        confidence="medium",
        rule_yaml="rules: []\n",
        predicted_impact=PredictedImpact(
            samples_evaluated=3, would_allow=2, would_still_deny=1, method="m"
        ),
        notes="n",
    )


def test_build_finding_pairs_pattern_with_draft():
    f = build_finding(_pattern(), _draft(), rank=1)
    assert f.rank == 1
    assert f.pattern.rule_id == "r1"
    assert f.draft is not None


def test_build_finding_with_no_draft():
    f = build_finding(_pattern(), None, rank=2)
    assert f.draft is None


def test_write_json_produces_deterministic_output(tmp_path):
    findings = [build_finding(_pattern(), _draft(), rank=1)]
    no_template = []
    out = tmp_path / "out.json"
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    window_since = datetime(2026, 4, 23, 12, 0, tzinfo=timezone.utc)

    write_json(out, findings=findings, no_template=no_template,
               input_summary={"total_decisions": 1225, "denies": 62, "allows": 1163,
                              "files_read": 6, "parse_errors": 0,
                              "distinct_rule_ids": 14},
               generated_at=now, window_since=window_since,
               window_until=now, window_days=7)

    a = out.read_bytes()

    out2 = tmp_path / "out2.json"
    write_json(out2, findings=findings, no_template=no_template,
               input_summary={"total_decisions": 1225, "denies": 62, "allows": 1163,
                              "files_read": 6, "parse_errors": 0,
                              "distinct_rule_ids": 14},
               generated_at=now, window_since=window_since,
               window_until=now, window_days=7)
    b = out2.read_bytes()

    assert a == b
    parsed = json.loads(a)
    assert parsed["schema_version"] == "1"
    assert parsed["stream"] == "decisions"
    assert parsed["window"]["days"] == 7
    assert parsed["input_summary"]["total_decisions"] == 1225
    assert len(parsed["patterns"]) == 1
    assert parsed["patterns"][0]["rank"] == 1
    assert parsed["patterns"][0]["draft"]["predicted_impact"]["would_allow"] == 2


def test_write_json_handles_empty_findings(tmp_path):
    out = tmp_path / "empty.json"
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    write_json(out, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=now, window_until=now, window_days=7)
    parsed = json.loads(out.read_bytes())
    assert parsed["patterns"] == []
    assert parsed["no_template_patterns"] == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_writers_json.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'analysis.writers'`

- [ ] **Step 3: Implement Finding + write_json**

```python
# python/analysis/writers.py
"""Output writers — JSON canonical and markdown projection.

JSON is the contract. Markdown is regenerable from JSON. Both deterministic
given identical inputs (and a fixed `generated_at`).
"""
from __future__ import annotations

import json
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Optional

from analysis.types import Pattern, RuleDraft


@dataclass(frozen=True)
class Finding:
    """A pattern paired with a (possibly None) rule draft, plus rank."""

    rank: int
    pattern: Pattern
    draft: Optional[RuleDraft]


def build_finding(pattern: Pattern, draft: Optional[RuleDraft], rank: int) -> Finding:
    return Finding(rank=rank, pattern=pattern, draft=draft)


def _pattern_to_json(p: Pattern) -> dict[str, Any]:
    return {
        "rule_id": p.rule_id,
        "action_type": p.action_type,
        "agent_id": p.agent_id,
        "count": p.count,
        "first_seen": p.first_seen.isoformat(),
        "last_seen": p.last_seen.isoformat(),
        "decision_class": p.decision_class,
        "sample_envelope_ids": list(p.sample_envelope_ids),
    }


def _draft_to_json(d: RuleDraft) -> dict[str, Any]:
    return {
        "kind": d.kind,
        "template": d.template,
        "confidence": d.confidence,
        "rule_yaml": d.rule_yaml,
        "predicted_impact": {
            "samples_evaluated": d.predicted_impact.samples_evaluated,
            "would_allow": d.predicted_impact.would_allow,
            "would_still_deny": d.predicted_impact.would_still_deny,
            "method": d.predicted_impact.method,
        },
        "notes": d.notes,
    }


def _finding_to_json(f: Finding) -> dict[str, Any]:
    obj = {"rank": f.rank, **_pattern_to_json(f.pattern)}
    obj["draft"] = _draft_to_json(f.draft) if f.draft else None
    return obj


def write_json(
    path: Path,
    *,
    findings: list[Finding],
    no_template: list[dict[str, Any]],
    input_summary: dict[str, Any],
    generated_at: datetime,
    window_since: datetime,
    window_until: datetime,
    window_days: int,
) -> None:
    """Write the canonical analysis JSON. Deterministic given fixed inputs."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": generated_at.isoformat(),
        "window": {
            "days": window_days,
            "since": window_since.isoformat(),
            "until": window_until.isoformat(),
        },
        "input_summary": dict(input_summary),
        "patterns": [_finding_to_json(f) for f in findings],
        "no_template_patterns": list(no_template),
    }
    text = json.dumps(body, indent=2, sort_keys=True, ensure_ascii=False)
    path.write_text(text + "\n")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_writers_json.py -v`
Expected: PASS, 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/writers.py python/analysis/tests/test_writers_json.py
git commit -m "feat(analysis): JSON canonical writer with deterministic output"
```

---

## Task 12: Markdown writer

**Files:**
- Modify: `python/analysis/writers.py` (add `write_markdown`)
- Create: `python/analysis/tests/test_writers_markdown.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_writers_markdown.py
"""Tests for markdown projection writer."""
import json
from datetime import datetime, timezone
from pathlib import Path

from analysis.writers import write_markdown_from_json


def _sample_json(tmp_path: Path) -> Path:
    p = tmp_path / "decisions-2026-04-30.json"
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": "2026-04-30T12:00:00+00:00",
        "window": {
            "days": 7,
            "since": "2026-04-23T12:00:00+00:00",
            "until": "2026-04-30T12:00:00+00:00",
        },
        "input_summary": {
            "total_decisions": 1225, "denies": 62, "allows": 1163,
            "files_read": 6, "parse_errors": 0, "distinct_rule_ids": 14,
        },
        "patterns": [
            {
                "rank": 1,
                "rule_id": "no-destructive-rm",
                "action_type": "shell.exec",
                "agent_id": "copilot-cli",
                "count": 19,
                "first_seen": "2026-04-23T08:00:00+00:00",
                "last_seen": "2026-04-30T11:00:00+00:00",
                "decision_class": "deny",
                "sample_envelope_ids": ["env_001", "env_002", "env_003"],
                "draft": {
                    "kind": "heuristic",
                    "template": "no_destructive_rm",
                    "confidence": "medium",
                    "rule_yaml": "rules:\n  - id: x\n",
                    "predicted_impact": {
                        "samples_evaluated": 19, "would_allow": 12,
                        "would_still_deny": 7, "method": "regex",
                    },
                    "notes": "n",
                },
            }
        ],
        "no_template_patterns": [
            {"rule_id": "envelope-exhausted", "action_type": "?",
             "agent_id": "?", "count": 2,
             "reason_no_template": "structural"}
        ],
    }
    p.write_text(json.dumps(body, indent=2, sort_keys=True))
    return p


def test_markdown_renders_top_pattern(tmp_path):
    json_path = _sample_json(tmp_path)
    md_path = tmp_path / "decisions-2026-04-30.md"
    write_markdown_from_json(json_path, md_path)
    md = md_path.read_text()
    assert "# Decisions Analysis — 2026-04-30" in md
    assert "1225 decisions" in md
    assert "no-destructive-rm" in md
    assert "19 denies" in md
    assert "samples_evaluated: 19" in md
    assert "12 of 19" in md or "12 / 19" in md or "would_allow: 12" in md
    assert "rules:" in md  # YAML block rendered


def test_markdown_renders_no_template_section(tmp_path):
    json_path = _sample_json(tmp_path)
    md_path = tmp_path / "out.md"
    write_markdown_from_json(json_path, md_path)
    md = md_path.read_text()
    assert "envelope-exhausted" in md


def test_markdown_deterministic(tmp_path):
    json_path = _sample_json(tmp_path)
    a = tmp_path / "a.md"
    b = tmp_path / "b.md"
    write_markdown_from_json(json_path, a)
    write_markdown_from_json(json_path, b)
    assert a.read_bytes() == b.read_bytes()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_writers_markdown.py -v`
Expected: FAIL — `write_markdown_from_json` not defined.

- [ ] **Step 3: Add write_markdown_from_json to writers.py**

Add to the bottom of `python/analysis/writers.py`:

```python
def write_markdown_from_json(json_path: Path, md_path: Path) -> None:
    """Render the markdown projection from a canonical JSON file.

    Markdown is non-authoritative — JSON is the contract. This function reads
    the JSON and produces a deterministic markdown rendering.
    """
    json_path = Path(json_path)
    md_path = Path(md_path)
    md_path.parent.mkdir(parents=True, exist_ok=True)

    data = json.loads(json_path.read_text())
    lines: list[str] = []

    # Date for title from the until-bound (or generated_at).
    until = data["window"]["until"][:10]  # YYYY-MM-DD
    lines.append(f"# Decisions Analysis — {until}")
    lines.append("")
    lines.append(f"**Window:** {data['window']['days']}d "
                 f"({data['window']['since'][:10]} → {until})")
    summary = data["input_summary"]
    lines.append(f"**Input:** {summary['total_decisions']} decisions "
                 f"({summary['allows']} allowed, {summary['denies']} denied), "
                 f"{summary['distinct_rule_ids']} distinct rule_ids, "
                 f"{summary['parse_errors']} parse errors")
    lines.append("")
    lines.append("---")
    lines.append("")

    if not data["patterns"]:
        lines.append("_No deny patterns in this window._")
        lines.append("")
    else:
        lines.append("## Top patterns")
        lines.append("")
        for p in data["patterns"]:
            _render_pattern(p, lines)

    if data.get("no_template_patterns"):
        lines.append("---")
        lines.append("")
        lines.append("## Patterns without a template")
        lines.append("")
        lines.append("These deny patterns were observed but no heuristic template "
                     "knew how to draft a candidate rule. Surfaced for human review.")
        lines.append("")
        for nt in data["no_template_patterns"]:
            lines.append(f"- **{nt['rule_id']}** × {nt['action_type']} × "
                         f"{nt['agent_id']} — {nt['count']} denies "
                         f"({nt['reason_no_template']})")
        lines.append("")

    md_path.write_text("\n".join(lines))


def _render_pattern(p: dict[str, Any], lines: list[str]) -> None:
    header = (f"### #{p['rank']} — {p['rule_id']} × {p['action_type']} × "
              f"{p['agent_id']} ({p['count']} denies)")
    lines.append(header)
    lines.append("")
    lines.append(f"First seen: {p['first_seen']}. Last seen: {p['last_seen']}.")
    lines.append("")
    if p["draft"] is None:
        lines.append("_No candidate rule drafted._")
        lines.append("")
        return

    d = p["draft"]
    lines.append(f"**Candidate rule** ({d['kind']}, {d['confidence']} confidence, "
                 f"template `{d['template']}`):")
    lines.append("")
    lines.append("```yaml")
    lines.append(d["rule_yaml"].rstrip())
    lines.append("```")
    lines.append("")
    impact = d["predicted_impact"]
    lines.append(f"**Predicted impact:** samples_evaluated: {impact['samples_evaluated']}, "
                 f"would_allow: {impact['would_allow']}, "
                 f"would_still_deny: {impact['would_still_deny']} "
                 f"(method: {impact['method']})")
    lines.append("")
    if d["notes"]:
        lines.append(f"_Notes: {d['notes']}_")
        lines.append("")
    sample_ids = ", ".join(p["sample_envelope_ids"])
    lines.append(f"_Sample envelope ids:_ {sample_ids}")
    lines.append("")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_writers_markdown.py -v`
Expected: PASS, 3 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/writers.py python/analysis/tests/test_writers_markdown.py
git commit -m "feat(analysis): markdown projection writer (regenerated from JSON)"
```

---

## Task 13: CLI entry point

**Files:**
- Create: `python/analysis/decisions.py`
- Create: `python/analysis/__main__.py`
- Create: `python/analysis/tests/test_cli.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_cli.py
"""Tests for the decisions CLI entry."""
import json
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


def _stage_fixture(decisions_dir: Path, fixtures_dir: Path):
    """Copy the fixture as a real-looking gov-decisions file."""
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    (decisions_dir / "gov-decisions-2026-04-25.jsonl").write_text(src.read_text())


def test_cli_runs_end_to_end_on_fixture(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "100d",
         "--top-n", "10",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr

    out_files = sorted(out_dir.iterdir())
    json_files = [f for f in out_files if f.suffix == ".json"]
    md_files = [f for f in out_files if f.suffix == ".md"]
    assert len(json_files) == 1
    assert len(md_files) == 1

    body = json.loads(json_files[0].read_text())
    assert body["stream"] == "decisions"
    rule_ids = {p["rule_id"] for p in body["patterns"]}
    assert "no-destructive-rm" in rule_ids


def test_cli_handles_missing_decisions_dir(tmp_path):
    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "7d",
         "--out-dir", str(tmp_path / "out"),
         "--decisions-dir", str(tmp_path / "does-not-exist"),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 2
    assert "does not exist" in result.stderr.lower() or "not found" in result.stderr.lower()


def test_cli_empty_window_succeeds(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)

    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "1m",  # 1 minute — fixture is from 2026-04-25
         "--out-dir", str(tmp_path / "out"),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0
    body = json.loads(list((tmp_path / "out").glob("*.json"))[0].read_text())
    assert body["patterns"] == []


def test_cli_deterministic_with_fixed_now(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)

    def run(out_dir):
        return subprocess.run(
            [sys.executable, "-m", "analysis.decisions",
             "--window", "100d",
             "--out-dir", str(out_dir),
             "--decisions-dir", str(decisions_dir),
             "--now", "2026-04-30T12:00:00+00:00"],
            capture_output=True, text=True, check=True,
        )

    a_dir = tmp_path / "a"
    b_dir = tmp_path / "b"
    run(a_dir)
    run(b_dir)
    a = list(a_dir.glob("*.json"))[0].read_bytes()
    b = list(b_dir.glob("*.json"))[0].read_bytes()
    assert a == b
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_cli.py -v`
Expected: FAIL — module not yet exists.

- [ ] **Step 3: Implement CLI**

```python
# python/analysis/decisions.py
"""CLI entry: python -m analysis.decisions ..."""
from __future__ import annotations

import argparse
import os
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from analysis.detect import detect_patterns
from analysis.draft import draft_for_pattern, reason_no_template
from analysis.loaders import Window, load_gov_decisions, parse_window_str
from analysis.writers import build_finding, write_json, write_markdown_from_json
import analysis.templates.all  # noqa: F401  (registers all templates)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.decisions",
                                description="Rank deny patterns and draft candidate rules.")
    p.add_argument("--window", default="7d", help="Window: e.g., 7d, 24h, 60m")
    p.add_argument("--top-n", type=int, default=10, help="Top N patterns to keep")
    p.add_argument("--out-dir", default="python/analysis/out",
                   help="Output directory")
    p.add_argument("--decisions-dir",
                   default=os.environ.get("HOME", "/") + "/.chitin",
                   help="Directory containing gov-decisions-*.jsonl")
    p.add_argument("--now", default=None,
                   help="ISO-8601 to fix the clock (deterministic tests)")
    p.add_argument("--llm-draft", action="store_true",
                   help="Enable Layer 2 LLM-drafted rules (opt-in)")
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    if args.now:
        now = datetime.fromisoformat(args.now)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
    else:
        now = datetime.now(tz=timezone.utc)

    decisions_dir = Path(args.decisions_dir)
    if not decisions_dir.exists():
        print(f"Error: decisions-dir does not exist: {decisions_dir}", file=sys.stderr)
        return 2

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    window = parse_window_str(args.window, now)
    print(f"Loading decisions from {decisions_dir} (window: {args.window})...",
          file=sys.stderr)
    load_result = load_gov_decisions(decisions_dir, window)
    decisions = load_result.decisions
    print(f"  Loaded {len(decisions)} decisions from {load_result.files_read} files "
          f"({load_result.parse_errors} parse errors).", file=sys.stderr)

    patterns = detect_patterns(decisions)
    top = patterns[: args.top_n]
    rest = patterns[args.top_n :]
    print(f"Detected {len(patterns)} deny patterns; keeping top {len(top)}.",
          file=sys.stderr)

    findings = []
    no_template = []
    for i, p in enumerate(top, start=1):
        d = draft_for_pattern(p)
        if d is None:
            no_template.append({
                "rule_id": p.rule_id,
                "action_type": p.action_type,
                "agent_id": p.agent_id,
                "count": p.count,
                "reason_no_template": reason_no_template(p),
            })
        else:
            findings.append(build_finding(p, d, rank=i))

    # Patterns beyond top-N: surface their rule_ids in no_template (no draft attempted)
    for p in rest:
        no_template.append({
            "rule_id": p.rule_id,
            "action_type": p.action_type,
            "agent_id": p.agent_id,
            "count": p.count,
            "reason_no_template": "below top-N cutoff",
        })

    distinct_rule_ids = len(Counter(d.rule_id for d in decisions))
    denies = sum(1 for d in decisions if not d.allowed)
    allows = len(decisions) - denies
    summary = {
        "total_decisions": len(decisions),
        "denies": denies,
        "allows": allows,
        "files_read": load_result.files_read,
        "parse_errors": load_result.parse_errors,
        "distinct_rule_ids": distinct_rule_ids,
    }

    date_str = now.date().isoformat()
    json_path = out_dir / f"decisions-{date_str}.json"
    md_path = out_dir / f"decisions-{date_str}.md"

    try:
        write_json(json_path, findings=findings, no_template=no_template,
                   input_summary=summary, generated_at=now,
                   window_since=window.since, window_until=window.until,
                   window_days=int((window.until - window.since).total_seconds() // 86400) or 1)
        write_markdown_from_json(json_path, md_path)
    except OSError as e:
        print(f"Error writing output: {e}", file=sys.stderr)
        return 3

    print(f"Wrote {json_path}", file=sys.stderr)
    print(f"Wrote {md_path}", file=sys.stderr)
    if findings:
        top1 = findings[0]
        print(f"\nTop finding: {top1.pattern.rule_id} × {top1.pattern.action_type} "
              f"× {top1.pattern.agent_id} — {top1.pattern.count} denies", file=sys.stderr)
        if top1.draft:
            imp = top1.draft.predicted_impact
            print(f"  → predicted: {imp.would_allow} allows, "
                  f"{imp.would_still_deny} still deny", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

```python
# python/analysis/__main__.py
"""Allow `python -m analysis.decisions` to work via package main."""
# Empty — `python -m analysis.decisions` invokes decisions.py directly because
# decisions.py has its own __main__ guard. This file exists so `python -m analysis`
# (without the .decisions suffix) is unambiguous in error messages.
import sys
print("Use: python -m analysis.decisions", file=sys.stderr)
sys.exit(2)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_cli.py -v`
Expected: PASS, 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/decisions.py python/analysis/__main__.py python/analysis/tests/test_cli.py
git commit -m "feat(analysis): CLI entry — python -m analysis.decisions"
```

---

## Task 14: Foundation generalization stubs (debt + souls)

These prove the foundation is shape-correct. They write valid empty findings — every assertion future streams need to hit.

**Files:**
- Create: `python/analysis/debt.py`
- Create: `python/analysis/souls.py`
- Create: `python/analysis/tests/test_stubs.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_stubs.py
"""Tests that the debt and souls stubs produce valid empty findings.

This is the foundation-generalization proof: if the stubs can plug into
the same writers and produce valid JSON/markdown, the foundation is reusable.
"""
import json
import subprocess
import sys
from pathlib import Path


def test_debt_stub_writes_valid_empty_json(tmp_path):
    out_dir = tmp_path / "out"
    result = subprocess.run(
        [sys.executable, "-m", "analysis.debt",
         "--out-dir", str(out_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr
    json_path = next(out_dir.glob("debt-*.json"))
    body = json.loads(json_path.read_text())
    assert body["stream"] == "debt"
    assert body["patterns"] == []
    md_path = next(out_dir.glob("debt-*.md"))
    assert "Debt Analysis" in md_path.read_text()


def test_souls_stub_writes_valid_empty_json(tmp_path):
    out_dir = tmp_path / "out"
    result = subprocess.run(
        [sys.executable, "-m", "analysis.souls",
         "--out-dir", str(out_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr
    json_path = next(out_dir.glob("souls-*.json"))
    body = json.loads(json_path.read_text())
    assert body["stream"] == "souls"
    assert body["patterns"] == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_stubs.py -v`
Expected: FAIL.

- [ ] **Step 3: Implement debt + souls stubs**

```python
# python/analysis/debt.py
"""Debt stream stub. Foundation-generalization proof — produces valid empty
findings via the same writers as decisions.py.

v1: empty patterns, valid JSON/markdown. Plug in real detection in v2.
"""
from __future__ import annotations

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path

from analysis.writers import write_json, write_markdown_from_json


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.debt")
    p.add_argument("--out-dir", default="python/analysis/out")
    p.add_argument("--now", default=None)
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.now:
        now = datetime.fromisoformat(args.now)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
    else:
        now = datetime.now(tz=timezone.utc)

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    date_str = now.date().isoformat()
    json_path = out_dir / f"debt-{date_str}.json"
    md_path = out_dir / f"debt-{date_str}.md"

    write_json(json_path, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=now, window_until=now,
               window_days=7)
    # Override stream label by hand-patching JSON before markdown render.
    import json as _json
    body = _json.loads(json_path.read_text())
    body["stream"] = "debt"
    json_path.write_text(_json.dumps(body, indent=2, sort_keys=True) + "\n")

    # Render a minimal stub markdown directly.
    md_path.write_text(
        "# Debt Analysis — " + date_str + "\n\n"
        "_Stub stream. Real detection ships in v2._\n"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

```python
# python/analysis/souls.py
"""Souls stream stub. Foundation-generalization proof."""
from __future__ import annotations

import argparse
import json as _json
import sys
from datetime import datetime, timezone
from pathlib import Path

from analysis.writers import write_json


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(prog="analysis.souls")
    p.add_argument("--out-dir", default="python/analysis/out")
    p.add_argument("--now", default=None)
    return p.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    if args.now:
        now = datetime.fromisoformat(args.now)
        if now.tzinfo is None:
            now = now.replace(tzinfo=timezone.utc)
    else:
        now = datetime.now(tz=timezone.utc)

    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    date_str = now.date().isoformat()
    json_path = out_dir / f"souls-{date_str}.json"
    md_path = out_dir / f"souls-{date_str}.md"

    write_json(json_path, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=now, window_until=now,
               window_days=7)
    body = _json.loads(json_path.read_text())
    body["stream"] = "souls"
    json_path.write_text(_json.dumps(body, indent=2, sort_keys=True) + "\n")

    md_path.write_text(
        "# Souls Analysis — " + date_str + "\n\n"
        "_Stub stream. Real detection ships in v2._\n"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_stubs.py -v`
Expected: PASS, 2 tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/analysis/debt.py python/analysis/souls.py python/analysis/tests/test_stubs.py
git commit -m "feat(analysis): debt + souls stream stubs (foundation-generalization proof)"
```

---

## Task 15: LLM-drafted rules (Layer 2, opt-in)

**Files:**
- Create: `python/analysis/llm_draft.py`
- Modify: `python/analysis/decisions.py` (wire `--llm-draft`)
- Create: `python/analysis/tests/test_llm_draft.py`

- [ ] **Step 1: Write the failing test**

```python
# python/analysis/tests/test_llm_draft.py
"""Tests for Layer 2 LLM-drafted rules.

The LLM call is mocked. We test the fallback contract: any failure in the
LLM path falls back to the heuristic draft, never raises, never aborts.
"""
from datetime import datetime, timezone
from unittest.mock import patch

from analysis.llm_draft import enrich_with_llm
from analysis.types import Pattern, PredictedImpact, RuleDraft


def _heuristic_draft():
    return RuleDraft(
        kind="heuristic",
        template="t",
        confidence="medium",
        rule_yaml="rules: []\n",
        predicted_impact=PredictedImpact(samples_evaluated=1, would_allow=1,
                                         would_still_deny=0, method="m"),
        notes="",
    )


def _pattern():
    return Pattern(
        rule_id="r", action_type="t", agent_id="a", count=1,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny", sample_envelope_ids=("e1",), decisions=(),
    )


def test_llm_failure_falls_back_to_heuristic():
    """If the LLM call raises, we fall back to the heuristic draft."""
    with patch("analysis.llm_draft._call_ollama", side_effect=RuntimeError("boom")):
        out = enrich_with_llm([(_pattern(), _heuristic_draft())])
    assert len(out) == 1
    assert out[0].kind == "heuristic-fallback"
    # Same yaml as heuristic on fallback.
    assert out[0].rule_yaml == "rules: []\n"


def test_llm_success_returns_llm_draft():
    fake_yaml = "rules:\n  - id: llm-drafted\n"

    def fake_call(*args, **kwargs):
        return fake_yaml

    with patch("analysis.llm_draft._call_ollama", side_effect=fake_call):
        out = enrich_with_llm([(_pattern(), _heuristic_draft())])
    assert len(out) == 1
    assert out[0].kind == "llm"
    assert "llm-drafted" in out[0].rule_yaml


def test_no_heuristic_passes_through():
    """If a pattern has no heuristic draft, LLM enrichment skips it."""
    with patch("analysis.llm_draft._call_ollama", return_value="..."):
        out = enrich_with_llm([(_pattern(), None)])
    assert out == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python/analysis && pytest tests/test_llm_draft.py -v`
Expected: FAIL — module not yet defined.

- [ ] **Step 3: Implement llm_draft**

```python
# python/analysis/llm_draft.py
"""Layer 2 — LLM-drafted rules via local ollama. Opt-in, fail-safe.

Any failure (network, parse, timeout) falls back to the heuristic draft, marked
kind='heuristic-fallback'. The LLM is never load-bearing for the demo.
"""
from __future__ import annotations

import json
import urllib.request
from typing import Iterable, Optional

from analysis.types import Pattern, RuleDraft

OLLAMA_URL = "http://localhost:11434/api/generate"
OLLAMA_MODEL = "qwen3-coder"
TIMEOUT_SECONDS = 30.0


def _build_prompt(pattern: Pattern, heuristic: RuleDraft) -> str:
    samples = "\n".join(
        f"  - {d.action_target!r} (envelope {d.envelope_id})"
        for d in pattern.decisions[:5]
    )
    return (
        f"You are drafting a chitin governance rule. Return ONLY YAML, no prose.\n"
        f"\n"
        f"Observed pattern: rule_id={pattern.rule_id}, "
        f"action_type={pattern.action_type}, agent={pattern.agent_id}, "
        f"count={pattern.count}.\n"
        f"\n"
        f"Sample denied actions:\n{samples}\n"
        f"\n"
        f"Heuristic draft (improve on this if you can):\n"
        f"{heuristic.rule_yaml}\n"
        f"\n"
        f"Reply with a single YAML block proposing a refinement. Be conservative."
    )


def _call_ollama(prompt: str) -> str:
    req = urllib.request.Request(
        OLLAMA_URL,
        data=json.dumps({
            "model": OLLAMA_MODEL,
            "prompt": prompt,
            "stream": False,
        }).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=TIMEOUT_SECONDS) as resp:
        body = json.loads(resp.read().decode("utf-8"))
    return body.get("response", "")


def enrich_with_llm(
    pairs: Iterable[tuple[Pattern, Optional[RuleDraft]]],
) -> list[RuleDraft]:
    """For each (pattern, heuristic_draft) pair, attempt LLM enrichment.

    Returns a list aligned with the input (same length, same order) but
    skipping pairs where heuristic_draft is None.
    """
    out: list[RuleDraft] = []
    for pattern, heuristic in pairs:
        if heuristic is None:
            continue
        try:
            prompt = _build_prompt(pattern, heuristic)
            response = _call_ollama(prompt)
            if not response.strip():
                raise RuntimeError("empty LLM response")
            out.append(RuleDraft(
                kind="llm",
                template=heuristic.template,
                confidence="medium",
                rule_yaml=response.strip() + "\n",
                predicted_impact=heuristic.predicted_impact,  # not re-evaluated by LLM in v1
                notes=f"LLM-enriched (ollama {OLLAMA_MODEL}). Heuristic was: {heuristic.template}",
            ))
        except Exception as e:
            # Fail-safe: fall back to heuristic, mark the failure.
            out.append(RuleDraft(
                kind="heuristic-fallback",
                template=heuristic.template,
                confidence=heuristic.confidence,
                rule_yaml=heuristic.rule_yaml,
                predicted_impact=heuristic.predicted_impact,
                notes=f"LLM enrichment failed ({e}); fell back to heuristic.",
            ))
    return out
```

- [ ] **Step 4: Wire `--llm-draft` into CLI**

Modify `python/analysis/decisions.py` — add after the heuristic-draft loop, just before `distinct_rule_ids = ...`:

```python
    if args.llm_draft and findings:
        from analysis.llm_draft import enrich_with_llm
        pairs = [(f.pattern, f.draft) for f in findings]
        enriched_drafts = enrich_with_llm(pairs)
        # Re-pair with patterns by index (enrich_with_llm preserves order, skipping Nones)
        new_findings = []
        for f, new_draft in zip(findings, enriched_drafts):
            new_findings.append(build_finding(f.pattern, new_draft, rank=f.rank))
        findings = new_findings
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd python/analysis && pytest tests/test_llm_draft.py -v`
Expected: PASS, 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add python/analysis/llm_draft.py python/analysis/decisions.py python/analysis/tests/test_llm_draft.py
git commit -m "feat(analysis): Layer 2 LLM-drafted rules with heuristic fallback (opt-in)"
```

---

## Task 16: End-to-end run on real data + talk-day rehearsal

This is the validation step — run on the user's actual `$HOME/.chitin/gov-decisions-*.jsonl` and confirm the demo path produces talk-ready output.

**Files:**
- Create: `python/analysis/tests/test_e2e.py` (skipped in CI, opt-in via env)

- [ ] **Step 1: Write the integration test (opt-in)**

```python
# python/analysis/tests/test_e2e.py
"""End-to-end test against real $HOME/.chitin gov-decisions data.

Skipped unless ANALYSIS_E2E=1 in env. This is a developer rehearsal harness,
not a CI test.
"""
import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

if os.environ.get("ANALYSIS_E2E") != "1":
    pytest.skip("Set ANALYSIS_E2E=1 to run e2e against real ~/.chitin data",
                allow_module_level=True)


def test_e2e_real_data(tmp_path):
    out_dir = tmp_path / "out"
    decisions_dir = Path(os.environ["HOME"]) / ".chitin"
    assert decisions_dir.exists(), "no ~/.chitin — cannot run e2e"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "7d",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir)],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr
    json_files = list(out_dir.glob("decisions-*.json"))
    assert len(json_files) == 1
    body = json.loads(json_files[0].read_text())
    assert body["input_summary"]["total_decisions"] > 0
    print("Top finding:", body["patterns"][0] if body["patterns"] else "none")
```

- [ ] **Step 2: Run the e2e test against real data**

Run: `cd python/analysis && ANALYSIS_E2E=1 pytest tests/test_e2e.py -v -s`
Expected: PASS. Stdout shows the top finding.

- [ ] **Step 3: Manual rehearsal — run the demo command**

Run from repo root:
```bash
cd /home/red/workspace/chitin-analysis
python -m analysis.decisions --window 7d --decisions-dir $HOME/.chitin
```

Expected output (stderr):
```
Loading decisions from /home/red/.chitin (window: 7d)...
  Loaded ~1200 decisions from ~6 files (0 parse errors).
Detected ~10 deny patterns; keeping top 10.
Wrote python/analysis/out/decisions-2026-04-30.json
Wrote python/analysis/out/decisions-2026-04-30.md

Top finding: no-destructive-rm × shell.exec × copilot-cli — 19 denies
  → predicted: ~12 allows, ~7 still deny
```

- [ ] **Step 4: Inspect the markdown output**

Open `python/analysis/out/decisions-2026-04-30.md` and verify:
- Title and summary line look right
- Top pattern is `no-destructive-rm × shell.exec × copilot-cli` with ~19 denies
- Candidate rule YAML block renders cleanly
- Predicted impact shows non-zero `would_allow`
- `no_template_patterns` section lists rule_ids without templates honestly

- [ ] **Step 5: Commit the integration test (do NOT commit the out/ files)**

```bash
git add python/analysis/tests/test_e2e.py
git commit -m "test(analysis): opt-in end-to-end test against real ~/.chitin data"
```

- [ ] **Step 6: Run the full test suite as a final check**

Run: `cd python/analysis && pytest -v`
Expected: All tests pass (e2e skipped without env var).

---

## Task 17: PR

- [ ] **Step 1: Push the branch**

```bash
git push -u origin spec/analysis-decisions
```

- [ ] **Step 2: Create the PR**

```bash
gh pr create --title "feat(analysis): decisions stream — repeat-pattern detection + candidate rule drafts" --body "$(cat <<'EOF'
## Summary
- Ships the decisions analysis stream (Milestone J3): reads `$HOME/.chitin/gov-decisions-*.jsonl`, ranks deny patterns, drafts candidate rules from heuristic templates, emits JSON canonical + markdown projection.
- Foundation extracted in `python/analysis/` so debt + soul-routing streams plug in as ~1-day additions post-talk (proven by stubs that share the writers).
- Layer 2 LLM-drafted rules opt-in via `--llm-draft`; off by default to keep the talk demo deterministic.

## Spec
`docs/superpowers/specs/2026-04-30-analysis-decisions-design.md`

## Talk demo (2026-05-07)
```bash
python -m analysis.decisions --window 7d
```
Top finding from real data: `no-destructive-rm × shell.exec × copilot-cli` (19 denies). Candidate rule proposes safe-dirs exemption with predicted impact (~12 allows, ~7 still deny).

## Test plan
- [x] Unit tests: parsing, loading, detection (boundaries: empty/single/all-same/tied), four templates, registry, JSON+markdown writers
- [x] CLI integration tests with deterministic `--now` fixed clock
- [x] Opt-in e2e test (`ANALYSIS_E2E=1`) against real `~/.chitin` data
- [x] Stub streams (debt, souls) write valid empty JSON via shared writers — foundation-generalization proof

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review

**1. Spec coverage** — every spec section has an implementing task:

| Spec section | Task |
|---|---|
| Goals 1-6 (foundation, decisions full, Layer 1 demo, Layer 2 opt-in, talk artifact, stubs) | Tasks 1-15 |
| Invariants I1 (ranked, tie-break) | Task 4 (test_tie_breaker_*) |
| Invariants I2 (JSON+markdown, regenerable) | Tasks 11+12 |
| Invariants I3 (Layer 1 zero-network) | All Layer 1 tasks (no urllib in detect/draft/templates) |
| Invariants I4 (deterministic with --now) | Task 13 (test_cli_deterministic_with_fixed_now) |
| Invariants I5 (bad lines don't abort) | Tasks 2+3 (test_parse_malformed_*, test_load parse_errors counts) |
| Boundaries (empty/single/all-same/tied/null/window-edge) | Task 4 (test_empty_input, test_single_decision, test_tie_breaker_*, test_null_*); Task 3 (test_load_with_empty_dir, test_narrow_window) |
| Layout, CLI, JSON schema, markdown projection | Tasks 11-13 |
| Detection algorithm | Task 4 |
| Templates 1-4 | Tasks 6-9 |
| Layer 2 fallback | Task 15 |
| Failure handling | Tasks 2 (parse), 3 (load), 13 (write), 15 (LLM) |
| Talk-day demo | Task 16 |
| Foundation extraction (C lean) | Task 14 (debt + souls stubs) |

**2. Placeholder scan** — `git grep -n "TBD\|TODO\|fill in"` in this plan: none.

**3. Type consistency** — `Decision`, `Pattern`, `RuleDraft`, `PredictedImpact`, `Finding` defined in Task 2 + Task 11. Used consistently in tasks 4, 5, 6-10, 11-13. Field names (`samples_evaluated`, `would_allow`, `would_still_deny`, `method`) consistent across all template tasks. Function names (`draft`, `register`, `draft_for_pattern`, `enrich_with_llm`, `write_json`, `write_markdown_from_json`, `build_finding`) consistent.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-30-analysis-decisions.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
