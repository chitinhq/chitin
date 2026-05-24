# Quickstart: Verify the Kanban Substrate Retirement

**Feature**: 087-retire-kanban-substrate
**Audience**: the reviewer landing the implementation PR(s), and the operator pulling main
post-merge.

This is the post-merge verification recipe. The CI gate (see step 8) catches regressions;
this quickstart is what a human runs to confirm the retirement actually landed and the
platform still works.

## Pre-flight

```bash
# Ensure you're on the merged-state main (or whichever branch carries the cull).
git checkout main
git pull
```

## 1. Surface is gone (SC-001 / FR-011)

```bash
# The grep gate — should return zero hits.
grep -rli 'kanban\|hermes.*board' apps/ go/ libs/ services/ swarm/ \
  | grep -v 'docs/decisions\|\.specify/specs' \
  | wc -l
# Expected: 0
```

If non-zero, the cull is incomplete — the matching files are the leak.

## 2. Directories are gone

```bash
test ! -e services/swarm-kanban-mcp && echo PASS || echo FAIL
test ! -e go/execution-kernel/internal/kanban && echo PASS || echo FAIL
test ! -e go/execution-kernel/internal/boardconfig && echo PASS || echo FAIL
test ! -e swarm/workflows/kanban-dispatch.lobster && echo PASS || echo FAIL
test ! -e swarm/bin/board_resolver.py && echo PASS || echo FAIL
test ! -e swarm/bin/board-watchdog-bounded.py && echo PASS || echo FAIL
test ! -e swarm/bin/clawta-swarm-board-watcher && echo PASS || echo FAIL
test ! -e apps/chitin-console/src/app/pages/queue.page.ts && echo PASS || echo FAIL
test ! -e apps/chitin-console/src/app/pages/tickets.page.ts && echo PASS || echo FAIL
```

## 3. Kernel subcommands return "unknown" cleanly (SC-006 + contract proof)

```bash
chitin-kernel kanban 2>&1 | grep -qiE 'unknown|invalid|not.recognized|usage'
chitin-kernel board-config 2>&1 | grep -qiE 'unknown|invalid|not.recognized|usage'
```

Both should exit non-zero (informational) and print a usage error.

## 4. Kernel preserves protocol-agnostic MCP recognition (FR-009)

```bash
# These files MUST still exist and contain the action normalization.
grep -lE 'mcp__|ActMCPCall' \
  go/execution-kernel/internal/driver/claudecode/normalize.go \
  go/execution-kernel/internal/driver/codex/normalize.go \
  go/execution-kernel/internal/driver/hermes/normalize.go \
| wc -l
# Expected: 3
```

## 5. Orchestrator dispatch is unaffected (SC-003)

```bash
# Build the orchestrator.
( cd go/orchestrator && go build ./... )

# Run the orchestrator unit tests.
( cd go/orchestrator && go test ./... )
```

If the orchestrator builds and tests pass, the dispatch substrate is whole. End-to-end:
the operator (or CI) starts `chitin-orchestrator` against a running `temporal-dev` and
observes child WorkUnitWorkflow dispatches.

## 6. Console builds + runs without kanban routes (SC-002 / FR-005)

```bash
pnpm nx build chitin-console
pnpm nx build chitin-console-api

# Optional smoke: start both, navigate to /sessions, confirm the page renders.
# The kanban routes (/queue, /tickets) should resolve to not-found.
```

## 7. Operator data is preserved (SC-005 / FR-008)

```bash
# Operator-owned files MUST still exist on the operator's box.
ls -la ~/.chitin/kanban/ 2>/dev/null     # expect: directory + DBs unchanged
ls -la ~/.hermes/kanban/ 2>/dev/null     # expect: directory + DBs unchanged
# The retirement diff contains NO `rm`, `unlink`, or `os.remove` against these paths —
# verify by inspection of the merged PR.
```

## 8. CI gate (added in the final partition)

A `scripts/check-no-kanban.sh` (added by partition 7) runs in CI:

```bash
#!/usr/bin/env bash
set -euo pipefail
hits=$(grep -rli 'kanban\|hermes.*board' apps/ go/ libs/ services/ swarm/ \
  | grep -v 'docs/decisions\|\.specify/specs' \
  | wc -l)
if [ "$hits" -ne 0 ]; then
  echo "FAIL: $hits files re-introduced kanban references"
  exit 1
fi
echo "PASS: kanban-free"
```

Wired into a CI workflow as a required check on every PR. Any future PR that adds
kanban references fails this gate. SC-001 is enforced for the future, not just the
one merge.

## 9. Operator-side cleanup (post-merge, on each operator box)

Crons referencing retired subcommands need removing:

```bash
crontab -l | grep -vE 'chitin-kernel (kanban|board-config)|clawta-swarm-board-watcher|board-watchdog-bounded|kanban-dispatch' | crontab -
```

(Idempotent; safe to re-run.) The retirement does not autonomously edit the operator's
crontab — operator decides their own schedule.

## Failure modes the verification catches

| Symptom | Likely cause |
|---|---|
| Step 1 returns non-zero | A partial-edit missed a kanban reference. Check the file in the grep output. |
| Step 3 returns "kanban-aware" output | The kernel CLI partial-edit on `main.go:61-63` didn't strip the cases. |
| Step 5 fails to build | An import in the orchestrator wasn't comment-only — re-check D1 from research.md. |
| Step 6 build fails | A console-UI partial-edit left a dangling reference to a deleted module / route / API method. |
| Step 7 shows DB files missing | A migration step accidentally deleted operator data — **this MUST NEVER happen**; the PR is rolled back. |
| Step 8 fails in CI | Future drift; the gate works as designed. |

## What's NOT verified by this quickstart

- External MCP callers' configurations — operator-side, out of scope per spec
  Assumptions.
- The `hermes kanban` CLI wrapper (lives outside chitin) — wraps `chitin-kernel kanban`
  and will fail with the same "unknown" error post-retirement; hermes-repo cleanup is
  separate.
- Visual confirmation that the operator's daily workflow remains satisfied (User Story 2
  / SC-004) — this is a manual operator walk-through, not a script.
