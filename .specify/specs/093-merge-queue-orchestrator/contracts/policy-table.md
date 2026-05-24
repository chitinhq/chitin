# Contract: Policy Table v1.0.0

**Producer**: this spec; mirrored in `go/orchestrator/activities/merge/policy/policy_table.go`
**Consumer**: `ClassifyPR` activity; `WaitForChecks` activity; `PRMergeWorkflow.classifyAndGate` step

**Canonical source**: This file. The Go file mirrors it with a header comment referencing this path (R-CANON).

---

## Version

`1.0.0` — initial table. Bumped semver-style on any change to the taxonomy, the per-class policy, or the classification rules.

---

## Classification algorithm

Given a `PRSnapshot` (with `Files`, `BranchName`, `Title`, `LinesChanged`), apply rules in this order — first match wins:

1. **Governance trigger paths** (FR-005): if any file in `Files` matches a path in `governance.GovernanceTriggerPaths`, return `governance` regardless of any other criterion. The governance class cannot be relaxed; an `expected_class` override that disagrees is rejected by the CLI.

2. **Live-fix branch prefix**: if `BranchName` starts with any prefix in `live-fix.BranchPrefixes`, return `live-fix`. Title patterns alone do not trigger live-fix.

3. **Research-docs path allowlist**: if every file in `Files` matches some glob in `research-docs.PathAllowlist`, return `research-docs`.

4. **Spec-only path allowlist**: if every file in `Files` matches some glob in `spec-only.PathAllowlist`, return `spec-only`.

5. **Bookkeeping**: if `LinesChanged <= bookkeeping.MaxLinesChanged` AND every file matches a glob in `bookkeeping.PathAllowlist`, return `bookkeeping`.

6. **Impl (catch-all)**: otherwise, return `impl`.

