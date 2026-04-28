# Copilot CLI Extension Spike — Design

**Date:** 2026-04-27
**Status:** Design. Post-talk spike (planned start 2026-05-08, day after the live-demo on 2026-05-07).
**Forcing function:** None — strategic, not deadline-driven. v1 (Go SDK as orchestrator) ships for the talk; this spike validates v2 (chitin as a Copilot CLI extension) as the cleaner long-term integration vector.
**Parent decisions (this session):**
- Research spike 2026-04-27 confirmed `@github/copilot-sdk` exposes a documented MIT-licensed extension surface — `joinSession({tools, hooks})` with `onPreToolUse`/`onPostToolUse`/`onSessionStart`/`onSessionEnd` lifecycle hooks (see `docs/observations/2026-04-27-copilot-openclaw-research.md`).
- v1 (`chitin-kernel drive copilot`, Go-SDK-as-orchestrator) ships for the talk and stays as the **closed-vendor pattern**; v2 (`~/.copilot/extensions/chitin/extension.mjs`, ride-inside) is the **open-vendor pattern**.
- Two-driver design as a permanent design principle: open vendors → in-process extension; closed vendors → wrapping orchestrator. Same governance API, vendor-specific shim.
- openclaw's existing `acpx copilot` integration (since 2026-03-09) is orthogonal — openclaw users inherit chitin gating once the extension is installed; no openclaw-side changes required.

## Preamble

The 2026-04-27 research spike produced a sharp finding: GitHub's Copilot CLI ships a documented, MIT-licensed extension API designed for exactly the integration shape chitin needs — a forked child process registers tools and lifecycle hooks via `joinSession`, and `onPreToolUse` fires synchronously before every tool execution. This is the surface chitin's tool-boundary governance was designed to attach to. Nobody has shipped this integration yet (PR `openclaw#4469` attempted an adjacent path and was rejected).

The v1 driver shipping for the 2026-05-07 talk is the inverse direction: chitin's Go binary spawns Copilot CLI as a child of a Go-SDK-driven harness. v1 works, demos, and is frozen until the talk. But it carries debt — wire-kind translations between Go enum and JS user-intent vocab, LockdownCh signalling because the SDK drops handler errors, the PrintEvent shim because session events aren't routed by default — that exists only because we're driving from outside instead of riding inside.

This spike asks one question: **does the extension API support synchronous tool-call refusal cleanly enough that v2 can replace v1 with a smaller, simpler shim?** "No-go" is a legitimate outcome; v1 is sufficient if the extension surface fails to deliver.

## One-sentence invariant

Within 2 calendar days, this spike produces a go / no-go / partial verdict on whether chitin can be packaged as a `~/.copilot/extensions/chitin/extension.mjs` that synchronously gates every Copilot CLI tool call via the existing `gov.Gate` — and if no-go, documents which rung blocked precisely enough to settle whether v1 stays canonical or whether a workaround is feasible.

## Scope

### In scope

- Install `@github/copilot-sdk` from npm in a scratch directory under `scratch/copilot-extension-spike/`.
- Author a minimal `extension.mjs` calling `joinSession({tools, hooks})`, discoverable at `~/.copilot/extensions/chitin-spike/extension.mjs`.
- Run `copilot "..."` against the spike installation and observe whether the extension loads, registers hooks, and the hooks fire.
- Validate `onPreToolUse` payload structure — tool name, arguments shape, parseability to a `gov.Action`.
- Synchronously refuse one tool call from inside `onPreToolUse` and confirm Copilot CLI does **not** execute the underlying command.
- Shell out from the hook to the existing `chitin-kernel gate evaluate` binary (JSON I/O), honor the returned `Decision`, and verify a line lands in `~/.chitin/gov-decisions-<today>.jsonl` matching the existing format.
- Produce a structured findings report (see §Deliverable).

### Out of scope

- Replacing v1. v1 stays canonical until v2 ships a full design spec + plan + implementation. The spike proves the path is buildable, nothing more.
- Re-implementing `gov.Gate` in TypeScript. The gate stays Go-side; the extension is a thin shim that calls into it.
- `onPostToolUse` / `onSessionStart` / `onSessionEnd` / `onErrorOccurred` hooks. These may be useful for observability but are not on the gate path. Brief mention in findings; do not exercise.
- Plugin marketplace publishing. The v2 design spec, if reached, scopes distribution.
- Performance tuning. The spike measures correctness; v2 design measures latency.
- Concurrent gate evaluations across multiple Copilot sessions. Single-session correctness only.
- openclaw-side changes. `acpx copilot` is independent; users who run openclaw → Copilot CLI inherit gating once the extension is installed.
- Claude Code or any other agent integration. Single-vendor spike.
- Readybench / bench-devs content. Chitin is OSS; the content-boundary rule applies (see `memory/feedback_chitin_oss_boundary.md`).

