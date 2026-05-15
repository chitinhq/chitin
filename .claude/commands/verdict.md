# /verdict — Durable disposition state for PRs and tickets

Record a disposition **once**, with a reason and a scope, into an
append-only ledger. `/queue` is designed to read the ledger instead of
re-deriving state every run (ledger integration is a planned follow-up).
Triage becomes O(new) — only items whose state actually moved — instead
of O(all).

A verdict is durable: it stays authoritative until the underlying
thing changes (a PR's head moves) or someone explicitly reopens it.

## Usage

```
/verdict <pr|ticket-id> <disposition> "<reason>"   — Record a disposition
/verdict show <id>                                 — Show current verdict + history
/verdict list [stale]                              — All verdicts; `stale` = head moved past verdict
/verdict reopen <id> "<why>"                        — Invalidate a verdict, send back to triage
```

## Disposition vocabulary

Standardized values — `/queue` keys its display and skip logic off
these (parallels the `block_reason` vocabulary used by Hermes):

| Disposition | Meaning | /queue behaviour |
|---|---|---|
| `merge-ready` | CI green, reviewed, no blockers | surfaced in `merge all green` batch |
| `rework` | Real must-fix items; reason lists them | shown with must-fix count, skipped from re-review |
| `blocked-environmental` | Non-code blocker (token scope, infra) | shown once, not re-triaged until reopened |
| `defer` | Low-priority + stale; archive candidate | folded into `defer` batch |
| `wont-merge` | Superseded / rejected by design | hidden from default view |

## The ledger

Append-only JSONL at `~/.hermes/verdicts/<board>.jsonl`. One line per
record:

```json
{
  "id": "pr-628",
  "disposition": "blocked-environmental",
  "reason": "merge blocked: OAuth token missing workflow scope; needs token refresh or web-UI merge",
  "scope": "no code work required",
  "head_sha": "a1b2c3d",
  "recorded_at": "2026-05-14T20:00:00-04:00",
  "recorded_by": "claude-code",
  "reopened": false
}
```

`head_sha` is the disposition's anchor. For a PR, capture it with
`gh pr view <n> --json headRefOid -q .headRefOid` at record time.

## Staleness — the O(new) mechanism

A verdict is **current** while the PR head still equals `head_sha`.
When new commits land, the verdict goes **stale**: `/queue` and
`/verdict list stale` flag it for re-triage, and only those items get
a fresh deep-dive. Everything with a current verdict is read, not
re-derived.

Tickets (no head SHA) use the kanban `updated_at` instead: a verdict
goes stale if the ticket changed after `recorded_at`.

## Contract with /queue

`/queue` consults `~/.hermes/verdicts/<board>.jsonl` before its PR
section:

- PR with a **current** verdict → one line: `#628 [verdict:
  blocked-environmental] "needs token refresh"` — no deep re-triage.
- PR with a **stale** verdict → re-triaged normally; a new `/verdict`
  supersedes the old line.
- PR with **no** verdict → triaged normally (this is the "new" in
  O(new)).

`/verdict reopen` is the only way to re-triage a still-current
verdict — disposition decisions don't get silently re-litigated.

## Chains with

- **Input** ← terminal step of `/land` (per-PR review outcome),
  `/gate preflight` (hits → `rework` with gate names as must-fix),
  `/invariant` (a superseding PR → old PR `wont-merge` or `merge-ready`),
  or adversarial review.
- **Reader** → `/queue`: the whole point. Verdicts are what make the
  queue cheap.

Goal-runner form: any `/goal` that dispositions PRs ends each item
with a `/verdict` write so the next run starts from state, not from
scratch.

## When to use

- End of every `/land` or `/queue` deep-dive — record the call you
  just made so it sticks.
- Any PR that's blocked for a reason that won't change on its own
  (environmental blockers especially — record once, stop rediscovering).

## Why this exists

Remote-control interview, 2026-05-14: sessions S174–S178 — five
"queue status check" runs in ~3 hours, each re-deriving the same
state from scratch. "The disposition state should be a fact the queue
reads, not something I rediscover." Disposition decisions should be
durable unless explicitly reopened.
