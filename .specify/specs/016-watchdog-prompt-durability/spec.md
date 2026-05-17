# Feature Specification: Watchdog prompt durability — tracked source for cron prompts

**Feature Branch**: `fix/watchdog-prompt-durability`

**Created**: 2026-05-17

**Status**: Draft

**Ticket**: `t_580bc20e` (related — broader durability concern from retro item)

**Parent spec**: `001-agent-bus/spec.md` (dispatch infrastructure)

---

## Problem

Constitution §6 requires tracked source over local patches. The board-watchdog is the most consequential untracked "code" in the system — its prompt lives in `~/.hermes/cron/jobs.json`, not in git. When we fixed Bug 1 (watchdog re-blocking tickets with valid specs), the fix was a prompt change in an external JSON file with no version control, no review trail, and no drift detection.

If the prompt changes outside git (operator edit, accidental overwrite, cron job migration), the system silently degrades: tickets get re-blocked, false positives accumulate, and there's no diff to review.

## User Scenarios

1. **Operator installs watchdog prompt** (P0): After a clean checkout or cron migration, the operator runs `bash swarm/bin/install-board-watchdog-prompt.sh` to deploy the tracked prompt. Acceptance: the watchdog cron job's prompt field exactly matches `swarm/prompts/board-watchdog.md`.

2. **Operator verifies drift** (P0): CI or an operator runs `bash swarm/bin/install-board-watchdog-prompt.sh --verify`. If the installed prompt has drifted from the tracked file, the command exits 1 with a diff. Acceptance: `--verify` exits 0 when prompt matches, exits 1 when it doesn't, and prints the drift to stderr.

3. **Agent reviews prompt change** (P1): A developer edits `swarm/prompts/board-watchdog.md`, opens a PR, and another agent cross-reviews the diff per §10.1. Acceptance: the prompt file is tracked in git, appears in PR diffs, and the installer propagates it.

## Acceptance Scenarios

```gherkin
Scenario: Fresh install deploys tracked prompt
  Given a clean cron jobs.json with a watchdog job
  When the operator runs `bash swarm/bin/install-board-watchdog-prompt.sh`
  Then the watchdog job's prompt field matches swarm/prompts/board-watchdog.md
  And subsequent --verify exits 0

Scenario: Verify detects drift
  Given an installed prompt that differs from swarm/prompts/board-watchdog.md
  When the operator runs `bash swarm/bin/install-board-watchdog-prompt.sh --verify`
  Then the command exits 1
  And the diff between tracked and installed is printed to stderr

Scenario: Idempotent install
  Given an already-installed prompt matching the tracked file
  When the operator runs `bash swarm/bin/install-board-watchdog-prompt.sh`
  Then the prompt is unchanged
  And --verify still exits 0

Scenario: Tracked prompt change is reviewable
  Given a developer edits swarm/prompts/board-watchdog.md
  When they open a PR with the change
  Then the diff appears in the PR and is cross-reviewable per §10.1
```

## Implementation Notes

1. **Tracked file**: `swarm/prompts/board-watchdog.md` contains the canonical prompt text, tracked in git, reviewable in PRs.

2. **Installer**: `swarm/bin/install-board-watchdog-prompt.sh` is an idempotent shell script that reads the tracked file and writes it to the cron job's prompt field via `python3` JSON manipulation. Same pattern as `install-hermes-clawta-bridge.sh`.

3. **Verify flag**: `--verify` reads both the tracked file and the installed prompt, normalizes trailing whitespace, and diffs them. Exit 0 on match, exit 1 on drift, diff on stderr.

4. **No new dependencies**: Uses `python3` (already required for bridge/poller), `bash`, standard Unix tools.

5. **CI hook candidate**: `--verify` can be added to CI or a pre-commit hook to catch drift before merge.

## Out of Scope

- Migrating other cron job prompts to tracked files (future work, same pattern).
- Changing the watchdog's behavior (that's spec 008, already merged).
- Auto-reinstall on cron job changes (the installer is manual/idempotent for now).