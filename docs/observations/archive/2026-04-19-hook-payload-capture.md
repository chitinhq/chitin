---
date: 2026-04-19
soul: curie
status: lab-note
related:
  - docs/superpowers/specs/2026-04-19-observability-chain-contract-design.md
  - docs/superpowers/plans/2026-04-19-phase-1.5-observability-chain-contract.md
  - libs/adapters/claude-code/src/hook-dispatch.ts
---

# Hook payload capture — what actually arrives on stdin

## Hypothesis

The 7 hooks subscribed by `hookinstall` (`SessionStart`, `UserPromptSubmit`,
`PreToolUse`, `PostToolUse`, `PreCompact`, `SubagentStop`, `SessionEnd`)
fire with structured JSON payloads sufficient for `hook-dispatch.ts` to
construct the corresponding event_type per spec §"hook → event_type"
table (lines 313–320 of the design doc). Specifically I want to confirm
the two underdocumented ones — `SessionStart` and `SubagentStop` — and
file a null for `PreCompact` if it doesn't fire.

**Decision rule for "wired and emitting":** at least one capture file
whose JSON `hook_event_name` matches the hook name. Anything less is
a null result, not a soft pass.

## Method

`~/.chitin/hook-capture/capture.sh` is registered as an extra command on
all 7 hooks; it tees raw stdin to `<HookEvent>-<RFC3339Nano>-<pid>.json`.
The capture is purely observational — it does not modify the dispatch
chain. Window: 2026-04-20T01:22Z–01:28Z (≈6 minutes of natural use,
not a forced trial).

## Distribution (n by hook)

| Hook | n |
|---|---|
| PreToolUse | 41 |
| PostToolUse | 39 |
| UserPromptSubmit | 9 |
| SessionEnd | 2 |
| **PreCompact** | **2** (forced trial, 2026-04-20T01:31Z) |
| SubagentStop | 1 |
| SessionStart | 1 |
| _SyntheticTest_ (scaffold) | 1 |
| _capture.sh_ (script) | 1 |

Variance worth not averaging away:

- **PreToolUse / PostToolUse mismatch (41 vs 39, Δ=2).** Either two
  PostToolUse fires were lost, or two PreToolUse calls had no matching
  Post (in-flight or aborted at window close). Need a longer window or
  a paired-call audit script to disambiguate. **Filed as open question.**
- **SessionEnd 2 vs SessionStart 1.** Capture window started mid-session
  at least once. Not a defect — confirms `source: startup` ≠ "first
  capture in window" and we can't infer chain-head from capture order.

## SessionStart — confirmed (n=1, source=startup)

```json
{
  "session_id": "505c4216-bc0a-49d1-b512-55df4d6563c0",
  "transcript_path": ".../505c4216-...jsonl",
  "cwd": "/home/red/workspace/chitin",
  "hook_event_name": "SessionStart",
  "source": "startup",
  "model": "claude-opus-4-7[1m]"
}
```

**Findings:**

- `source` field discriminates `startup | resume | clear | compact` —
  this is the signal `hook-dispatch.ts` needs to decide whether to emit
  a fresh `session_start` event or splice into an existing chain on
  resume. Confirmed present. Spec already accounts for this; capture
  data validates the field exists in production payloads, not just docs.
- `model` is included in the payload — useful for the multi-model
  routing work (Phase D/E) without needing to grep transcript headers.
- No `parent_session_id` field on `source: startup` (as expected). Have
  not yet captured a `source: resume` to confirm it appears there.

## SubagentStop — confirmed (n=1, agent_type=general-purpose)

```json
{
  "session_id": "e2abc574-187b-4d6e-91c8-1ce2114e860d",
  "permission_mode": "bypassPermissions",
  "agent_id": "abc22387f8b2cee71",
  "agent_type": "general-purpose",
  "hook_event_name": "SubagentStop",
  "stop_hook_active": false,
  "agent_transcript_path": ".../subagents/agent-abc22387f8b2cee71.jsonl",
  "last_assistant_message": "OK"
}
```

**Findings (load-bearing for the dispatch contract):**

1. **The subagent has its own transcript path** under `/subagents/`,
   distinct from `transcript_path` on the parent session. The chain key
   for the subagent's `session_end` event MUST be the subagent's
   `agent_id` (or `agent_transcript_path`), not the parent `session_id`
   — otherwise we'd corrupt the parent chain by closing it on every
   subagent return. `hook-dispatch.ts` should be audited against this.
2. `last_assistant_message` is provided but appears to be raw text only
   ("OK" here). Useful for debugging but not a substitute for ingesting
   the subagent transcript at SubagentStop time if we want the full turn.
3. `stop_hook_active: false` — meaning available but unobserved in the
   "true" state. Not yet clear under what condition Claude Code sets it
   true; flag for follow-up.
