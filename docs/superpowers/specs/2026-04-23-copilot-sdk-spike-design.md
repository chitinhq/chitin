# Copilot SDK Feasibility Spike — Design

**Date:** 2026-04-23
**Status:** Design. Ready for user review, then dispatch to a subagent for 2-day execution per §Execution.
**Forcing function:** Tech talk "Copilot CLI Without Fear: Adding Safety Guardrails to AI-Generated Terminal Commands," Session 2, 19:00, in **14 days** (2026-05-07). 60-minute live-demo session on extending Copilot CLI with a chitin-backed safety layer that intercepts terminal commands before execution.
**Parent decisions (this session):**
- Kill hermes as a driver; chitin is governance around openclaw + Claude Code + Copilot CLI (see `memory/project_hermes_killed_chitin_as_governance.md`).
- Phase 0 = monitoring parity (A) + decision-log ingest (B). C (dashboards/ops) deferred.
- Copilot CLI gov integration has two candidate patterns: **X** (external wrap, hermes-style plugin), **Y** (in-kernel Go SDK embed), **Z** (ship X for talk, spec Y post-talk).
- Spike Y before committing to Y or Z.

## Preamble

The talk two weeks from now is a live demo of chitin intercepting Copilot CLI commands before execution. Two weeks is not a window to bet the stage on a brand-new SDK integration that may or may not work as advertised — but it is also short enough that committing to the safer external-wrap pattern (X) without testing the more elegant in-kernel pattern (Y) leaves architectural upside on the table. The resolution is a time-boxed feasibility spike: spend 2 days proving or disproving Y, then make a clean choice for the remaining 12 days.

The spike has one job — produce a go/no-go verdict with enough rigor that the fallback decision is not a guess. "No-go" is a legitimate outcome; the spike is not a commitment to ship Y.

## One-sentence invariant

The spike produces, within 2 calendar days, a go/no-go verdict on whether chitin can integrate Copilot CLI via the GitHub Copilot Go SDK with inline governance sufficient to ship a live demo in 12 more days — and if no-go, documents the blocker precisely enough to inform the Option X fallback without re-litigating the question.

## Scope

### In scope

