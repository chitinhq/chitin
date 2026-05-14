# Architecture audit — 2026-05-13

## Executive summary

- `go/` is the core and the hotspot: 215 files, 58 commits in 7d, and dominant ownership of `gate`, `router`, `hook`, and `governance`.
- `swarm/` is small but overloaded: 21 files, 22 commits, with high `kanban`, `clawta`, `hermes`, and `openclaw` overlap.
- Governance invariants look spread across `go/`, `apps/`, `scripts/`, `swarm`, and likely `chitin.yaml`; that is the top drift risk.
- `docs/` is nearly as active as code: 159 files and 54 commits in 7d, so spec lifecycle tracking matters.
- 25 local worktrees with mixed prefixes create real AI-navigation and coordination cost.

## Findings

### High

- `go/` owns most `gate` references, but `apps`, `scripts`, and `swarm` also contain policy-adjacent terms. This may be adapter glue, but the breadth is risky for a governance runtime.
- `swarm/` has 16 `kanban` hits in only 21 files, plus heavy `clawta`, `hermes`, and `openclaw` overlap. This suggests dispatch, state mutation, and messaging are tightly coupled.
- Recent commits are concentrated in safety-critical paths: protected commits, self-modification bypasses, inner-hop governance, router calibration, and kanban-flow defaults. That much invariant work needs cross-surface regression tests.

### Medium

- `scripts/` has 45 files, 18 commits, and overlap on `gate`, `router`, `governance`, `hook`, and `hermes`. Scripts may be accumulating runtime behavior rather than staying as glue.
- `apps/` has 15 `hook` hits and 8 `openclaw` hits across 34 files. The OpenClaw plugin surface is important enough to need a crisp adapter contract.
- `router` is concentrated in `go/` but appears across four other surfaces. Routing heuristics can easily fork between kernel logic and dispatch scripts.
- `docs/` has 54 commits in 7d. Without status metadata, agents may treat proposed or superseded specs as current architecture.

### Low

- `python/` is comparatively quiet with 57 files and 6 commits, but its `gate` and `router` hits still warrant a light boundary check.
- `libs/` has 72 files but no keyword-overlap facts here, so its architectural role is under-observed in this audit.

## Top 3-5 actionable findings

### 1. Make Go the only governance authority

**Problem.** `gate` appears in `go` 65 files, but also in `python`, `apps`, `scripts`, and `swarm`. `governance` also spans multiple surfaces. This is acceptable only if non-Go code translates calls into the kernel and never reimplements decisions.

**Suggested next step.** Add a CI boundary check that flags policy evaluation outside `go/`, with explicit allowlists for adapter invocation code.

**Effort.** M.

### 2. Isolate swarm kanban mutations

**Problem.** `swarm/` has 16 `kanban` hits in 21 files and heavy overlap with `clawta`, `hermes`, and `openclaw`. That points to a compact but tightly coupled orchestration layer. Recent dispatch and kanban-flow commits show this surface is still volatile.

**Suggested next step.** Define one canonical kanban mutation module or CLI wrapper and route all swarm workflows through it.

**Effort.** M.

### 3. Audit scripts for runtime logic

**Problem.** `scripts/` is active and policy-adjacent: 18 commits in 7d, with `gate`, `router`, `governance`, `hook`, and `hermes` hits. That increases navigation cost because important behavior may live outside typed package boundaries. It also makes test coverage less obvious.

**Suggested next step.** Classify every script as `ci`, `migration`, `operator`, or `runtime-critical`, then move runtime-critical scripts behind tests or package APIs.

**Effort.** M.

### 4. Add spec lifecycle metadata

**Problem.** `docs/` has 159 files and 54 commits in 7d, including active architecture/spec work. Fast docs churn is useful, but stale specs become dangerous instructions for agents. The provided facts do not show a clear proposed/implemented/superseded lifecycle.

**Suggested next step.** Create a spec index with status, owner, linked ticket, implementation PR, and superseded-by fields.

**Effort.** S.

### 5. Reduce worktree ambiguity

**Problem.** There are 25 local worktrees with mixed prefixes and messy branch labels. In a codebase with fast governance and swarm changes, this makes it easy for agents to inspect or patch the wrong branch. This is an AI-navigation smell, not just repo hygiene.

**Suggested next step.** Add a worktree status report mapping each worktree to ticket, branch, PR, owner, and last activity.

**Effort.** S.

## Trends since previous audit

First audit — no prior baseline.