4. `permission_mode` propagates from the parent (`bypassPermissions`
   here). Useful provenance for governance — a subagent inherits the
   parent's permission posture, and we now have a way to record that on
   the chain.

## PreCompact — confirmed via forced trial (n=2, trigger=manual)

**Initial null (n=0 in the 6-minute observational window) was resolved by
the cheap experiment:** invoking `/compact` from inside this session
landed two captures within 30s.

```json
{
  "session_id": "505c4216-bc0a-49d1-b512-55df4d6563c0",
  "transcript_path": ".../505c4216-...jsonl",
  "cwd": "/home/red/workspace/chitin",
  "hook_event_name": "PreCompact",
  "trigger": "manual",
  "custom_instructions": ""
}
```

**Findings:**

1. **`trigger` field discriminates `manual | auto`** — load-bearing for
   the chain contract. A manual `/compact` is a user-initiated chain
   transition; an auto-compaction (token-threshold driven) is not. The
   dispatch `compaction` event_type should record `trigger` so downstream
   policy can distinguish "user chose to compact" from "system had to."
   Auto-trigger value not yet observed; assumed but unconfirmed.
2. **`custom_instructions`** captures the optional text passed via
   `/compact <text>` (empty here). Useful provenance — when a user steers
   compaction with custom instructions, that's a governance-relevant
   signal worth keeping on the chain.
3. **Two captures from one user-visible `/compact`** (timestamps 013113
   and 013142, Δ≈30s, identical payloads modulo timestamp). Either (a)
   the user invoked `/compact` twice in this window and only one was
   acknowledged in the visible transcript, or (b) Claude Code fires
   PreCompact more than once per compaction (e.g., a dry-run pass plus
   the real one). **Open question** — the dispatch chain must not emit
   two `compaction` events for one logical compaction or it'll corrupt
   downstream chain integrity. Audit needed.
4. `transcript_path` on PreCompact equals the *pre-compaction* transcript
   path — the post-compact transcript may be the same file with appended
   summary turn, or a new file. Worth confirming on the next capture.

**The `PreCompact` hook is wired and emitting.** Initial hypothesis
(spec §313–320 mapping holds) confirmed.

## What this changes (or doesn't)

- **Spec stays correct as written.** Nothing in the captured payloads
  contradicts the §313–320 mapping table.
- **Dispatch implementation needs two audits:**
  1. Confirm SubagentStop closes the *subagent's* chain (keyed on
     `agent_id`), not the parent. If it currently closes by `session_id`
     alone, that's a latent bug.
  2. Confirm PreCompact emits exactly one `compaction` event per logical
     compaction (the n=2 forced-trial result may indicate duplicate
     fires; if so, dispatch must dedupe or risk corrupting chain
     integrity).
- **PreCompact is wired** (forced-trial result, 2026-04-20T01:31Z) — but
  the `trigger=auto` branch and the duplicate-fire question remain open.

## Open hypotheses (next experiments)

1. ~~**`PreCompact` fires with usable payload**~~ — **resolved
   2026-04-20T01:31Z**, n=2 forced-trial captures, payload shape
   includes `trigger` + `custom_instructions`. New sub-questions
   spawned: see (1a) and (1b) below.
   - **1a. `trigger=auto` payload matches `manual`** — wait for or force
     a token-threshold compaction; confirm `trigger` value and that no
     other fields shift.
   - **1b. PreCompact fires once per logical compaction** — n=2 in the
     forced trial is unexplained. Either user double-invoked or Claude
     Code fires twice; need one clean controlled `/compact` to
     disambiguate.
2. **`SessionStart` `source` enumeration is complete** — capture a
   `resume` and a `clear` to confirm the field's value space matches
   what dispatch branches on. Note: a `compact`-sourced SessionStart
   should now be observable in this very session's next SessionStart
   capture (post-compact resume).
3. **Pre/Post tool-call pairing is 1:1 in steady state** — write a
   small audit (group by `tool_use_id`, count unmatched). The 41/39
   mismatch may be a window-edge artifact or a real loss; right now
   we can't tell.
4. **`stop_hook_active: true` is reachable** — find the trigger
   condition; document it.

## Decision-rule rollup

| Hook | Wired? | Payload sufficient? | Notes |
|---|---|---|---|
| SessionStart | ✅ | ✅ | `source` + `model` present |
| UserPromptSubmit | ✅ | (not audited here) | n=9, deferred |
| PreToolUse | ✅ | (not audited here) | n=41, deferred |
| PostToolUse | ✅ | (not audited here) | n=39, deferred |
| PreCompact | ✅ | ✅ | `trigger` + `custom_instructions` present; `auto` branch + dedupe still open |
| SubagentStop | ✅ | ✅ | **subagent transcript distinct from parent** |
| SessionEnd | ✅ | (not audited here) | n=2, deferred |
| _Stop_ | n/a | n/a | intentionally not subscribed (spec line 322) |
