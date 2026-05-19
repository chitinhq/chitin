# Minnie session — status contract

You are running inside a Minnie session managed by `swarm/bin/minnie`.

**Goal**: {goal}

**Session id**: `{goal_id}`
**State directory**: `{state_dir}`
**Status file**: `{status_path}`

## Required: write status.json after every meaningful step

You MUST write `{status_path}` periodically with the following six fields:

```json
{{
  "state": "starting | working | blocked | verifying | done | failed | needs_review",
  "updated_at": <unix_timestamp_seconds>,
  "summary": "<one-line current action>",
  "next": "<one-line next intended step>",
  "blockers": ["<human-needed blocker>", "..."],
  "verify": "<shell command that exits 0 iff goal is satisfied>"
}}
```

Rules:

- `state` must be one of the exact enum values above (no other strings).
- `updated_at` is unix epoch seconds (integer or float).
- Re-write the file at least every 60 seconds while `state == "working"`.
- Write atomically — write to `status.json.tmp` then rename to `status.json`.
- `blockers` is an empty list `[]` when nothing is blocking.
- `verify` is the shell command that proves completion (e.g.
  `pytest swarm/tests/test_foo.py` or `make slice1`). Set it as soon as
  you know what success looks like — don't wait until done.

The outer Minnie controller treats `updated_at` as the primary liveness
signal — not terminal output. A stale `updated_at` will trigger a nudge.

## State transitions you should write

- `starting` — initial setup, reading spec, planning
- `working` — actively making changes
- `blocked` — waiting on operator input (populate `blockers`)
- `verifying` — running `verify` to confirm completion
- `done` — `verify` passed, work complete (controller will re-verify independently)
- `needs_review` — work complete but no verifier defined; operator review needed
- `failed` — irrecoverable error (populate `summary` with reason)

## /goal

Begin work on the goal above. Set `state=starting` and write status.json before
doing anything else, then proceed.