## Parameters

| Parameter | Value |
|---|---|
| Time-box | 2 calendar days |
| Earliest start date | 2026-05-08 (day after talk) |
| Owner | Subagent dispatched via `Agent`; operator advisory |
| Shape | Standard ladder, early-exit on first failed rung |
| Concurrent work | Operator processes talk feedback; may queue small v2 design notes |

## Ladder

| Rung | Window | Test | Pass criterion |
|---|---|---|---|
| 1 | Day 1 AM | SDK install + extension auto-discovery | A minimal extension at `~/.copilot/extensions/chitin-spike/extension.mjs` loads when `copilot` starts a session, evidenced by a `console.error()` line visible in the session output (or `--verbose` log). The `@github/copilot-sdk/extension` import resolves without manual path configuration |
| 2 | Day 1 PM | `onPreToolUse` fires with parseable payload | Hook receives a tool-call object containing tool name and arguments structured enough to map to a `gov.Action` (parseable JSON, separable name + args). One `bash` tool call captured end-to-end. The payload contract is documented in the findings, including any version-skew risk vs. the published types |
| 3 | Day 2 AM | Synchronous refusal | Hook synchronously refuses a tool call per the SDK's documented contract (return value, thrown error, or rejected promise — whichever the contract specifies); Copilot CLI confirms the tool was not executed (no shell side-effect, no successful tool result reported back to the model) |
| 4 | Day 2 PM | gov.Gate bridge | Hook shells out to `chitin-kernel gate evaluate` with a JSON action payload, parses the returned `Decision` JSON, refuses on `Allowed=false`, allows on `Allowed=true`. One allow path (e.g., `ls /tmp`) and one deny path (e.g., `rm -rf /tmp/foo`) exercised end-to-end. Decision-log line lands in `~/.chitin/gov-decisions-<today>.jsonl` matching the v1 format |

### Rung-pass semantics

- Rung N cleared ⟹ the primitive at Rung N works on this box, this SDK version, with these credentials. Not "production-ready" — "exists."
- All four rungs cleared ⟹ "go" verdict. v2 design spec follows.
- Rung N failed ⟹ document precisely. v1 stays canonical unless workaround is identified within the spike window.

## Kill conditions

| Condition | Rung | Consequence |
|---|---|---|
| Extension auto-discovery requires capabilities the spike box doesn't have (signed extensions, registered author, org-allowlist) | 1 | No-go: blocked at Rung 1 |
| `joinSession` API has churned since 2026-04-02 public preview; documented signature no longer matches the installed SDK | 1 | No-go: blocked at Rung 1 (also flag for v2 design — preview-stability risk) |
| `onPreToolUse` is observe-only with no return-value contract for refusal | 3 | No-go: blocked at Rung 3 — extension API is observability, not governance |
| Refusal "works" but is async/next-turn — Copilot CLI executes the tool before the refusal lands | 3 | No-go: blocked at Rung 3 — same diagnosis as above for the gate path |
| Hook payload is too unstructured to map to `gov.Action` (e.g., free-form prompt without tool/args separation) | 2 | No-go: blocked at Rung 2 |
| Subprocess shell-out to `chitin-kernel gate evaluate` is blocked from inside the extension sandbox (Node `child_process` restricted) | 4 | Soft no-go: flag possible workarounds (in-process Go via WASM, unix socket to a long-running gate daemon, FFI). Decision deferred to v2 design |

### Soft blockers (document, do not force no-go)

- Extension manifest requires a permission field we don't yet emit (e.g., `permissions: ["execute_subprocess"]`).
- SDK is in public preview as of 2026-04-02; version-pinning needed and may churn.
- Hook latency observed during the spike — document for the v2 design.
- Authentication state required by the extension differs from what `gh auth` provides today.

## Deliverable

End-of-Day-2 output: a markdown findings report committed at `docs/superpowers/specs/2026-05-10-copilot-extension-spike-findings.md` with these sections:

1. **Verdict** — one of: `go (v2 buildable)`, `no-go (v1 stays canonical)`, `partial (rungs 1–N cleared, rung N+1 blocked, workaround=X)`. One sentence, no hedging.
2. **Rung results** — for each of the four rungs: cleared / failed + evidence (log excerpt, code snippet, error message). Audit-able.
3. **v2 build estimate** — if `go`: rough day-count to ship a v2 driver behind a feature flag. Identify the largest unknown.
4. **Blockers + workarounds** — every soft blocker; every hard blocker if no-go; explicit workaround paths for any partial outcome.
5. **Comparison with v1** — code-size delta (v2 expected smaller); demo-invocation-flow comparison (`chitin-kernel drive copilot "..."` vs. `copilot "..."` with extension installed); user-facing UX delta; debt items v1 had that v2 eliminates (wire-kind hack, LockdownCh, PrintEvent shim).
6. **Recommendation** — proceed to v2 design spec immediately, defer indefinitely with rationale, or kill v2 path entirely.
7. **Artifacts** — paths to spike code under `scratch/copilot-extension-spike/`, config files, decision-log samples, version-pinned SDK manifest.

