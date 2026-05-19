# Feature Specification: Drift Guard — multi-repo hardcode elimination + CI enforcer

**Feature Branch**: `feat/drift-guard`

**Created**: 2026-05-16

**Status**: shipped (ff32594, #687)

**Refs**: t_12568dca

## User Scenarios & Testing *(mandatory)*

### User Story 1 — PR lifecycle scripts are board-aware (Priority: P1)

A swarm actor working on the readybench board runs `clawta-pr-lifecycle` and it targets `wjcmurphy/bench-devs-platform` instead of `chitinhq/chitin`. Every PR lifecycle script (`clawta-pr-lifecycle`, `clawta-pr-ci-triage`, `clawta-pr-fix-dispatcher`, `clawta-pr-reviewer`) resolves repo URL and workspace root from board config, not from hardcoded literals.

**Why this priority**: These scripts are the most-frequently-invoked swarm operations. If they hardcode `chitinhq/chitin`, every dispatch against a non-chitin board silently targets the wrong repo.

**Independent Test**: Set `BOARD=readybench` in the environment; run `clawta-pr-lifecycle`; verify it resolves `wjcmurphy/bench-devs-platform` from board config and never references `chitinhq/chitin` in its git operations.

**Acceptance Scenarios**:

1. **Given** board config maps `readybench` → repo `wjcmurphy/bench-devs-platform`, **When** `clawta-pr-lifecycle` runs with `--board readybench`, **Then** it opens a PR against `wjcmurphy/bench-devs-platform` (not `chitinhq/chitin`).
2. **Given** board config maps `chitin` → repo `chitinhq/chitin`, **When** `clawta-pr-lifecycle` runs with `--board chitin`, **Then** it opens a PR against `chitinhq/chitin` (existing behavior unchanged).
3. **Given** `--board` is not provided, **When** `clawta-pr-lifecycle` runs, **Then** it defaults to the `chitin` board (backward-compatible).

### User Story 2 — Audit and report scripts are board-aware (Priority: P2)

All audit and report scripts (`swarm-audit`, `clawta-swarm-daily-audit`, `clawta-swarm-pr-owner-cron`, `architecture-audit` + its installer, `clawta-report`) read repo/workspace from the shared helper. No script in this set contains a `chitinhq/chitin` or `/workspace/chitin` literal except in documented allowlist entries.

**Why this priority**: Audit scripts produce operator-visible reports. If they hardcode repo paths, the reports are silently wrong for non-chitin boards.

**Independent Test**: Run `swarm-audit --board readybench`; verify report references `bench-devs-platform` paths. Run `grep -r 'chitinhq/chitin' swarm/bin/architecture-audit` — it returns zero hits.

**Acceptance Scenarios**:

1. **Given** the readybench board, **When** `swarm-audit` runs, **Then** its output contains the readybench repo URL and workspace paths (not chitin ones).
2. **Given** a grep for `chitinhq/chitin` in `swarm/bin/architecture-audit`, **When** run, **Then** it returns zero hits (the literal only exists in `config.json` seed and drift guard allowlist).

### User Story 3 — Sentinels and watchdogs are board-aware (Priority: P3)

All sentinel/watchdog scripts (`clawta-blocked-escalator`, `clawta-stale-worker-watchdog`, `clawta-invariants`, `clawta-worker-failure-sentinel`) resolve board-specific paths from the shared helper.

**Why this priority**: Watchdogs monitor system health. A stale-worker sentinel that hardcodes chitin paths would miss stale workers on other boards.

**Independent Test**: Run `clawta-blocked-escalator --board readybench`; verify it queries the readybench kanban DB (not chitin's).

**Acceptance Scenarios**:

1. **Given** the readybench board, **When** `clawta-stale-worker-watchdog` runs, **Then** it scans readybench kanban tickets (not chitin tickets).
2. **Given** `clawta-blocked-escalator` runs without `--board`, **Then** it defaults to chitin (backward-compatible).

### User Story 4 — CI drift guard catches new hardcodes (Priority: P1)

A developer adds a new script to `swarm/bin/` that contains `chitinhq/chitin` as a literal string. On the next CI run, `scripts/check-no-chitin-hardcodes.sh` fails and the PR is blocked.

**Why this priority**: The drift guard is what makes all the other despecifications permanent. Without it, the next PR can silently re-introduce hardcodes.

**Independent Test**: Introduce a file `swarm/bin/test-script.sh` containing `git clone chitinhq/chitin`; run `scripts/check-no-chitin-hardcodes.sh`; verify it exits non-zero and reports the file.

**Acceptance Scenarios**:

1. **Given** a new file `swarm/bin/new-tool.sh` contains the string `chitinhq/chitin`, **When** `check-no-chitin-hardcodes.sh` runs in CI, **Then** it exits 1 and prints the offending file and line.
2. **Given** all files pass the allowlist, **When** `check-no-chitin-hardcodes.sh` runs, **Then** it exits 0.
3. **Given** `install-swarm.sh` contains `chitinhq/chitin` in its seed config, **When** the drift guard runs, **Then** it skips this file (allowlisted) and exits 0.

### User Story 5 — install-swarm.sh seeds per-board config (Priority: P2)

Running `install-swarm.sh` for the first time on a board creates that board's `config.json` with correct repo and workspace paths. Subsequent runs detect the existing config and skip re-seeding.

**Why this priority**: Without seeded config, all board-aware scripts fail on first run. This is the bootstrap path.

**Independent Test**: Delete `config.json` for a board; run `install-swarm.sh --board readybench`; verify the config contains `wjcmurphy/bench-devs-platform`.

**Acceptance Scenarios**:

1. **Given** no `config.json` for board `readybench`, **When** `install-swarm.sh --board readybench` runs, **Then** it creates `config.json` with `repo: wjcmurphy/bench-devs-platform`.
2. **Given** an existing `config.json` for board `readybench`, **When** `install-swarm.sh --board readybench` runs again, **Then** it skips re-seeding (idempotent).

## Edge Cases

- **Board config missing**: Scripts must fail with a clear error message, not fall back to chitin defaults silently.
- **Helper function unavailable on legacy installs**: Scripts that can't import the shared helper should log a deprecation warning and use chitin as default until the next install cycle.
- **Drift guard allowlist drift**: The allowlist itself must be version-controlled; changes to it trigger the same review path as the scripts it exempts.
- **Multi-board concurrent runs**: Two cron jobs for different boards running simultaneously must not clobber each other's config or kanban DB.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: All scripts in the PR lifecycle, audit, report, sentinel, and watchdog categories MUST accept `--board` and resolve repo/workspace from `chitin-kernel board-config <board>`.
- **FR-002**: `scripts/check-no-chitin-hardcodes.sh` MUST grep `swarm/bin/` and `scripts/` (excluding documented allowlist) for `chitinhq/chitin` and `/workspace/chitin` literals; exit 1 on any hit.
- **FR-003**: The allowlist for the drift guard MUST include: README/doc references, the `chitin/config.json` seed, and the drift guard script itself.
- **FR-004**: `install-swarm.sh` MUST seed `config.json` on first run and bail if config already exists (idempotent).
- **FR-005**: The drift guard MUST be wired into CI (`.github/workflows/`) on the same job as `check-swarm-deployed-sync.sh`.
- **FR-006**: When `--board` is omitted, scripts MUST default to `chitin` (backward-compatible).

### Key Entities

- **Board config**: JSON file per board (`~/.hermes/kanban/boards/<board>/config.json`) containing `repo`, `workspace_root`, `default_branch`.
- **Shared helper**: Bash/Python function library that wraps `chitin-kernel board-config` for scripts that don't have direct Go access.
- **Drift guard**: `scripts/check-no-chitin-hardcodes.sh` — CI-enforced grep with allowlist.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: `grep -r "chitinhq/chitin\|/workspace/chitin" swarm/bin/ scripts/` returns ONLY allowlisted hits (config seed, docs, drift guard itself).
- **SC-002**: All 14 affected scripts pass `--board readybench` integration test (correct repo targeted, zero chitin hardcodes).
- **SC-003**: Drift guard catches an injected `chitinhq/chitin` literal in CI with exit code 1 and a clear error message.
- **SC-004**: `install-swarm.sh --board readybench` is idempotent (second run is a no-op).

## Assumptions

- `chitin-kernel board-config <board>` is available and returns `repo`, `workspace_root`, `default_branch`.
- Shared helper (Slice A) is already merged. Drift guard depends on Slice C (dispatch proves runtime board scoping).
- The allowlist is small and stable. If it grows beyond 10 entries, it should be extracted to a separate `allowlist.txt` file.

## Phased Delivery

- **Phase 1 (this PR)**: Shared helper import + drift guard + CI wiring. Scripts unchanged but guard catches new violations.
- **Phase 2**: PR lifecycle scripts migrated to `--board` (4 scripts).
- **Phase 3**: Audit, report, and sentinel scripts migrated (9 scripts).
- **Phase 4**: `install-swarm.sh` seeding + final hardcoded-config removal.

Each phase ships as its own tracking-epic kanban ticket linked back to this spec.

## Out of scope

- Porting scripts to Go (separate followup per kanban-isolation spec).
- Changing board config schema (it already has the fields we need).
- Testing against boards beyond `chitin` and `readybench` (the board-config abstraction handles arbitrary boards, but CI only tests these two).