Rules 2–5 are mutually exclusive when their conditions hold simultaneously because each is checked in priority order. If two classes could apply (e.g., a PR that's all docs but uses a `fix/` branch), the higher-priority rule wins (in this example, `live-fix`).

---

## The six classes

### `governance`

Constitution amendments, strategy documents, governance ratification.

| Field | Value |
|-------|-------|
| `RequiredChecks` | `["test", "Analyze (go)", "Analyze (javascript-typescript)", "Analyze (python)", "Analyze (actions)", "CodeQL", "GitGuardian Security Checks"]` (full suite) |
| `RequiresApproval` | **true** (always; cannot be overridden by submitter identity) |
| `MaxLinesChanged` | 0 (unbounded) |
| `BranchPrefixes` | `[]` (any branch is fine) |
| `PathAllowlist` | `[]` (no restriction) |
| `PathDenylist` | `[]` |
| `GovernanceTriggerPaths` | `[".specify/memory/constitution.md"]` (v1.0.0) — see note below |

**Operator note**: PR #925 (constitution §7) would land in this class. The `approve` signal is mandatory regardless of green CI.

**v1.0.0 scoping note on `docs/strategy/`**: An earlier draft included `docs/strategy/**` as a governance trigger. Removed in v1.0.0 because the existing `docs/strategy/` directory is heterogeneous (audits, decision docs, roadmap, diagrams, operating models) — not all are governance-class. Future convention: `docs/strategy/policy/**` will auto-promote in v1.1.0+. Existing files are not migrated in v1; only the directory split is adopted as a forward rule. Operators authoring new policy-grade strategy docs should create them under `docs/strategy/policy/` from now on.

---

### `live-fix`

Production fixes that need to land fast. Full check suite still required — speed comes from no human-approval gate, not from skipped checks.

| Field | Value |
|-------|-------|
| `RequiredChecks` | full suite (same as governance) |
| `RequiresApproval` | false |
| `MaxLinesChanged` | 0 (unbounded) |
| `BranchPrefixes` | `["fix/", "hotfix/"]` |
| `PathAllowlist` | `[]` |
| `PathDenylist` | `[".specify/memory/constitution.md"]` (would have promoted to governance) |
| `GovernanceTriggerPaths` | (inherited via classification rule 1) |

**Operator note**: PR #923 (lockdown loop fix) would land in this class — branch `feat/091-fix-clawta-lockdown-loop` does start with `feat/`, NOT `fix/`, so for v1 this gets `impl` classification. **This is a known gap** to refine in a v1.1 amendment (consider also matching titles like `fix(...)` or `feat(...) -fix-...`). For v1, the more conservative `impl` class is the right default — full checks + no approval gate, which matches the manual treatment #923 just received.

---

### `spec-only`

PRs that only modify spec-kit artifacts. CI runs (the repo's `test` and analyze jobs are cheap on spec-only changes), but full implementation check suite isn't meaningful.

| Field | Value |
|-------|-------|
| `RequiredChecks` | `["test", "GitGuardian Security Checks"]` (test passes trivially on spec-only diffs; secret-scan still mandatory) |
| `RequiresApproval` | false |
| `MaxLinesChanged` | 0 (unbounded — spec text can be long) |
| `BranchPrefixes` | `[]` |
| `PathAllowlist` | `[".specify/specs/**", ".specify/feature.json", "CLAUDE.md"]` |
| `PathDenylist` | `[".specify/memory/constitution.md", ".specify/templates/**"]` (would promote elsewhere) |
| `GovernanceTriggerPaths` | (inherited) |

**Operator note**: PRs #919, #920, #921, #922, #927 would land in this class.

---

### `research-docs`

Strategy docs (when promoted to governance via rule 1, they go there instead), research reports, runbooks, architecture decisions outside `docs/strategy/`.

| Field | Value |
|-------|-------|
| `RequiredChecks` | `["GitGuardian Security Checks"]` (only secret-scan; markdown rendering is human-verified) |
| `RequiresApproval` | false |
| `MaxLinesChanged` | 0 |
| `BranchPrefixes` | `[]` |
| `PathAllowlist` | `["docs/**"]` |
| `PathDenylist` | `[]` (v1.0.0) — `docs/strategy/policy/**` will move here in v1.1.0+ |
| `GovernanceTriggerPaths` | (inherited) |

**Operator note**: PR #926 lands in this class. Its file `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` is research/reference material, not policy-grade. v1.0.0 deliberately does NOT auto-promote `docs/strategy/` paths to governance — the directory is heterogeneous today (audits, decision docs, roadmap, diagrams). Future v1.1.0+ amendment will add `docs/strategy/policy/**` as the gov trigger; new policy-grade docs should live under that subdirectory.

---

### `impl`

Code changes. The default catch-all class.

| Field | Value |
|-------|-------|
| `RequiredChecks` | full suite (same as governance) |
| `RequiresApproval` | false |
| `MaxLinesChanged` | 0 |
| `BranchPrefixes` | `[]` |
| `PathAllowlist` | `[]` |
| `PathDenylist` | `[]` |
| `GovernanceTriggerPaths` | (inherited) |

**Operator note**: any PR touching code in `go/`, `apps/`, `python/`, `swarm/`, etc. with no qualifying live-fix branch prefix lands here.

---

### `bookkeeping`

Tiny PRs that update spec status/pointer files (e.g., marking tasks complete, INDEX entries). CI required but checks are minimal because the diff is mechanical.

| Field | Value |
|-------|-------|
| `RequiredChecks` | `["test", "GitGuardian Security Checks"]` |
| `RequiresApproval` | false |
| `MaxLinesChanged` | **50** (FR-classification spec) |
| `BranchPrefixes` | `[]` |
| `PathAllowlist` | `[".specify/specs/**/tasks.md", ".specify/specs/INDEX.md", ".specify/specs/**/plan.md"]` |
| `PathDenylist` | `[".specify/memory/constitution.md"]` |
| `GovernanceTriggerPaths` | (inherited) |

**Operator note**: PR #924 (21-line task-status update on spec 068) would land here.

---

## Class summary table

| Class | Required checks | Approval gate | Triggered by |
|-------|----------------|---------------|--------------|
| `governance` | full suite | **YES** | files in `.specify/memory/constitution.md` or `docs/strategy/**` |
| `live-fix` | full suite | no | branch prefix `fix/` or `hotfix/` |
| `spec-only` | test + secrets | no | all files in `.specify/specs/**`, `.specify/feature.json`, `CLAUDE.md` |
| `research-docs` | secrets only | no | all files in `docs/**` (excluding `docs/strategy/**`) |
| `impl` | full suite | no | catch-all |
| `bookkeeping` | test + secrets | no | ≤ 50 lines, all files in `tasks.md`/`INDEX.md`/`plan.md` |

---

## Auto-classification dry-run for current backlog (SC-001 fixture)

Running the v1.0.0 classifier against the 7 queued PRs:

| PR | Files touched (representative) | Auto-class | Why |
|----|--------------------------------|------------|-----|
| #926 | `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` | research-docs | Path matches `docs/**` allowlist; `docs/strategy/**` NOT a v1.0.0 gov trigger |
| #927 | `.specify/specs/092-no-driver-bypass-invariant/spec.md`, `.specify/feature.json`, `.specify/specs/INDEX.md` | spec-only | All paths in allowlist |
| #919 | `.specify/specs/087-retire-kanban-substrate/spec.md` etc. | spec-only | All paths in allowlist |
| #920 | `.specify/specs/088-cull-mention-listeners/spec.md` etc. | spec-only | All paths in allowlist |
| #921 | `.specify/specs/089-retire-pre-v2-skills/spec.md` etc. | spec-only | All paths in allowlist |
| #922 | `.specify/specs/090-discord-channel-ingress/spec.md` etc. | spec-only | All paths in allowlist |
| #924 | `.specify/specs/068-icarus-bench-loop-revival/tasks.md` | bookkeeping | 21 lines ≤ 50, file in `tasks.md` allowlist |

**Result**:
- 1 research-docs (#926) → auto-merge with secrets-scan only
- 5 spec-only (#927, #919–#922) → auto-merge with test + secrets
- 1 bookkeeping (#924) → auto-merge with test + secrets
- 0 governance entries; no `approve` signal required for the current 7-PR backlog.

---

## Future amendments

These are NOT in v1.0.0 — listed here so the next policy-table change has context:

- **Title-based live-fix detection**: extend rule 2 to also match PRs whose title contains `fix(...)` even if branch isn't `fix/`-prefixed. Would auto-classify #923 as `live-fix` instead of `impl`. Requires title parsing rules.
- **Per-PR check override**: allow operator to add a check name to `RequiredChecks` for a specific submission. Out of scope for v1 per spec Assumption "per-class checks fixed."
- **Path-glob nesting in research-docs**: split into `research-docs-low` (just secrets) vs `research-docs-rendered` (also requires markdown lint when one exists). Out of scope until markdown-lint CI exists.
- **`migration` class**: PRs that are pure file moves with no semantic change. Would require diff-content analysis, not just path analysis. Out of scope until classification refinement spec.

Each future change requires a table-version bump and the corresponding policy_table.go update.
