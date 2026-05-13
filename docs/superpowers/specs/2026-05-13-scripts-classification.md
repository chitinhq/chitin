# Spec: classify scripts/ and move runtime-critical logic out

Date: 2026-05-13
Status: spec — open
Kanban: `t_75c8c8c1` (priority 25)
Source: `docs/audits/2026-05-13-architecture-audit.md` — Top finding 3
Author: claude-code (operator-controlled, spec writer)

## Problem

Architecture audit 2026-05-13: `scripts/` has 45 files, 18 commits
in 7d, and substantive hits on `gate`, `router`, `governance`,
`hook`, `hermes`. A directory that mixes installers, operator
helpers, and runtime-critical code is dangerous because:

1. The Go boundary check (spec
   `2026-05-13-go-only-governance-authority.md`) treats `scripts/`
   as non-Go and will scrutinize it for policy logic. We need to be
   able to declare *which* scripts are policy-adjacent on purpose
   (installers writing rule files) versus accidental.
2. Tests don't run against shell. A bug in a runtime-critical
   shell script ships without the regression coverage that Go code
   gets.
3. Operators reading the tree to understand "what runs and when"
   have to read each file's contents to classify it. AI navigation
   cost is the same.

Listing of current `scripts/` top level (from `ls scripts/`):

```
bootstrap-worktree.sh           install-claude-code-hook.sh
chitin-agent-unlock.sh          install-codex-hook.sh
chitin-budget                   install-gemini-hook.sh
chitin-chain-watch.sh           install-hermes-hook.sh
chitin-envelope-rotate.sh       install-hermes-plugin.sh
chitin-status                   install-kernel.sh
create-worktree.sh              install-kernel-symlink.sh
hermes/                         install-systemd-units.sh
kanban-flow                     migrate-ready-assignees-to-clawta.py
mine-default-deny-bash.sh       replay-hook-captures.sh
smoke-hermes-clawta-chain.sh    sync-kanban-dispatch.sh
```

Quick read of the names suggests:

- **installers**: `install-*` (8 of them), `bootstrap-worktree.sh`,
  `create-worktree.sh`
- **operator ops helpers**: `chitin-status`, `chitin-budget`,
  `chitin-agent-unlock.sh`, `chitin-envelope-rotate.sh`,
  `chitin-chain-watch.sh`
- **migrations / one-shots**: `migrate-ready-assignees-to-clawta.py`,
  `mine-default-deny-bash.sh`
- **runtime-critical**: `kanban-flow` (every SDLC transition runs
  through this), `hermes/tick.sh` (cron driver?),
  `sync-kanban-dispatch.sh` (chain-replay?)
- **smoke tests**: `smoke-hermes-clawta-chain.sh`,
  `replay-hook-captures.sh`

This is provisional — the spec body details the formal
classification process.

## Invariant (the claim)

> Every file under `scripts/` is tagged with exactly one of four
> categories: `ci`, `migration`, `operator`, `runtime-critical`.
> Runtime-critical scripts have either (a) a regression test in a
> tracked test directory, or (b) a documented plan to be ported to
> a Go package API. No script is unclassified.

A script is **runtime-critical** if any of:

- It is invoked by a systemd unit, cron, or chitin / hermes /
  swarm daemon at runtime.
- Its behavior is part of the SDLC happy path (e.g. `kanban-flow`
  is invoked by `clawta-poller` every tick).
- Removing it breaks a chitin invariant (e.g. log rotation, envelope
  rotation, chain-watch backpressure).

A script is **operator** if it exists for human invocation
(`chitin-status`), runs ad-hoc, and its failure is recoverable by
the operator running it again.

A script is **migration** if it is a one-shot data fix run during
a specific upgrade and not expected to run again.

A script is **ci** if it is only invoked from GitHub Actions / a CI
runner and never from a production process.

## Decision

Add a manifest file (`scripts/MANIFEST.yaml`) that enumerates each
script with its category and one-line purpose. Add a CI check
(`scripts/check-scripts-manifest.sh`) that fails on:

1. An untracked script (file present but not in MANIFEST).
2. A runtime-critical script with no test file referenced and no
   followup ticket linked.
3. A migration script older than 90 days that hasn't been deleted.

The MANIFEST is the chokepoint that forces classification before
merge. The check is the enforcer.