- Install `github.com/github/copilot-sdk` Go SDK on this box (per <https://github.com/github/copilot-sdk/blob/main/docs/setup/index.md>).
- Enterprise auth flow completes end-to-end; minimal Go program authenticates and reaches Copilot.
- Drive one Copilot CLI invocation via the SDK from a minimal Go program in a throwaway directory under `scratch/copilot-spike/`.
- Observe the JSON-RPC protocol stream between the SDK and Copilot CLI — determine whether tool calls surface as parseable structured messages or as opaque blobs.
- Attempt pre-execution intercept of one tool call via whatever hook the SDK exposes.
- Invoke the existing `chitin-kernel gate evaluate` binary (already shipped in PR #45) from within the SDK intercept path for one synthetic allow-decision and one synthetic block-decision.
- Verify the decision is written to `~/.chitin/gov-decisions-<date>.jsonl` and is readable in the standard format.
- Produce a structured findings report (see §Deliverable).

### Out of scope

- Adding new action types to `go/execution-kernel/internal/gov/normalize.go`. Baseline action vocabulary from PR #45 is sufficient to prove the path.
- Tuning policy for demo scenarios (`terraform destroy`, `kubectl delete`, etc.). That is post-spike work whose shape depends on the go/no-go outcome.
- Multi-scenario demo rehearsal or polish. The spike proves one scenario works end-to-end, not that the demo is stage-ready.
- Full chain ingest / `chitin-kernel ingest-policy` work. The spike verifies the decision log is written; folding the decision log into the event chain is a separate Phase 0 workstream.
- Claude Code or OpenClaw integration. The structurally-identical plugins (2a/2b in the updated scope) wait until after the talk.
- Hermes retirement. Hermes stays dormant (no work dispatched) until after the talk.
- Production-grade error handling, retries, or UX polish in the spike code. The probe is a probe, not production.
- Readybench / bench-devs content. Chitin is OSS; the content-boundary rule applies (see `memory/feedback_chitin_oss_boundary.md`).

## Parameters (resolved in brainstorming)

| Parameter | Value |
|---|---|
| Time-box | 2 calendar days |
| Owner | Subagent dispatched via `Agent` tool on Day 1 start; operator in advisory mode |
| Shape | Standard (Approach 2 from brainstorming): end-to-end miniature on one scenario, ladder-style with early exit on first failed rung |
| Concurrent work | Operator works on talk narrative / slides / demo scenario list during the spike; does not block on subagent |

## Ladder

The spike proceeds as an ordered ladder of four rungs. Each rung has an explicit pass criterion. On the first failed rung, the spike exits immediately with a "no-go: blocked at rung N" verdict and does not attempt subsequent rungs. This is deliberate — an inability to clear an early rung is itself the finding; there is no value in pushing past it.

| Rung | Window | Test | Pass criterion |
|---|---|---|---|
| 1 | Day 1 AM | SDK install + Enterprise auth | A minimal Go program using `github.com/github/copilot-sdk` authenticates with the operator's Enterprise credentials and completes one round-trip to the Copilot backend without error |
| 2 | Day 1 PM | Run Copilot CLI via SDK, observe JSON-RPC | The SDK, driven by the same minimal Go program, spawns or communicates with Copilot CLI and produces an observable JSON-RPC message stream; at least one tool-call message is captured as structured data with a parseable tool name and parseable arguments |
| 3 | Day 2 AM | Intercept + synchronous block | The SDK exposes (directly or indirectly) a pre-execution hook that allows the Go program to inspect a tool call and synchronously refuse it, preventing Copilot CLI from executing the underlying command. One synthetic refusal is demonstrated end-to-end |
| 4 | Day 2 PM | End-to-end: gate + log | The intercept handler from Rung 3 shells out to `chitin-kernel gate evaluate`, honors the returned `Decision`, and verifies that the resulting line lands in `~/.chitin/gov-decisions-<today>.jsonl`. One allow path (shell command not in the baseline deny rules) and one block path (e.g., `rm -rf`) are both exercised |

### Rung-pass semantics

- Rung N cleared ⟹ the integration primitive tested at Rung N works on this machine, for this SDK version, with these credentials.
- Rung N cleared does NOT mean Rung N is production-ready. It means the building block exists.
- All four rungs cleared ⟹ "go" verdict, contingent on the Day 3+ estimate being achievable in the remaining 12 days.

## Kill conditions

The following observed states force a "no-go" verdict at the rung where they appear:

| Condition | Rung | Consequence |
|---|---|---|
| Enterprise auth requires an org-level permission the operator does not control (e.g., seat not provisioned, org-policy blocks SDK usage) | 1 | No-go: blocked at Rung 1 |
| SDK installation requires a platform the operator does not run (e.g., macOS-only binaries) | 1 | No-go: blocked at Rung 1 |
| JSON-RPC stream is encrypted, gzipped without a public parser, or exposes only high-level events (e.g., "session_started") without tool-call granularity | 2 | No-go: blocked at Rung 2 |
| Tool calls are observable but arguments are free-form prose that can't be normalized to a canonical `Action` | 2 | No-go: blocked at Rung 2 |
| SDK supports only post-hoc observation (read-only event stream), not pre-execution intercept | 3 | No-go: blocked at Rung 3 |
| SDK supports intercept but only via an async event bus that doesn't block the executing call | 3 | No-go: blocked at Rung 3 |
| Intercept works but there is no mechanism to signal "refuse this tool call" back to Copilot CLI | 3 | No-go: blocked at Rung 3 |
| `chitin-kernel gate evaluate` invocation from Go succeeds but its Decision cannot be honored by the SDK (e.g., SDK doesn't accept synchronous refusal) | 4 | No-go: blocked at Rung 4 |
| Decision log write fails, cannot locate log directory, or file permissions block creation | 4 | Documented blocker in the findings report (not an automatic no-go — this is a wiring issue, probably fixable, but flag it) |

### Soft blockers (do NOT force no-go, but must be documented)

- SDK auth process requires manual one-time steps (e.g., browser-based OAuth) — document, proceed.
- SDK version on this box differs from what's documented online — note version used.
- Rate limits, token costs, or similar operational constraints observed during the spike — note them for the Day 3+ plan.

## Deliverable

End-of-Day-2 output: a markdown findings report committed at `docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md` with these sections:

1. **Verdict** — one of: `go (Option Y)`, `no-go, fall back to X via Z`. Single sentence. No hedging.
2. **Rung results** — for each of the four rungs: cleared / failed + evidence (log excerpt, code snippet, error message). Explicit so the verdict is audit-able.
3. **Y build estimate** — if verdict is `go`, a 12-day milestone plan:
   - Days 3-5: full integration + policy additions
   - Days 6-10: end-to-end demo scenarios ready
   - Days 11-13: rehearsal + polish
   - Day 14: talk
   - Flag any risk areas that would push milestones.
4. **Blockers observed** — especially any blocker that would also affect Option X (external-wrap fallback). A blocker in Copilot's tool-call surface, not in the SDK, would bite X too.
5. **Recommendation** — `Y` or `Z`, with a one-sentence rationale that references the specific rung outcomes, not general vibes.
6. **Artifacts committed** — paths to minimal proof-of-concept code under `scratch/copilot-spike/` (ignore-listed from any release), one short README explaining how to re-run the probe.

## Post-spike paths

### If verdict is `go`

Immediately brainstorm + write the full Copilot CLI governance v1 spec (Y-based, SDK-embedded). The spec targets **2026-05-07 as a live-demo shipping date.** The brainstorming session reuses the context from this spike's findings report as its starting point. Do not start this work until the findings report is committed.

### If verdict is `no-go, fall back to X via Z`

Immediately brainstorm + write the Copilot CLI governance v1 spec (X-based, hermes-plugin port pattern). Same target date. The SDK path becomes the closing "where this is going" slide of the talk, not the demo. Do not start this work until the findings report is committed.

### Either way

- The findings report is committed on the spike branch and PR'd into main.
- The spec for v1 is a fresh brainstorming session, not a continuation — the decision gate is clean.
- Operator's parallel work (talk narrative, slide deck, demo-scenario list) continues regardless of verdict.

## Execution

### How the subagent is dispatched

After user review of this spec, the operator triggers the execution handoff. Per the brainstorming skill flow, next step is `superpowers:writing-plans` to produce a task-by-task plan for the subagent. The plan:

1. Per-rung task with explicit inputs, expected artifacts, and fallback action on failure.
2. Time checkpoints (end of Day 1 AM, end of Day 1 PM, end of Day 2 AM) where the subagent reports progress.
3. Commit checkpoint: each rung's artifacts are committed on a spike branch before moving to the next rung.
4. Kill switch: operator can review the Day 1 EOD state and call the spike early if a Rung 2 failure already decides the verdict.

### Environment

- Box: operator's Linux box (this machine, RTX 3090, Go 1.25).
- Scratch location: `scratch/copilot-spike/` (new directory, gitignored at repo root or in a separate branch).
- Credentials: operator's GitHub Enterprise license (already held).
- Network: outbound HTTPS to GitHub Copilot backend required.

### Branch

- Spike branch: `spike/copilot-sdk-feasibility` off current `main`.
- Worktree: per operator convention (see `memory/feedback_always_work_in_worktree.md`), the subagent operates in a worktree at `~/workspace/chitin-spike-copilot-sdk/`.

## Self-review

### Placeholder scan

No TBD / TODO / vague-requirement fields. The one `<today>` placeholder in the decision-log path is a literal runtime substitution (chitin already does this). `<date>` and `<slug>` in the frontmatter of the findings report are filled in by the subagent at report time.

### Internal consistency

- The four rungs (Auth → Observe → Intercept → Gate+Log) are used consistently across §Ladder, §Kill conditions, and §Deliverable. No rung drift.
- Time-box (2 calendar days) is consistent across preamble, parameters table, and ladder windows.
- The go/no-go outcomes are paired symmetrically with post-spike paths — no asymmetric treatment.
- Baseline action vocabulary from PR #45 is treated as sufficient in §Out-of-scope and §Ladder Rung 4; no contradiction.

### Scope check

Single coherent spike. Multi-subsystem work is explicitly out of scope (claude-code, openclaw, hermes retirement, chain ingest). The spike produces one artifact (findings report) that gates one downstream spec (Copilot CLI governance v1). Clean decomposition.

### Ambiguity check

- "Structured data" in Rung 2: defined as "parseable tool name + parseable arguments." Not "JSON-shaped" (too restrictive) or "anything you can see" (too loose).
- "Synchronously block" in Rung 3: means the intercept handler's return value affects whether Copilot CLI proceeds with the tool call within the same code path, not via a subsequent retry or async message.
- "Honors the returned Decision" in Rung 4: means if `Decision.Allowed=false`, Copilot CLI does not execute the tool call. The exact mechanism (return error, raise exception, return a no-op) is SDK-dependent and documented in the findings.
- "Go" verdict is contingent on *both* all four rungs clearing *and* the Day 3+ estimate fitting in 12 days. A four-rung clear with a 20-day estimate is still no-go.

### Out-of-scope leak check

- No claude-code work sneaking in. No openclaw work. No hermes changes.
- No new action types added to `gov/normalize.go`. Baseline is enough.
- No ingest-policy subcommand work (that's a Phase 0 workstream for after the talk).
- No Readybench / bench-devs content.

### Dependencies

All in place or verifiable before Day 1:
- GitHub Copilot Enterprise license: operator has it (stated this session).
- `chitin-kernel gate evaluate` binary: shipped in PR #45, current on main.
- Go 1.25 toolchain: current chitin toolchain.
- Network access to GitHub Copilot backend: verifiable via any `gh copilot` command pre-spike.

## Execution handoff

Next action after user review: invoke `superpowers:writing-plans` to produce the 2-day, rung-by-rung implementation plan the subagent executes.