## Post-spike paths

### If verdict is `go`

Brainstorm + write the v2 driver design spec. The spec scopes:
- Distribution path — npm package (`@chitinhq/copilot-extension`)? plugin marketplace listing? bundled with `chitin-kernel` binary and installed via a `chitin install copilot-extension` subcommand?
- gov.Gate boundary protocol — subprocess JSON (cleanest, matches v1's `chitin-kernel gate evaluate`); unix socket to a long-running gate daemon (lower latency); embedded WASM (smallest dependency footprint, highest build complexity).
- Coexistence with v1 during a transition window — both can ship; the talk's "two patterns" framing makes coexistence a feature, not a migration burden.
- User migration story — `chitin-kernel drive copilot "..."` becomes a deprecated convenience wrapper after the extension is the default.

The v2 design targets a 4-week implementation horizon. No fixed external deadline.

### If verdict is `no-go`

v1 stays canonical. The talk's "v2 = SDK extension" closing-slide note gets an addendum in the runbook acknowledging the spike was attempted and what blocked. The closed-vendor pattern (wrapping orchestrator) becomes the chitin design pattern for both Copilot and Claude Code, not just Claude Code. The "two-driver design as a permanent principle" claim weakens — refine the framing in any future talk to match the empirical state.

### If verdict is `partial`

Document the partial state with explicit rung-cleared-vs-blocked detail. Make a binary call within 1 week of the spike's end: either workaround the blocker into a v2 design spec, or accept v1 canonical and treat the spike as a strategic-knowledge investment, not an integration. Do not drift between options.

## Execution

### How the subagent is dispatched

After user review of this spec and the talk on 2026-05-07, run `superpowers:writing-plans` to produce a per-rung task plan. The plan:

1. Per-rung task with explicit inputs, expected artifacts, fallback action on failure.
2. Time checkpoints (end of Day 1 AM, end of Day 1 PM, end of Day 2 AM) at which the subagent reports progress.
3. Commit checkpoint per rung's artifacts before moving to the next rung.
4. Operator kill switch — review Day 1 EOD state and call the spike early if a Rung 2 failure already settles the verdict.

### Environment

- Box: operator's Linux box (RTX 3090, Go 1.25, Node 22.x).
- Scratch location: `scratch/copilot-extension-spike/`, gitignored.
- Credentials: existing GitHub Enterprise license (already held).
- Network: outbound HTTPS to `api.individual.githubcopilot.com` required (same as v1).

### Branch

- Spike branch: `spike/copilot-extension-feasibility` off `main` post-talk.
- Worktree: `~/workspace/chitin-spike-copilot-extension/` per `memory/feedback_always_work_in_worktree.md`.

## Self-review

### Placeholder scan

No TBD / TODO. `<today>` is a runtime substitution chitin already does. `<date>` and `<slug>` are filled at report-write time, matching the prior spike's pattern.

### Internal consistency

- Four rungs (Discover → onPreToolUse → Refuse → gov.Gate bridge) consistent across §Ladder, §Kill conditions, §Deliverable.
- 2-day time-box consistent across preamble, parameters, ladder windows.
- "go / no-go / partial" outcomes mapped 1-to-1 to post-spike paths.
- Out-of-scope items (v1, openclaw, claude-code) reinforced in §Post-spike paths.

### Scope check

Single coherent spike on one integration vector. Multi-subsystem work explicitly out (claude-code, openclaw, hermes). Findings gate exactly one downstream artifact (v2 design spec). Clean decomposition.

### Ambiguity check

- "Synchronous refusal" (Rung 3): means Copilot CLI does not execute the tool *in the same turn* as the refusal — not "eventually doesn't execute." Tested by checking whether the model receives a refusal signal vs. a successful tool result.
- "Parseable to gov.Action" (Rung 2): means tool name maps to `Action.Type` AND `Action.Args` round-trips to/from JSON without loss. Loose match (e.g., extra fields in payload) is acceptable; lossy mapping is not.
- "SDK contract" (Rung 3): the spike trusts whatever `@github/copilot-sdk/extension` documents at the time of execution. If the doc and the runtime disagree, that disagreement is itself the finding.

### Out-of-scope leak check

- No v1 driver changes. v1 is frozen for the talk.
- No claude-code work. No openclaw work. No hermes changes.
- No `gov.Gate` refactoring. Extension calls the existing `chitin-kernel gate evaluate` binary unchanged.
- No marketplace publishing. v2 design scopes that.
- No Readybench / bench-devs content.
