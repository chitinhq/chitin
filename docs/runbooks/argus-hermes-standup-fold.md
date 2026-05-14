# Argus → Hermes → Operator (standup-fold)

Status: Slice 5 contract  •  Date: 2026-05-13

## Purpose

Argus surfaces patterns and drift continuously; the operator only reads
Hermes' standups. This runbook documents the one-way flow that folds
Argus findings into the existing Hermes standup so the operator sees one
unified feed instead of two parallel streams.

## Flow

```
   ┌──────────────┐  argus findings --since=<ts>   ┌──────────────┐
   │   Argus      │  ─────────────────────────────▶ │    Hermes    │
   │ (observatory)│                                 │ (standup gen)│
   └──────────────┘                                 └──────┬───────┘
                                                           │
                                                           │ summary
                                                           ▼
                                                  ┌──────────────┐
                                                  │   Operator   │
                                                  └──────────────┘
```

**Invariants:**

- One-way. Argus → Hermes → operator. Hermes NEVER writes back into the
  Argus index.
- Graceful degradation. If `argus findings` errors or the binary is
  missing, Hermes' standup omits the section silently — never blocks
  the standup itself.
- Cap at 3 in the standup. The full list lives in Argus's daily HTML
  digest; the standup is the headline, not the archive.

## CLI contract

```
$ argus findings --since=<unix_ts> [--indent N] [--limit N]
[
  {
    "kind": "demote_loop",
    "severity": "warning",
    "summary": "Demote loop: t_abc bounced ready→triage 2× in 24h",
    "evidence_links": ["kanban_event#12", "kanban_event#15"],
    "suggested_action": "investigate",
    "ts_unix": 1700000000,
    "subject": "t_abc"
  },
  ...
]
```

Findings are sorted by severity descending (critical → warning → info),
then by ts_unix descending, then by subject ascending. Hermes can take
the first N and fold them in.

### `suggested_action` values

| Action            | When                                                | Operator's expected reply                  |
|-------------------|-----------------------------------------------------|--------------------------------------------|
| `investigate`     | Pattern detected, needs human triage                | "dispatch finding-N" / "ack finding-N"     |
| `dispatch_fix`    | Concrete failure that has a known fix path          | "dispatch finding-N" → kanban ticket filed |
| `archive_belief`  | Stale or evidence-free belief                       | "archive finding-N" → belief marked stale  |
| `file_ticket`     | Capability gap operator may want to track formally  | "file finding-N" → triage ticket created   |

## Hermes-side integration (pseudocode)

```python
# agent/standup.py (or equivalent cron job)
import subprocess, json, time

def get_argus_findings(since_ts: int, limit: int = 3) -> list[dict]:
    try:
        out = subprocess.run(
            ["argus", "findings", "--since", str(since_ts), "--limit", str(limit)],
            capture_output=True, text=True, timeout=10, check=False,
        )
        if out.returncode != 0:
            return []
        return json.loads(out.stdout)
    except (FileNotFoundError, subprocess.TimeoutExpired, json.JSONDecodeError):
        return []  # graceful degrade — never block the standup

def fold_into_standup(report: StandupReport):
    last_standup_ts = report.previous_run_ts or 0
    findings = get_argus_findings(since_ts=last_standup_ts, limit=3)
    if not findings:
        return  # quiet day — omit the section entirely
    report.add_section(
        title="Argus says:",
        bullets=[
            f"[{f['severity']}] {f['summary']}  (action: {f['suggested_action']})"
            for f in findings
        ],
    )
```

## Operator reply grammar

Hermes' existing reply-action grammar handles `dispatch <id>`, `ack
<id>`, `archive <id>`. Argus finding IDs in the standup are positional
(`finding-1`, `finding-2`, `finding-3`); Hermes resolves these to the
underlying kanban / belief / git PR based on the finding's `kind`:

- `dispatch finding-3` on a `demote_loop` → Hermes promotes the
  associated `t_*` ticket through triage with priority bump.
- `archive finding-3` on a `stale_belief` → Hermes flips the belief
  source's last-reaffirmed timestamp to now (or marks it stale).
- `file finding-3` on a `capability_without_belief` → Hermes opens a
  triage ticket "Update agent card: <agent> uses <action_type> ×N".

## Acceptance (Slice 5 spec)

- [x] `argus findings --since=<ts>` CLI emits the JSON shape above
- [x] Sorting deterministic (severity desc → ts desc → subject)
- [x] Empty data → empty list; missing DBs → empty list, no crash
- [x] CLI smoke against local data produces a real findings list
- [ ] Hermes-side `agent/standup.py` integration (separate repo, follow-up)
- [ ] 7 consecutive standups include Argus section (operator-side smoke)
- [ ] One operator action taken via standup reply (operator-side smoke)

## Out of scope

- Direct push from Argus to `#ares` independent of Hermes. Default is
  Hermes-only fold; operators can enable per-detector direct push if a
  finding category proves time-sensitive.
- The Hermes-side reply-action wiring lives in hermes' repo. This
  runbook documents the contract; the actual integration is a follow-
  up ticket on the hermes side.
