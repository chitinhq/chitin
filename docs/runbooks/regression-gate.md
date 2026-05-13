# Regression-gate runbook

How the regression-gate works, how to author an invariant, how to
override a false positive, and the one-time operator setup.

Schema and design in
[`docs/superpowers/specs/2026-05-13-regression-gate.md`](../superpowers/specs/2026-05-13-regression-gate.md).

## What the gate does

For every PR, every invariant in `scripts/check-*.{sh,py}` runs against
the PR's merged-state tree. If any invariant exits non-zero, the gate
fails and the PR cannot auto-merge until the failure is resolved.

The gate runs in two places:

1. **CI** — as part of the `test` job in `.github/workflows/ci.yml`.
   This is the binding gate visible to humans on the PR page.
2. **`swarm/bin/clawta-pr-lifecycle`** — reruns the gate against the
   PR's merge-ref tree as the 6th auto-merge check, after CI passes,
   to close the stale-CI race (commit pushed after last CI run).

Both venues run the same `scripts/regression-gate.sh` script.

## Invariant author contract

A file under `scripts/` is an invariant iff it matches the glob
`scripts/check-*.{sh,py}` (top level only — no recursion). To add a
new invariant:

1. Create `scripts/check-<your-invariant>.sh` (or `.py`).
2. Make it executable: `chmod +x scripts/check-<name>.sh`.
3. Follow the contract:

   | Concern | Contract |
   |---|---|
   | Exit code | `0` = preserved · `1` = broken · `≥ 2` = tool error (treated as broken) |
   | Stdout | One human-readable line per violation; ideally `path:line  reason` |
   | Stderr | Tool errors only |
   | Input | Runs from the repo root; assume merged-state tree is checked out |
   | Allowlist | Optional `scripts/<name>.allow` next to the script; format `<path-or-pattern> # <reason>`; load it inside your script |
   | Timeout | 30 seconds per invariant (configurable via `REGRESSION_GATE_TIMEOUT` env var for local debugging) |

4. **Test it locally** by running:

   ```bash
   bash scripts/regression-gate.sh
   ```

   Your new check should appear in the summary.

### warn-* — invariants in a soak period

If your invariant is not yet ready to be gating (e.g., legacy data
still violates it during a migration window), name it
`scripts/warn-<name>.{sh,py}` instead. Warn scripts follow the same
contract but the aggregator **ignores** their exit code — their
output surfaces but never fails the gate. Promote `warn-` → `check-`
by renaming the file when ready.

## Overriding a false positive

The gate does not have a per-PR override mechanism. False positives
are addressed by editing the invariant's allowlist file:

1. Open `scripts/<invariant-name>.allow`. Create it if it doesn't
   exist.
2. Add a line: `<path-or-pattern> # <reason>`. The reason is
   mandatory — invariant linters typically reject entries missing
   it.
3. Commit the allowlist change in the same PR (or a follow-up PR).
4. Push; CI reruns; the gate passes.

This is by design — bypasses are auditable in git history and force
a written reason.

## When the gate fails in clawta-pr-lifecycle

The `clawta-pr-lifecycle` cron reruns the gate against the PR's
merge ref. Two failure modes:

| Aggregator exit | clawta action |
|---|---|
| `1` — invariant broken | Posts a structured comment on the PR; reassigns the kanban ticket to `red`. PR stays open; no merge. |
| `≥ 2` — tool error (aggregator crashed, worktree fetch failed, timeout) | Posts a comment flagging "could not evaluate"; **does NOT reassign**. Operator must investigate (it's a tooling issue, not a PR-content issue). |

## One-time operator setup (branch protection)

The `Regression gate` step rides inside the existing `test` job in
`.github/workflows/ci.yml`. Make sure your branch protection rule
on `main` includes:

1. Repo settings → Branches → `main` rule → Required status checks.
2. Add or confirm `test` is in the required list.
3. Enable **"Require branches to be up to date before merging"** so
   that a stale PR re-runs CI (and the gate) against the latest
   `main`.

This is a one-time settings change, not a code change.

## Inaugural registry (Day-0)

When this runbook ships, the registry contains two invariants:

| Script | Enforces |
|---|---|
| `scripts/check-spec-frontmatter.py` | Every spec under `docs/superpowers/specs/**` has a valid 6-field YAML front-matter block. See `docs/runbooks/spec-lifecycle.md`. |
| `scripts/check-spec-index-sync.sh` | `docs/superpowers/specs/INDEX.md` matches what `regen-spec-index.py` produces. |

The five already-specced audit-response invariants (governance
boundary, kanban-isolation, scripts-classification,
worktree-status, worktree-naming) join the registry as their
implementation PRs land — no coordination required; the aggregator
picks them up by filename match.

## Followups (file as kanban tickets if needed)

- Promote `regression-gate` to its own CI job if check-list
  legibility on the PR page becomes a debugging cost.
- Reimplement the aggregator as `chitin-kernel regression-gate` if
  a dashboard or audit surface needs typed metadata.
- Chain-event logging of gate decisions for the analyzer cron in
  `docs/superpowers/specs/2026-05-12-chitin-dashboard.md` Slice 5.
