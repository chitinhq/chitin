---
status: draft
owner: clawta
kanban: t_79e8d3bf
implementation_pr: null
superseded_by: null
effective_from: '2026-05-14'
effective_to: null
---

# Governed gstack methodology adoption for Chitin swarm

Date: 2026-05-14
Status: draft proposal
Author: Clawta
Source audited: `garrytan/gstack` at shallow clone time, repo metadata 2026-05-15T03:53:20Z

## Goal

Adopt the useful process patterns from `garrytan/gstack` without weakening the Chitin/Hermes/Clawta governance model.

The target is **methodology import**, not runtime replacement:

- Chitin remains the execution governance boundary.
- Hermes remains the kanban/source-of-truth and priority owner.
- Clawta remains the Control Tower and governed dispatch operator.
- gstack patterns become optional planning/review/QA/security lenses applied to selected swarm tickets and PRs.

## What gstack is, in one sentence

`gstack` is an opinionated software-factory workflow pack for Claude Code: product office-hours, CEO/engineering/design reviews, autoplan, code review, QA, security audit, release, deploy, canary, and browser tooling, with OpenClaw integration guidance that explicitly frames gstack as a methodology source rather than an OpenClaw daemon.

## Audited high-signal patterns

### 1. Dispatch tiers

`docs/OPENCLAW.md` defines five tiers:

| Tier | gstack intent | Chitin adaptation |
| --- | --- | --- |
| Simple | one-file edits, typos | existing direct worker dispatch |
| Medium | multi-file obvious changes with lightweight planning | add lightweight plan preamble for larger tickets |
| Heavy | named skill such as `/review`, `/qa`, `/cso` | explicit `methodology_profile` on ticket/dispatch |
| Full | feature/project objective | Hermes plans/specifies, then Clawta dispatches through Chitin |
| Plan | planning only, no implementation | Control Tower planning tickets/spec PRs |

Useful rule to steal: **choose methodology at spawn time based on task shape**, not by letting workers self-upgrade into a large process mid-run.

### 2. gstack-lite planning discipline

The OpenClaw doc's `gstack-lite` section is small and valuable:

1. Read every relevant file before modifying.
2. Write a 5-line plan: what, why, files, test, risk.
3. Resolve ambiguity using decision principles.
4. Self-review before reporting done.
5. Completion report: shipped changes, decisions, uncertainty.

Chitin adaptation: add this as a bounded prompt prefix for medium/high-risk swarm tickets. Keep it short to avoid consuming the whole worker budget.

### 3. Review army / specialist fan-out

`review/SKILL.md` has a useful structure:

- detect stack, diff size, scope, and test framework;
- always run testing + maintainability specialists above a changed-line threshold;
- conditionally run security, performance, data-migration, API-contract, and design specialists;
- parse specialist findings as JSON lines;
- dedupe by fingerprint;
- apply confidence gates;
- optionally run a red-team pass when the diff is large or any critical issue appears.

Chitin adaptation: implement this as **review metadata**, not as autonomous branch mutation. The Clawta reviewer can produce structured findings and the fix dispatcher can consume them through existing PR feedback loops.

### 4. Completion audit against plan

The review workflow audits plan items against `git diff` and classifies them as `DONE`, `PARTIAL`, `NOT DONE`, `CHANGED`, or `UNVERIFIABLE`.

This is directly useful for swarm work because many worker PRs pass tests while quietly missing planned deliverables.

Chitin adaptation: when a ticket has a structured body/spec, lifecycle should eventually include a **ticket-completion audit** before marking done or auto-merging high-priority work.

### 5. Security audit phases

`cso/SKILL.md` is valuable as a checklist source. Its phases include:

- architecture/trust-boundary mental model;
- attack surface census;
- secrets archaeology;
- dependency supply chain;
- CI/CD pipeline security;
- infrastructure shadow surface;
- webhook/integration audit.

Chitin adaptation: add an optional `security-review` methodology profile for tickets touching auth, CI/CD, secrets, external callbacks, package management, or execution policy.

### 6. QA as test-fix-verify loop

`qa/SKILL.md` treats QA as browser testing plus bug fixing plus regression test generation, with severity tiers and before/after evidence.

Chitin adaptation: useful for UI/web repos, but currently Chitin should not add browser daemon complexity to the core swarm. Prefer a future optional lane that invokes browser-capable workers only when the ticket has a URL or frontend scope.

### 7. Investigation discipline

The `/investigate` and shared preamble patterns emphasize: no fix without root-cause evidence, stop after repeated failed fixes, and report `DONE`, `DONE_WITH_CONCERNS`, `BLOCKED`, or `NEEDS_CONTEXT`.

Chitin adaptation: strengthen worker finalization prompts and failure comments so silent confusion becomes structured `block_reason` instead of vague red escalation.

## Anti-patterns to reject

Do **not** import these into Chitin core:

1. **Bypassing Chitin via Claude Code sessions.** gstack's OpenClaw doc says to spawn Claude Code directly. For this swarm, leaf work must still route through Chitin governance and lifecycle.
2. **Interactive AskUserQuestion gates inside background workers.** Swarm workers should not pause on chat UX. Ambiguity becomes a ticket comment/block reason.
3. **Local telemetry/artifact sync as a controller dependency.** gstack's local analytics/learnings are useful for solo coding, but Clawta/Hermes already have kanban, chain, PR, and Discord telemetry planes.
4. **Browser daemon in the default path.** Useful for QA, too much surface area for baseline autonomous controller work.
5. **Full gstack skill porting.** Chitin needs compact, governed prompt modules and invariants, not another large runtime inside the worker path.

## Proposed Chitin implementation slices

### Slice A — Methodology profile field and prompt map

Add a ticket/dispatch concept named `methodology_profile` with values:

- `none` — default existing behavior.
- `lite-plan` — gstack-lite 5-line plan + self-review.
- `plan-only` — no implementation; produce plan/spec artifact.
- `review-army` — structured specialist review of a PR/diff.
- `security-review` — cso-inspired security audit pass.
- `qa-browser` — future browser-backed QA pass.
- `completion-audit` — compare ticket/spec items to diff before lifecycle done.

Initial implementation can be heuristic-only in Clawta tooling if Hermes has no schema field yet. Prefer eventually storing it in Hermes DB to avoid free-text parsing.

### Slice B — Lite-plan worker preamble

For `priority >= 80`, multi-file scopes, or tickets containing `invariants_and_boundaries`, prefix worker prompts with a short bounded checklist:

```text
Before editing, produce a compact plan:
- What will change
- Why it is safe
- Files likely touched
- Smallest validation command
- Main risk/ambiguity
After editing, self-review against the ticket and report uncertainty explicitly.
Do not ask interactive questions; if blocked, leave a ticket comment and set a structured block_reason.
```

Acceptance:

- Prompt stays under a small fixed budget.
- Worker final comments include plan/test/risk lines.
- No process argv leakage regression.

### Slice C — Review-army as structured Clawta reviewer mode

Extend `clawta-pr-reviewer` with optional specialist passes selected by diff scope:

- always-on above threshold: testing, maintainability;
- conditional: security, performance, data migration, API contract, design;
- red-team if large diff or critical finding.

Output remains a normal GitHub review comment with current-head marker, but include machine-readable finding markers for fix dispatcher consumption.

Acceptance:

- Specialist findings are deduped by fingerprint.
- Findings below confidence threshold are suppressed or appendix-only.
- Fix dispatcher remains head-aware and idempotent.
- Lifecycle still requires current-head approval before merge.

### Slice D — Ticket completion audit

For high-priority or explicitly planned work, compare ticket body/spec checklist against the PR diff and classify deliverables:

- `DONE`
- `PARTIAL`
- `NOT DONE`
- `CHANGED`
- `UNVERIFIABLE`

Acceptance:

- `NOT DONE` high-impact items block auto-merge with `block_reason=needs-fix` or `operator-decision`.
- `UNVERIFIABLE` items do not auto-pass silently; dashboard surfaces them.
- Completion audit references concrete evidence: files, tests, or external check needed.

### Slice E — Security-review profile

Trigger on tickets/PRs touching:

- auth/session/permissions;
- shell/exec/governance policy;
- CI/CD workflows;
- secrets/env/config;
- package/dependency management;
- webhook/callback integrations.

Acceptance:

- Security findings require confidence and exploit scenario.
- Known false-positive classes are documented.
- No secret values are copied into comments or logs.

## Suggested routing heuristics

| Signal | Profile |
| --- | --- |
| `priority >= 90` and implementation ticket | `lite-plan` + `completion-audit` |
| touches `.github/workflows`, `scripts/check-*`, governance policy, or shell exec | `security-review` |
| PR diff > 200 changed lines | `review-army` |
| PR review has repeated fix loops | `review-army` + red-team |
| ticket is planning/design/spec only | `plan-only` |
| ticket includes URL/front-end QA scope | `qa-browser` later |

## Invariants

1. No gstack profile may bypass Chitin `gate -> chain -> signals`.
2. No gstack profile may merge, push, or mark done outside existing lifecycle gates.
3. No worker may ask interactive gstack questions in autonomous background mode.
4. Every blocked methodology outcome must map to a Hermes `block_reason`.
5. Methodology profiles must be visible in dashboard/telemetry before they influence routing automatically.
6. Browser/QA tooling is opt-in and never part of baseline poller execution.

## Recommended first PR

Implement **Slice A + B only**:

- add a compact `lite-plan` prompt fragment to swarm dispatch;
- add a `methodology_profile` annotation in dispatch comments/logs;
- route only high-priority multi-file tickets through it;
- add tests that the prompt fragment is present and that workers are still dispatched through Chitin.

This gives immediate quality improvement with low blast radius. Review-army and completion-audit are higher leverage but should come after profile telemetry exists.

## Open questions

1. Should `methodology_profile` live in Hermes schema, Chitin dispatch metadata, or both?
2. What changed-line threshold should trigger review-army? gstack uses 50+ lines for specialists and 200+ for red-team; Chitin may want higher thresholds due to swarm volume.
3. Should completion-audit block auto-merge for all P90+ tickets or only tickets with explicit checklist/spec sections?
4. Which lane should own future browser QA: Clawta direct, Hermes bridge, or a separate browser-capable worker profile?
