---
status: open
owner: claude-code
kanban: t_ac6da121
implementation_pr: null
superseded_by: null
effective_from: '2026-05-13'
effective_to: null
---

# Spec: regression-gate — registry of invariants run pre-merge

Date: 2026-05-13
Source: SDLC architecture diagram (`http://100.115.89.9:8888/chitin-sdlc-architecture.html`) — the dashed arrow from `Clawta PR Owner` to `PR Merged`.
Sister spec: `t_6dbe137e` (invariant-gate on triage → ready + PR-open test-diff coverage).
Author: claude-code (operator-controlled, spec writer)

## Problem

The SDLC architecture diagram highlights a gap: between `Clawta PR
Owner` and `PR Merged` is a dashed arrow with no intermediate step.
Today's auto-merge gates check only shape:

1. PR open + non-draft
2. Mergeable (no conflicts)
3. CI checks passing
4. Ticket maps to in_progress
5. Clawta APPROVE on current head

CI runs unit tests, but unit tests only cover **lines that exist** —
they're blind to invariants the change might violate elsewhere in
the system. This is the same class of bug as the #514 → t_d44e4648
→ #557 chain (a missing-case regression that line-coverage couldn't
see). `t_6dbe137e` addresses the *pre-code* half (triage → ready
requires a named invariant + boundary list); this spec addresses
the *pre-merge* half.

A parallel symptom: five specs landed today (`2026-05-13`'s
audit-response batch + the May 12 hardening pack) each name a
`scripts/check-*.sh` invariant linter. There is no shared runner,
no shared discovery, no shared override convention. Each linter
ships independently and is wired into CI as its own step. As the
count grows the orchestration overhead grows linearly. The runner
this spec defines is the natural home for that consolidation.

## Invariant (the claim)

> Every PR auto-merged by `clawta-swarm-pr-owner` has been verified
> against every registered invariant in `scripts/check-*.{sh,py}`,
> running on the PR's merged-state tree, with all invariants
> returning exit 0.

A violation is any path by which a PR can reach `main` without
running the registered invariant set. This includes:

- A CI step that's skipped or removed.
- A `clawta-swarm-pr-owner` decision that doesn't re-evaluate the
  gate on the PR's current head.
- A new invariant file added to `scripts/` without `check-*` (or
  `warn-*`) prefix, missing from discovery.

The gate is **hard-block** (operator decision, see Decisions
below): a failing invariant flips the PR to needs-action; auto-merge
is refused; the operator must either fix the PR or amend the
invariant's allowlist file.

## Decisions

Captured from the brainstorming pass on 2026-05-13:

| Decision | Choice | Rationale |
|---|---|---|
| Gate strength | Hard block | Auto-merge refusal is the only signal strong enough to keep regressions out of `main` reliably. False positives are addressed by allowlist edits, not bypass. |
| Invariant representation | Executable per invariant (`scripts/check-<name>.{sh,py}`) | Matches today's pattern in 5 already-specced linters. No schema to fight; no shared runtime to maintain. |
| Execution venue | Both — CI step + clawta-pr-owner local rerun | CI is the canonical source-of-truth (visible to humans, blocks the GitHub merge button). clawta-pr-owner's rerun closes the stale-CI race. |
| Override mechanism | Per-invariant allowlist file (`scripts/<name>.allow`) — no per-PR `/bypass` comments | Allowlist edits are auditable in git history and force a written reason; per-PR bypass is too easy to habituate. |
| Runner architecture | Pure shell (`scripts/regression-gate.sh`) | Smallest viable scope. Promote to Go (`chitin-kernel regression-gate`) only if a dashboard or audit surface forces it. |

## Scope of this spec

**In scope:**

1. `scripts/regression-gate.sh` — the aggregator runner.
2. The **invariant script contract** — exit codes, stdout
   convention, allowlist convention, discovery rules.
3. CI wiring in `.github/workflows/ci.yml` (Day-0).
4. `clawta-swarm-pr-owner` extension (the 6th check, with the
   `REQUEST_CHANGES` vs. `COMMENT` split on tool-error).
