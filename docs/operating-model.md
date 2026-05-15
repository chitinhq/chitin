# Operating Model

How chitin actually runs today, and what each subsystem owns.

## Topology

Single-box. One Linux workstation with an RTX 3090 hosts the entire dev + dogfood loop.

```
                  ┌──────────────────────────────────────────────────────┐
                  │  Local workstation (RTX 3090, this machine)          │
                  │                                                      │
   Anthropic ─────┤── Claude Code ──┐                                    │
   OpenAI    ─────┤── Codex CLI  ───┤                                    │
   Google    ─────┤── Gemini CLI ───┼──► chitin-kernel ──► ~/.chitin/    │
   GitHub    ─────┤── Copilot CLI ──┤    (gov.Gate gates                 │
                  │                  │     every tool call,               │
                  │                  │     PreToolUse hooks               │
   local Ollama ──┤── openclaw ─────┘     for the CLIs;                  │
   (3090 GPU)     │                       drive copilot for              │
                  │                       the closed CLI;                │
                  │                       openclaw plugin                │
                  │                       for local-*)                   │
                  └──────────────────────────────────────────────────────┘
```

- **No Hetzner box.** Multi-machine observability was a Phase 3 framing in older docs; the box is dead. Collapse any "cross-machine" reading into single-box.
- **Murphy's Mac** is separate — chitin is not installed there. Out of scope for current dogfooding.
- **Cloud reasoning surfaces** (Anthropic, OpenAI, Google, GitHub) are accessed via their CLIs; chitin governs the tool calls those CLIs make on this box. The cloud reasoning itself is out of scope; the local effects are not.

## Subsystem ownership

| Subsystem | Owner | Live? |
|-----------|-------|-------|
| Capture (event chain) | Go kernel `emit` | ✅ shipped Phase 1.5, 2026-04-19 |
| Replay (kernel + heuristic layers) | Go kernel `internal/replay` + `chain replay` | ✅ shipped 2026-05-03 (#240, #253) |
| Chain analytics (stats / recommend-tier / snapshot / simulate / summarize / related) | Go kernel `internal/replay` | ✅ shipped 2026-05-03/04 (#240, #245, #246, #247, #249) |
| Skill mining (n-gram surface from chain) | Python `analysis/skill_mine.py` | ✅ shipped 2026-05-03 (#259) |
| Predictive model (chain-predict-outcome) | Python `analysis/predict.py` | ✅ shipped 2026-05-03 (#256) |
| Governance (`gov.Gate`) | Go kernel `internal/gov` | ✅ shipped 2026-04-28 (PR #45 + #51) |
| Cost envelope (cross-process) | Go kernel `internal/envelope` | ✅ cost-gov v3 |
| Universal usage feed (codex 5h/weekly, gemini calls, ollama-cloud rpm/tpm) | `python/analysis/codex_mine.py` + `~/.cache/chitin/usage/` | ✅ schema + codex producer shipped 2026-05-04 (#269); gemini/ollama producers in backlog |
| Drivers — Claude Code hook | `internal/driver/claudecode/normalize.go` + kernel install path | ✅ shipped (PR #66) |
| Drivers — Codex CLI (PreToolUse) | `internal/driver/codex/normalize.go` + `scripts/install-codex-hook.sh` | ✅ shipped 2026-05-04 (#272) |
| Drivers — Gemini CLI (BeforeTool) | `internal/driver/gemini/normalize.go` + `scripts/install-gemini-hook.sh` | ✅ shipped 2026-05-04 (#267) |
| Drivers — Copilot CLI (wrapping) | Kernel `drive copilot` | ✅ shipped (PR #51) |
| Drivers — openclaw (`local-*`) | openclaw `before_tool_call` plugin | ✅ shipped |
| Plugin runtime (Python + TS heuristic plugins) | `internal/router/plugins` + opt-in side-effect gate libs | ✅ shipped (#235, #237, #241, #250) |
| Plugin sandbox (bubblewrap, opt-in) | `internal/router/plugins/sandbox.go` | ✅ shipped 2026-05-03 (#255) |
| OTEL emit (projection) | Go kernel `internal/emit` | ✅ F4 shipped before 2026-05-07 talk |
| Router signal stamping | Go kernel `internal/router` + `cmd/chitin-kernel/router_hook.go` | ✅ post-cull shape: pure-Go signals, no in-gate LLM advisor |
| Removed in-tree orchestration | `apps/runner`, scheduler, Temporal, Slack app, in-gate peer spawn | ❌ culled 2026-05-06 to 2026-05-08; replaced by substrate composition |
| Swarm (substrate composition) | `swarm/bin/clawta-poller`, `swarm/workflows/kanban-dispatch.lobster`, `scripts/kanban-flow` | ✅ shipping incrementally since 2026-05-11; composes hermes (kanban) + openclaw (Lobster) |
| Hermes (operations agent) | `docs/hermes-role.md` + `~/.hermes/scripts/` | ✅ own P0/P1, board engine, clawta bridge, blocked digest |
| Souls library | `souls/canonical/` + `souls/experimental/` | historical analytics/reference artifact; not a kernel runtime surface |

## Order of operations (current, not aspirational)

Older docs stated "observability → governance → automation, in that order, per surface." That ordering described Phase 1's implementation sequence; it is not a design constraint anymore. Today chitin owns the first two and emits signals for downstream automation:

1. **Observability** is always-on. Every driver emits events to the chain by default.
2. **Governance** is on by default in `mode: enforce` for the baseline policy. Policies tighten as the debt ledger surfaces real-world denials worth enforcing.
3. **Automation** is composed, not re-implemented. The chitin-owned swarm (`swarm/`) drives the four-hop pipeline `hermes kanban → clawta tick → openclaw Lobster → frontier-coder CLI`. Hermes owns the kanban substrate; openclaw owns the workflow runtime + agent runtime; chitin owns the tick scripts, the workflow definition, and the chain/policy contracts that unify the hops. Approval flow still lives in hermes' `tools/approval.py`. See `docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md`.

## The three analysis output streams

The event chain feeds three distinct triage categories — this is the shape of the debt ledger:

- **What needs to be fixed** — bugs, hardening candidates, kernel correctness issues. Feeds the issue backlog.
- **What needs determinism** — non-deterministic agent behavior worth pinning down via policy (rewrites, denials, forced outputs). Feeds new `chitin.yaml` rules.
- **Which soul is best at what** — empirical soul-routing data. Correlates `soul_id` in `session_start.payload` with outcome quality. Feeds the souls library as evidence, replacing self-reported `best_stages` with measured ones.

## Dogfooding posture

Dogfooding is permanent, not a phase. Every agent action on this box flows through chitin. The only tunable is cadence (how often we close the loop from chain → policy update).

## Local-first

All capture and gating works fully offline. OTEL emit (post-F4) is opt-in and reaches a configured collector; the kernel-write-survives-OTEL-failure invariant means losing the network never loses the chain.

## What chitin does not do

- It does not run an agent loop. The drivers do.
- It does not ship a cloud SaaS. Local-only is the product boundary.
- It does not replace the agent. It observes and gates.
