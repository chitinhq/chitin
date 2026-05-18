# Architecture audit — 2026-05-17

## Executive summary

- Chitin’s swarm layer is the current architectural hotspot: `swarm/` has 88 files but 84 commits in 7 days, suggesting rapid operational growth and likely abstraction drift.
- Core concerns are spread widely across surfaces: `gate`, `hook`, `kanban`, `hermes`, and `openclaw` appear in Go, Python, apps, scripts, and swarm, which raises coupling and AI-navigation cost.
- Governance ownership looks blurry: `go/` still appears central, but `apps/`, `scripts/`, and `swarm/` all contain governance/gate/router language, increasing risk of invariant drift.
- Documentation is active, not stale: 77 docs commits in 7 days is good, but it may also indicate the system needs too much explanatory scaffolding to operate safely.
- The 73 local worktrees/in-flight branches are a process smell: even if justified by swarm scale, they increase merge, review, and architectural consistency risk.

## Findings

### High

- **Swarm is becoming a parallel control plane.** `swarm/` has 84 commits in 7 days and dominates `kanban`, `clawta`, and `openclaw` references, which suggests operational logic may be accumulating outside the core kernel.
- **Governance invariants appear duplicated across surfaces.** `gate`, `governance`, `router`, and `hook` occur in `go/`, `apps/`, `scripts/`, `python/`, and `swarm/`; without a clear authority boundary, policy behavior can diverge between runtime, UI, automation, and scripts.
- **Worktree/branch volume is an architectural risk amplifier.** 73 local worktrees across many prefixes implies many concurrent edits touching related surfaces, increasing the chance of inconsistent fixes and stale assumptions.

### Medium

- **Go remains the largest kernel surface but not the only policy surface.** `go/` has 260 files and high counts for `gate`, `hook`, `router`, and `hermes`, yet scripts/swarm/apps also reference the same concepts heavily.
- **Scripts may be carrying production semantics.** `scripts/` has 66 files and 32 commits in 7 days, with notable overlap on `gate`, `hermes`, `hook`, and `governance`; this often means workflows are executable architecture but not modeled as first-class modules.
- **Docs are compensating for operational complexity.** `docs/` has 193 files and 77 recent commits, including dispatch pipeline/runbook/spec-kit updates; that is healthy if docs track stable design, risky if they are patching over unclear boundaries.
- **Hermes/OpenClaw integration spans too many places.** `hermes` appears in 47 Go files, 37 swarm files, 20 scripts, and 12 Python files; `openclaw` appears heavily in swarm and Go, implying adapter/integration concepts may be leaking.

### Low

- **Naming/layout likely costs agents time.** Surfaces named `apps`, `libs`, `scripts`, `swarm`, `python`, and `go` describe implementation location more than ownership boundaries, making AI navigation harder without strong conventions.
- **Recent commits show good operational tightening.** Spec-kit audit, dependency unblock veto, lifecycle installer, fallback headroom, and kernel redeploy tests indicate active hardening, not neglect.

## Top 3-5 actionable findings

### 1. Define the governance authority boundary

**Problem.** Governance-related concepts are spread across `go/`, `apps/`, `scripts/`, `python/`, and `swarm/`: `gate`, `governance`, `router`, and `hook` all show broad keyword overlap. The risk is invariant drift where `chitin.yaml`, Go kernel behavior, app UI assumptions, and swarm automation each encode slightly different policy semantics.

**Suggested next step.** Write and enforce a short “governance ownership map” that names the source of truth for gate/router/hook semantics and marks other surfaces as adapters, views, or tests.

**Effort.** M

### 2. Stop swarm from becoming an unbounded second kernel

**Problem.** `swarm/` has only 88 files but 84 commits in 7 days, plus dominant counts for `kanban`, `clawta`, `openclaw`, and `hermes`. That velocity suggests swarm is absorbing orchestration, lifecycle, dispatch, audit, and watchdog behavior faster than the core architecture can stabilize.

**Suggested next step.** Split `swarm/` into explicit subdomains such as dispatch, lifecycle, watchdog, spec-kit, and reporting, then require each to document which core invariants it may read or mutate.

**Effort.** M

### 3. Promote durable scripts into owned modules or delete them

**Problem.** `scripts/` has 66 files, 32 recent commits, and meaningful overlap with `gate`, `hook`, `governance`, `hermes`, and `kanban`. This is a classic sign that scripts are carrying production semantics without the same API boundaries, tests, or ownership as Go/Python/libs code.

**Suggested next step.** Audit the top-used scripts and classify each as one of: one-shot migration, maintained CLI, test fixture, or obsolete; move maintained CLIs under an owned package/module.

**Effort.** M

### 4. Reduce AI-navigation cost with architectural indexing

**Problem.** The repo has large active surfaces: 260 Go files, 102 Python files, 112 app files, 84 lib files, 88 swarm files, and 193 docs files. The same concepts recur across many locations, so agents will waste context discovering whether a file is source of truth, adapter glue, UI, or automation.

**Suggested next step.** Add a machine-readable architecture index that maps major concepts — gate, router, hook, kanban, hermes, openclaw, clawta — to owning directories and “do not edit here for semantics” locations.

**Effort.** S

### 5. Put branch/worktree sprawl under architectural review

**Problem.** 73 local worktrees across many prefixes means many concurrent changes can touch overlapping architectural concerns before they converge. The risk is not just merge conflict; it is inconsistent local fixes to the same governance/swarm/spec-kit boundary.

**Suggested next step.** Add a weekly or automated “worktree architecture sweep” that groups open branches by touched surface and flags duplicate concern areas before PR review.

**Effort.** S

## Trends since previous audit

First audit — no prior baseline.