5. `docs/runbooks/regression-gate.md` — invariant author guide +
   override workflow + one-time branch-protection setup.
6. Day-0 inaugural registry: adopt the existing
   `scripts/check-spec-frontmatter.py` (from PR #581); add
   `scripts/check-spec-index-sync.sh` as a thin wrapper around
   `regen-spec-index.py --check` so the existing index drift check
   joins the registry uniformly.
7. Remove the PR #581 standalone CI steps for those two checks
   (single chokepoint; subsumed by the gate).

**Out of scope (followups):**

- Retrofitting any non-spec'd script into the `check-*` namespace.
  Adding new invariants is independent per-PR work.
- Per-PR override (`/bypass-invariant <name>`) — operator decision
  was allowlist-only.
- Promoting `regression-gate` to a separate named CI job. It rides
  inside the existing `test` job for now. Split later if check-list
  legibility is a real cost.
- A Go reimplementation (`chitin-kernel regression-gate`). Only if
  the dashboard surface (`docs/superpowers/specs/2026-05-12-chitin-dashboard.md`
  Slice 5/6) forces it.
- Chain-event logging of gate decisions for Argus consumption. File
  as a Slice 5 (analyzer cron) input ticket.

## Architecture

```
scripts/
  regression-gate.sh            # the aggregator (this spec)
  check-<name>.sh|.py           # one invariant per file (per-invariant authors)
  <name>.allow                  # optional exemption list, owned by its invariant
  warn-<name>.sh|.py            # informational; runs but never fails
.github/workflows/ci.yml        # adds a `Regression gate` step in the `test` job
swarm/bin/clawta-swarm-pr-owner # extended with the 6th check
docs/runbooks/regression-gate.md
```

### Invariant script contract

A file at `scripts/check-*.{sh,py}` is a registry entry. Contract:

| Concern | Contract |
|---|---|
| Exit code | `0` = preserved · `1` = broken · `2+` = tool error (treated as broken; surfaced separately in clawta-side handling) |
| Stdout | One human-readable diagnostic line per violation (ideally `path:line  why`); summary at the end is encouraged but not required |
| Stderr | Tool errors only; CI suppresses unless the script exits `≥ 2` |
| Input | Runs from the repo root against the PR's merged-state tree, checked out at `refs/pull/<N>/merge`. No diff parsing required for most invariants. |
| Allowlist | Optional `scripts/<name>.allow` next to the script. Format: `<path-or-pattern> # <reason>`. Loaded by the script itself; no shared schema. |
| Discovery | Matches `scripts/check-*.{sh,py}` at top level only (no recursion). Anything else is not a gate. |

A `scripts/warn-*.{sh,py}` follows the same contract but the
aggregator ignores its exit code. Use this for invariants in a
soak period (e.g., the worktree-naming check from
`2026-05-13-worktree-status-report.md`'s spec — warn-only until
legacy worktrees drain).

### Aggregator (`scripts/regression-gate.sh`)

```bash
#!/usr/bin/env bash
# regression-gate — run every registered invariant against the current tree.
# Exit 0 iff every check-*.{sh,py} passes; exit 1 on any failure.
# warn-*.{sh,py} run informationally and never affect the exit code.
#
# Spec: docs/superpowers/specs/2026-05-13-regression-gate.md
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mapfile -t gates < <(find scripts -maxdepth 1 -type f \
    \( -name 'check-*.sh' -o -name 'check-*.py' \) | sort)
mapfile -t warns < <(find scripts -maxdepth 1 -type f \
    \( -name 'warn-*.sh'  -o -name 'warn-*.py'  \) | sort)

PER_INVARIANT_TIMEOUT="${REGRESSION_GATE_TIMEOUT:-30}"

run_one() {  # path → prints header, runs with timeout, returns exit code
    local s="$1"
    echo "── $s ──"
    case "$s" in
        *.py) timeout "$PER_INVARIANT_TIMEOUT" python3 "$s" ;;
        *)    timeout "$PER_INVARIANT_TIMEOUT" bash    "$s" ;;
    esac
}

declare -A rc
fails=0
for s in "${gates[@]}"; do
    run_one "$s"; r=$?
    rc["$s"]=$r
    [ "$r" -eq 0 ] || fails=$((fails+1))
done
for s in "${warns[@]}"; do run_one "$s" || true; done

echo
echo "═══ regression-gate summary ═══"
for s in "${gates[@]}"; do
    [ "${rc[$s]}" -eq 0 ] && tag=PASS || tag=FAIL
    printf "  %-5s  %s\n" "$tag" "$s"
done

if [ "$fails" -gt 0 ]; then
    echo
    echo "$fails/${#gates[@]} invariant(s) broken."
    echo "False positive? Add an entry to scripts/<name>.allow with a # reason."
    echo "Spec: docs/superpowers/specs/2026-05-13-regression-gate.md"
    exit 1
fi
echo "All ${#gates[@]} invariants preserved."
exit 0
```

Notes:

- `set -uo pipefail`, **not** `-e` — we want to keep running every
  invariant after a failure and report aggregate, not short-circuit.
- Per-invariant timeout of 30s (`REGRESSION_GATE_TIMEOUT` env
  override for local debugging). Kills hung scripts; `timeout`'s
  exit-124-on-kill is treated as exit ≥ 2 (tool error).
- Empty registry → exits 0 with `All 0 invariants preserved.` This
  is the Day-0 safe state if the spec lands ahead of any
  invariant.

### CI wiring

In `.github/workflows/ci.yml`, append a step to the existing `test`
job (alongside the linters added by PRs #580/#581):

```yaml
- name: Regression gate
  run: bash scripts/regression-gate.sh
```

`actions/checkout` on `pull_request` events fetches
`refs/pull/<N>/merge` by default — the merged-state tree the
invariants assume.

**Branch protection** (one-time operator action, documented in the
runbook): the existing `test` check stays the required-and-up-to-date
gate; the new step rides inside it. No new check name to register.

Tradeoff: a failing invariant reports as a `test` failure in
GitHub's UI, not as its own named check. We accept the legibility
cost for now; promote to a separate job if the cost becomes real.

### clawta-pr-owner integration

Extend the auto-merge check list in `swarm/bin/clawta-swarm-pr-owner`:

```python
def auto_merge_ok(pr) -> bool:
    if not (open_non_draft(pr) and mergeable(pr) and ci_passing(pr)
            and ticket_in_progress(pr) and clawta_approve(pr)):
        return False

    # Belt-and-suspenders: rerun against PR's merge-state tree to close
    # the stale-CI race (commit pushed after last CI run).
    with checkout_pr_merge_state(pr.number) as workdir:
        rc, out = run(["bash", "scripts/regression-gate.sh"],
                      cwd=workdir, timeout_s=120)
    if rc == 0:
        return True

    summary = tail(out, 40)
    if rc == 1:
        # Real invariant broken.
        post_request_changes(pr, f"regression-gate failed:\n{summary}")
        assign_ticket(pr.kanban_id, "red",
                      reason="regression-gate broke; needs operator")
    else:
        # Tool error (aggregator crash / checkout fail / etc.).
        # Fail-closed but flag for human investigation rather than
        # auto-escalating the ticket.
        post_comment(pr, f"regression-gate could not evaluate (exit {rc}):\n{summary}")
    return False
```

The `REQUEST_CHANGES` vs. `COMMENT` split is the key reliability
property. Without it, a single buggy invariant script would flip
every PR to escalation.

## Data flow

```
operator opens PR
     │
     ▼
CI runs `test` job — including `Regression gate` step
     │
     ├── all invariants pass → check `test` = SUCCESS
     │                          │
     │                          ▼
     │                       clawta-pr-owner sees (a-e) ✓, reruns gate locally
     │                          │
     │                          ├── PASS → auto-merge
     │                          └── FAIL → REQUEST_CHANGES + reassign ticket to red
     │
     └── any invariant fails → check `test` = FAIL
                                │
                                ▼
                             branch protection blocks the merge button
                                │
                                ▼
                             clawta-pr-owner sees (c) ✗ → does not attempt merge
                                (no REQUEST_CHANGES from clawta — CI is the binding signal)
                                │
                                ▼
                             operator pushes fix OR amends `<name>.allow` + reason
                                │
                                ▼
                             CI reruns → resume normal flow
```

## Error handling

| Failure | Behaviour |
|---|---|
| Invariant exits 1 (broken) | Aggregator exits 1; CI fails; clawta posts REQUEST_CHANGES + reassigns ticket |
| Invariant exits ≥ 2 (tool error) | Aggregator exits 1; **clawta posts COMMENT** (not REQUEST_CHANGES); ticket NOT reassigned — operator investigates |
| Invariant hangs > 30s | Killed by `timeout`; treated as exit ≥ 2 |
| Aggregator itself crashes | CI step fails (fail-closed); clawta posts COMMENT |
| Empty registry | Aggregator exits 0 with `All 0 invariants preserved.` Day-0 safe state. |
| `<name>.allow` syntax error | The invariant script's responsibility to surface (per-script; not aggregator concern) |

## Migration

### Day-0 — this PR

1. `scripts/regression-gate.sh` written, executable, empty-registry safe.
2. `scripts/check-spec-index-sync.sh` written as a thin wrapper:
   ```bash
   #!/usr/bin/env bash
   exec python3 "$(dirname "$0")/regen-spec-index.py" --check
   ```
3. CI step added in `.github/workflows/ci.yml`.
4. PR #581's two standalone CI lines for `check-spec-frontmatter.py`
   and `regen-spec-index.py --check` are **removed**. Both are now
   picked up by `regression-gate`. Single chokepoint; no double-runs.
5. `clawta-swarm-pr-owner` extended with the 6th check.
6. `docs/runbooks/regression-gate.md` committed.

**Inaugural registry on Day-0:**

| Script | Source | Effect |
|---|---|---|
| `scripts/check-spec-frontmatter.py` | already in tree from PR #581 | enforces YAML front-matter on every spec |
| `scripts/check-spec-index-sync.sh` | new in this PR | enforces `INDEX.md` is in sync with the spec set |

### Day-N — the five audit-response specs land

The five already-specced invariant scripts
(`go-only-governance-authority`, `isolate-swarm-kanban-mutations`,
`scripts-classification`, the worktree-status linter, and the
worktree-naming `warn-*` script) ship as independent PRs by their
assigned workers. They land into the contract this spec defines.
Each writes a `scripts/check-<name>.{sh,py}` (or `warn-<name>.sh`)
and is picked up by the aggregator on the next CI run.

No coordination is required beyond each implementation following
the contract section above. That's the central property of
executable-per-invariant: registry growth is zero-coordination.

### Operator action (one-time, documented in runbook)

Branch protection on `main`: ensure the `test` check is in
"required status checks" and "require branches to be up to date
before merging" is on. This step is **outside the PR** — a repo
settings change the operator performs once.

## Testing

Unit tests for the aggregator (added in this PR as either `bats`
cases or a small Python harness — implementation choice deferred
to the plan):

| Behaviour | Test |
|---|---|
| Empty registry | No `check-*` files → exit 0 with `All 0 invariants preserved.` |
| Single PASS | One stub `check-foo.sh` returning 0 → aggregator exit 0 |
| Single FAIL | One stub returning 1 → exit 1; summary contains `FAIL  scripts/check-foo.sh` |
| Mixed | PASS + FAIL → exit 1; both listed; **PASS still ran** (no short-circuit) |
| Tool error | `check-crash.sh` exits 2 → aggregator exit 1 |
| Timeout | `check-hang.sh` sleeps 31s → killed by `timeout 30`; counts as exit ≥ 2 |
| Warn-only | `warn-drift.sh` exits 1 → aggregator's exit code unchanged; output still surfaces |
| Allowlist discovery | `governance-boundary.allow` exists but does not match `check-*` glob → not invoked |
| Naming hygiene | `check-foo.txt` (wrong suffix) → not invoked |

Integration tests:

1. **Fixture PR fails CI.** Once `check-spec-frontmatter.py` is the
   inaugural invariant, a branch that adds a spec without
   front-matter triggers the gate's first real failure; the `test`
   check goes red; branch protection blocks merge.
2. **Fixture PR passes CI.** A clean docs-only PR runs the gate,
   sees PASS for both inaugural invariants, CI green.
3. **clawta-pr-owner REQUEST_CHANGES.** Manual simulation: a PR
   matching (a-e) but breaking an invariant locally → clawta
   posts `REQUEST_CHANGES` with the aggregator tail; kanban ticket
   reassigned to `red`.
4. **clawta-pr-owner COMMENT on tool error.** Simulate
   `scripts/regression-gate.sh` returning exit 2 → clawta posts
   `COMMENT`; does NOT reassign; does NOT post REQUEST_CHANGES.

## Verification

- `scripts/regression-gate.sh` exists, exits 0 on empty registry,
  discovers `check-*.{sh,py}` and `warn-*.{sh,py}` at
  `scripts/` top level only.
- `scripts/check-spec-index-sync.sh` exists and wraps
  `regen-spec-index.py --check`.
- `.github/workflows/ci.yml` has a single `Regression gate` step
  and the prior PR-#581 standalone steps are gone.
- `swarm/bin/clawta-swarm-pr-owner` has the 6th check with the
  REQUEST_CHANGES / COMMENT split.
- `docs/runbooks/regression-gate.md` exists; covers contract,
  override workflow, and one-time branch-protection setup.
- All aggregator behaviour tests pass.

## Done-condition

- [ ] `scripts/regression-gate.sh` exists and supports empty
      registry.
- [ ] `scripts/check-spec-index-sync.sh` exists and wraps the
      existing INDEX drift check.
- [ ] CI step `Regression gate` wired in
      `.github/workflows/ci.yml`; PR-#581 redundant steps removed.
- [ ] `clawta-swarm-pr-owner` extended with the regression-gate
      check, including the `REQUEST_CHANGES` / `COMMENT` split on
      tool-error.
- [ ] `docs/runbooks/regression-gate.md` exists, linked from the
      runbook index.
- [ ] All boundary tests pass.
- [ ] Operator updates branch protection (action documented in the
      runbook; not done in the PR).

## Effort

S — approximately one day.

- Aggregator + tests: ~2 hr
- CI wiring + redundant-step removal: ~1 hr
- `clawta-swarm-pr-owner` extension (need to read the existing
  structure, integrate, test): ~2-3 hr
- Runbook: ~1 hr
- Integration fixture PRs: ~1-2 hr

## Followups (file as kanban tickets on PR merge)

1. **Promote `regression-gate` to its own CI job** for legibility,
   if the in-`test`-step location becomes a debugging cost.
2. **`chitin-kernel regression-gate` Go subcommand** for read-side
   surfaces (`list`, `explain <name>`, `audit --since <ts>`) — only
   if a real consumer (dashboard, argus) needs it.
3. **Chain-event logging of gate decisions** so the analyzer cron
   (`2026-05-12-chitin-dashboard.md` Slice 5) can mine
   regression-gate trends.
4. **Adopt the May-12 hardening specs** (`no-gov-self-mod-bypass`,
   `no-commit-to-protected-branch`) as registry entries once their
   implementation lands.

## Open questions

- **Granularity of the merged-state checkout in clawta-pr-owner**:
  shallow vs. full? Implementation-time call. Default to shallow
  (`git fetch --depth=1 origin pull/<N>/merge`) for speed; expand
  if any invariant turns out to need history.
- **What happens if two PRs are both queued for auto-merge and one
  amends the registry?** The second PR's gate runs against the
  pre-amendment registry on its own CI cycle; when CI re-fires after
  the first PR merges, the second PR sees the new invariant. This
  is the standard `require branches to be up to date` behaviour —
  the operator's one-time branch-protection setup covers it. Worth
  noting in the runbook.
