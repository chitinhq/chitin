# Cull the escalate effect; defer operator approvals to Hermes

**Date:** 2026-05-08
**Status:** Accepted (PRs #397, #398, #399 merged)
**Supersedes:** the operator-approval escalation design from 2026-05-07
(`docs/superpowers/specs/2026-05-07-operator-approval-escalation-design.md`)

## Context

Between 2026-05-07 and 2026-05-08 we shipped 16 PRs (#380–#396)
implementing an in-gate operator-approval escalate effect:
`pending_approvals.sqlite`, a 3-call notify pipeline (`hermes kanban
create` + `notify-subscribe` + direct WhatsApp bridge POST), a
`chitin-pending-watch.timer` systemd unit, a `chitin-escalate-replies`
hermes plugin to intercept WhatsApp replies, and a `remember_grants`
short-circuit table.

While verifying the flow end-to-end on 2026-05-08, the operator asked
for an external survey of Hermes and OpenClaw recipes. The survey
returned a finding the operator labeled the killer:

**Hermes already shipped the entire approval flow we built — and it's
better.** `tools/approval.py` provides:

| Capability we built in chitin | Hermes already does it |
|---|---|
| Operator-approval escalate effect (`effect: escalate`) | `approvals.mode: manual\|smart\|off` in `config.yaml` |
| Bridge-POST WhatsApp ping | `pre_approval_request` hook fires on every approval; gateway routes to active chat automatically |
| `chitin-escalate-replies` plugin to parse "approve"/"deny" | Native: gateway parses `yes/y/approve/ok/go` and `no/n/deny/cancel` |
| `remember_grants` table + window short-circuit | `[once \| session \| always \| deny]` 4-way; "always" persists to `command_allowlist` |
| `chitin-pending-watch.timer` (30s polls Hermes for replies) | Approval blocks the tool call directly — no polling, no separate process |
| `escalate-timeout` rule with `Wait` deadline | `approvals.timeout: 60` config, fail-closed |
| Bug C/D/L (Wait killed by harness; grant insertion) | Doesn't exist — Hermes runs approval inline within its own loop, no harness-timeout race |
| Hardline blocklist on top of all of it | Native: `tools/approval.py::UNRECOVERABLE_BLOCKLIST` |

Plus Hermes has plugin hooks `pre_approval_request` +
`post_approval_response` that give us audit-trail integration without
re-implementing the loop.

## The reorientation

Today's chitin escalate flow was "chitin opens a kanban task → runs
notify-subscribe → POSTs to WhatsApp bridge → plugin intercepts the
reply → CLI mutates pending_approvals." That was chitin trying to **be**
Hermes' approval system, badly, in parallel.

**Right shape:** chitin gate evaluates → if a rule denies → return the
deny back through `pre_tool_call` → Hermes' native approval system
takes over. Operator gets the prompt in their active chat, replies
normally, Hermes blocks-and-resumes the tool call. Chitin observes
the outcome via `post_approval_response`.

## What chitin keeps (genuinely additive vs Hermes)

- **`chitin-kernel gate` per-tool-call evaluation.** Universal cross-driver gate; Hermes' `pre_tool_call` is local to Hermes only. Chitin gates Claude Code, Codex, Gemini, and Hermes from one canonical action vocabulary.
- **Cross-driver `normalize.go`** (claudecode/codex/gemini/hermes). Canonical action vocabulary; no Hermes equivalent.
- **`chitin.yaml` declarative policy schema.** Typed actions + `path_under` + bounds; Hermes is regex-on-shell-string only.
- **`chitin-router-hook` signal stamping.** Heuristics (blast-radius, floundering, drift) + tamper-evident chain rows; LLM consultation belongs downstream.
- **`gov-decisions.jsonl` audit log + chain replay.** Tamper-evident chain shape; Hermes has logs but not this.
- **Severity ladder + lockdown counter** (`agent_state` in gov.db). Per-agent escalation across all tasks; Hermes has per-task retry budgets only.
- **Bounds enforcement** (lines/files changed on git push). No Hermes equivalent.

## What chitin culls (parallel to Hermes)

- `internal/gov/escalate.go` — `EscalateStore`, `Wait`, `RememberGrants`, `SweepStale`, `OpenEscalateStore`. ~500 LOC.
- `cmd/chitin-kernel/notify_hermes.go` — 3-call pipeline + bridge POST. ~370 LOC.
- `cmd/chitin-kernel/pending.go` — `pending list/approve/deny/watch-hermes` CLI. ~200 LOC.
- `libs/hermes-plugins/chitin-escalate-replies/` — WhatsApp reply interceptor. ~200 LOC.
- `infra/systemd/chitin-pending-watch.{service,timer}` — 30s reply watcher.
- `EscalateConfig`, `EffectEscalate` constant, nested `escalation:` parser machinery, and the third evaluation pass in `Policy.Evaluate`.
- `pending_approvals.sqlite` and `remember_grants` tables (data only; deleted at runtime).
- `~/.chitin/operator.yaml` — `loadOperatorConfig` + `operatorConfig` struct deleted.

**Total: ~3900 LOC removed** (PR #399's net diff).

## How operator-tier approval works now

For policy rules like `no-governance-self-modification` (file.write to
`chitin.yaml`), the rule is back to `effect: deny`. Operator workflow:
edit gov files manually via `vim`/`sed` from a non-Claude-Code shell.
This is what we'd been doing all 2026-05-07 anyway, since the in-gate
escalate flow was unreachable from a fresh Claude Code session for
several hours of substrate debugging.

For dangerous shell patterns (`rm -rf`, `sudo tee /etc/`, etc.), Hermes'
own `tools/approval.py` regex matchers fire when the agent is
Hermes-driven. For non-Hermes agents (Claude Code, Codex, Gemini),
chitin's policy denies the action; the operator does it manually. No
in-gate prompt-and-wait.

This loses the convenience of "ping operator on WhatsApp; resume the
agent on approval." Hermes-driven agents keep this via Hermes' built-in
flow. Other drivers don't — but they didn't have it before PR #380
either.

## Phased cull execution

Sequenced because each phase neutralizes the next:

| Phase | PR | What |
|---|---|---|
| 1 | #397 | `chitin.yaml`: revert `no-governance-self-modification` from `effect: escalate` → `effect: deny`. Removes the rule's nested `escalation:` block. Neutralizes the runtime path. |
| 2 | #398 | Delete `infra/systemd/chitin-pending-watch.{service,timer}` from repo + operator-side `disable --now` + symlink remove. |
| 3 | #399 | Delete the runtime: `escalate.go`, `notify_hermes.go`, `pending.go`, the `chitin-escalate-replies` plugin, all related tests. Edit 5 source files to keep build green: drop `Effect` typed enum, drop `EscalateStore`/`NotifyHermes` fields on `Gate`, drop `EscalationYAML`/`Channel`/`TimeoutSeconds`/`RememberWindowSeconds`/`NotifyTemplate`/`Escalation` fields on `Rule`, drop the third evaluation pass in `Evaluate`, drop `findRuleEscalation`, drop `pending` subcommand. Rejects any remaining `effect: escalate` in chitin.yaml at parse time so a stale policy fails loud. |
| 4 | (operator) | `rm ~/.chitin/pending_approvals.sqlite ~/.chitin/operator.yaml`. Uninstall the `chitin-escalate-replies` plugin from `~/.hermes/plugins/`. Already done. |
| 6 | (this doc) | Decision record. |

Phase 5 (cleanup `loadOperatorConfig` + `operatorConfig` struct) was
folded into Phase 3 because the relevant code lived in the deleted
`notify_hermes.go`.

## What was un-shipped

Substantively reverted: PRs #380, #382, #385, #389, #390, #391, #392,
#393, #395, #396. ~10 of yesterday/today's 16 PRs were scaffolding
around a Hermes feature we hadn't found yet.

Kept: #381 (gate plumbing — `sqlite busy_timeout`, `mergePolicies`
PerAction merge), #386 (chitin-runner → chitin-worker rename), #387
(test isolation via per-test `CHITIN_HOME`), #383's Go-side
`ActKanbanCall` + `ActHermesProcess` action types, #384's
`default-allow` rules for those types, #388 (which is now moot with
the timer removed but harmless), #394 (cosmetic chitin.yaml line 251).

## Counterfactual

If we had done the external recipe survey before PR #380 instead of
after PR #396, the cull wouldn't have been needed. Lesson: when
operator says "I want a new feature," check if the substrate already
provides it before designing a parallel implementation. The signal
chitin chases — universal-tool-call interception, cross-driver
canonical actions, audit chain — is unique. The signal it duplicated
— "wait for operator approval over a chat platform" — is not.

## Followups

- The hermes-side approval flow needs to be exercised against a chitin
  policy denial to confirm composition works as described. Today's
  cull removed the in-gate path; the next test is "Claude Code agent
  triggers a chitin deny → does Hermes' approval surface? On
  non-Hermes drivers, what's the operator UX?"
- Bug F (shell-redirect bypass of gov-file rules) is unaffected by
  this cull — it's a `gov.Normalize` issue in chitin.
- Filed Bug F refile is `t_c7b3f3f8`. Bugs C/D/E are deletable: the
  underlying code is gone.