## In scope

1. **`scripts/MANIFEST.yaml`** — flat list, one entry per script,
   format below.
2. **`scripts/check-scripts-manifest.sh`** — linter. Pure bash +
   `yq`. CI-wired.
3. **First-pass classification** of all 28 current scripts (~half
   day of read-and-classify).
4. **For each `runtime-critical`** script without a test:
   - File a followup ticket to either (a) add a smoke/regression
     test, or (b) port to a Go package API.
   - Link the followup ticket id in the MANIFEST entry so the
     check can verify it.
5. **`docs/runbooks/scripts-classification.md`** — operator-facing
   guide explaining the four categories, when to add a script,
   when to delete one, and how to write the MANIFEST entry.

## Out of scope (followups)

- Actually porting `kanban-flow` to Go — that's a separate ticket
  (also referenced by `2026-05-13-isolate-swarm-kanban-mutations.md`).
- Adding regression tests for every runtime-critical script —
  tracked via per-script followup tickets after classification.
- Subdirectory recursion (`scripts/hermes/`, etc.) — classification
  applies recursively but tooling treats subdirs the same as top
  level. No special-casing.
- Auto-deletion of expired migrations — the linter *flags* them;
  operator confirms deletion.

## Approach detail

### MANIFEST format

```yaml
# scripts/MANIFEST.yaml
# Every file under scripts/ must appear exactly once.
# category: ci | migration | operator | runtime-critical
# Each runtime-critical entry must have a `tested_by` field or a
# `port_ticket` field.

- path: scripts/kanban-flow
  category: runtime-critical
  purpose: canonical kanban mutation wrapper for the swarm SDLC
  tested_by: null
  port_ticket: t_77f5b407-followup-1  # Go rewrite

- path: scripts/chitin-status
  category: operator
  purpose: human-readable runtime status snapshot for a chitin host

- path: scripts/install-claude-code-hook.sh
  category: ci  # also runs locally on operator install — see note
  purpose: installs the claude-code pre-tool hook into the user's settings
  notes: |
    Installer scripts run both in CI (smoke test) and locally
    (operator install). CI category because the regression check
    that matters runs in CI.

- path: scripts/migrate-ready-assignees-to-clawta.py
  category: migration
  purpose: one-shot migration from <2026-05-11 schema
  added_on: 2026-05-11
  expires_on: 2026-08-09  # 90d
```

### Linter contract

Exit codes:

- `0` — every script tracked, every runtime-critical has
  `tested_by` or `port_ticket`, no migration expired.
- `1` — drift detected. Stdout enumerates: untracked files,
  runtime-critical without coverage, expired migrations.

The 90-day TTL on migrations is policy: a migration script that
sits in the tree past its window is either still needed (turn it
into a runtime-critical with a test) or stale (delete it). The
linter forces the call.

### Test-reference convention

`tested_by` is one of:

- `null` paired with `port_ticket: <id>` — no test today, scheduled
  to port to Go.
- A file path: `tests/scripts/test_kanban_flow.bats` or similar.

The linter does **not** validate the test file passes; it only
checks the field is non-null.

## Verification

- **Bootstrap pass**: write MANIFEST.yaml covering all 28 current
  scripts; linter exits 0.
- **Untracked detection**: add `scripts/dummy.sh` without a
  MANIFEST entry; CI fails with "untracked".
- **Migration TTL**: set a migration entry's `expires_on` to
  yesterday; linter fails with "expired migration".
- **Runtime-critical without coverage**: add a new entry with
  `category: runtime-critical` and no `tested_by` / `port_ticket`;
  linter fails with "needs test or port plan".

## Done-condition

- `scripts/MANIFEST.yaml` exists, covers every file under
  `scripts/` (recursively), and `check-scripts-manifest.sh` exits
  0 on `main`.
- CI workflow invokes the linter on every PR touching `scripts/`.
- Every runtime-critical script has a `tested_by` or a
  `port_ticket` and the corresponding ticket exists on the kanban.
- `docs/runbooks/scripts-classification.md` exists and is linked
  from the operator runbook index.

## Effort

M. The classification pass is ~half a day. Writing the manifest
schema + linter is half a day. Filing the per-script followup
tickets for `runtime-critical` items is another half-day depending
on how many surface. Total ~1.5 days.
