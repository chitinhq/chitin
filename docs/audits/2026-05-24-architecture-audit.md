# Architecture audit — 2026-05-24

## Executive summary

- **Kanban is everywhere**: 120 files across 5 surfaces (60 in swarm/ alone). Spec 087 is retiring the kanban substrate, but until that lands, the codebase carries a dead-weight coupling layer spanning every surface.
- **Worktree bloat is an operational hazard**: 150 local worktrees, many stale. This is the single highest-ROI cleanup target — it slows every `git` operation and obscures active work.
- **Agent-name hardcoding is pervasive**: "hermes" (163 refs) and "clawta" (126 refs) are scattered across go/, python/, scripts/, and swarm/. Any agent rename or addition requires cross-surface surgery.
- **Gate/hook/governance overlap across go/ and scripts/**: "gate" (146 go + 23 scripts), "hook" (83 go + 21 scripts), "governance" (45 go + 12 scripts) suggest duplicated enforcement paths rather than a single substrate.
- **swarm/ is a commit furnace outpacing its surface size**: 90 commits in 7 days on 153 files — a churn rate of 0.59 commits/file/week, 3× any other surface. This signals either a hot path or an unstable one.

## Findings

### High

- **Kanban coupling is cross-cutting and soon-to-be-orphaned**: 120 files reference "kanban" across 5 surfaces. Spec 087 (merged: "retire the kanban substrate") is recent, meaning ~60 swarm/ files and ~27 go/ files now reference a substrate being decommissioned. Deletion lag will leave dead imports and stale tests.
- **150 local worktrees are a latent hazard**: The in-flight branches list shows broad, shallow prefixes (agent names, spec numbers, sw-NNN tickets) with no visible reaping discipline. Active `git` operations scan all worktrees; stale ones obscure what's live and risk accidental base-checkout edits.
- **Agent identity hardcoding (hermes 163, clawta 126)**: Agent names appear as string literals across go/, python/, apps/, scripts/, swarm/. Governance, routing, and dispatch logic all branch on agent identity strings rather than registered agent cards or config. Adding or renaming an agent requires edits in every surface.

### Medium

- **Gate enforcement split across go/ (146) and scripts/ (23)**: Two separate enforcement paths for the same invariant class. scripts/ likely wraps go/ but the overlap count suggests independent logic rather than thin shelling out.
- **Hook surface dispersion (83 go + 21 scripts + 23 apps + 11 python + 16 swarm = 154)**: Hook lifecycle is a core abstraction with no clear domicile. Changes to hook semantics require coordinated edits across 5 surfaces.
- **swarm/ churn rate is unsustainably high relative to other surfaces**: 90 commits / 153 files in 7 days vs go/ at 47/569. If swarm/ is orchestration glue, this churn signals unstable interfaces. If it's agent logic, it's in the wrong surface.
- **libs/ stagnation (4 commits / 91 files)**: Shared infrastructure that sees barely any touch. Either it's mature and stable (good) or it's ossified and no one wants to touch it (bad). The low churn alongside high cross-surface dependency deserves investigation.

### Low

- **docs/ is 222 files but no keyword overlap signal**: Clean separation, expected. Worth maintaining as the repo grows.
- **python/ quiet (9 commits / 127 files)**: Likely stable analysis/tooling surface. Low risk, low priority.

## Top 3-5 actionable findings

### 1. Reap stale worktrees en masse

**Problem.** 150 local worktrees exist, many with prefixes from resolved tickets (sw-009, sw-011, spec 068–074). The heartbeat itself flagged "327 git worktrees" on-disk. Every `git` operation scans these; stale worktrees obscure active branches and risk accidental edits on the primary checkout.

**Suggested next step.** Enumerate worktrees whose HEAD branches are merged to main or whose ticket is closed, then `git worktree remove` them in a batch script.

**Effort.** S

### 2. Purge kanban references post-spec-087

**Problem.** Spec 087 merged to retire the kanban substrate, but 120 files still reference "kanban" across go/ (27), python/ (11), apps/ (10), scripts/ (12), and swarm/ (60). These are now dead coupling points that will diverge from actual dispatch state.

**Suggested next step.** After spec 087's removal pass lands, run a cross-surface `rg -l kanban` sweep and delete or redirect each hit to the replacement dispatch substrate.

**Effort.** M

### 3. Consolidate agent identity resolution behind agent-card registry

**Problem.** Agent names "hermes" (163 refs) and "clawta" (126 refs) are hardcoded as string literals across go/, python/, apps/, scripts/, and swarm/. Governance, routing, and dispatch all switch on these strings. Adding a new agent or renaming one currently requires a cross-repo find-and-replace.

**Suggested next step.** Introduce a single agent-card registry (likely in go/ or libs/) that every surface queries by UUID or short-name alias. Replace string-literal branching with registry lookups.

**Effort.** L

### 4. Merge scripts/ gate/hook logic into go/ thin wrappers or eliminate

**Problem.** scripts/ has 23 "gate" and 21 "hook" files that duplicate enforcement paths in go/ (146 and 83 respectively). Two enforcement paths for the same invariants create drift risk — a gate added in go/ but not scripts/ is a silent bypass.

**Suggested next step.** Audit scripts/ gate/hook files; convert each to a thin wrapper calling the go/ binary, or delete if the go/ path already handles the case natively.

**Effort.** M

### 5. Investigate swarm/ churn and formalize its scope

**Problem.** swarm/ sees 90 commits in 7 days on 153 files (0.59 commits/file/week). This is 3× the next-highest churn rate and suggests either an unstable interface layer or glue code that should live closer to its consumers in go/ or apps/.

**Suggested next step.** Classify swarm/ files into: (a) orchestration glue that wraps go/ binaries, (b) agent-specific logic that should live in an agent surface, (c) shared tooling. Migrate (b) and (c) out; keep swarm/ as thin orchestration only.

**Effort.** M

## Trends since previous audit

Prior audit: 2026-05-17-architecture-audit.md (referenced but content not provided in facts). Based on the 7-day delta:

- **Continuity**: Spec-driven development continues (specs 087–104 in recent commits). This is healthy discipline.
- **Regression**: Worktree count has grown from a known problem (flagged May 20) to 150+ by May 24 with no visible reaping. The cleanup directive exists but hasn't executed.
- **New risk**: Spec 087 (kanban retirement) merged but kanban references are still pervasive. The window between "retired" and "purged" is where dead-code bugs accumulate.
- **Improvement**: Recent chore commits (#948 repo cleanliness, #962 closing DispatchMachineReviewer stub) show active housekeeping, but it's not keeping pace with the worktree and cross-surface coupling accumulation.
