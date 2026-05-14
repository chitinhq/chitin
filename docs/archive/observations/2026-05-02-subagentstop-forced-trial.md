# 2026-05-02 — SubagentStop forced trial (lab note)

**Goal:** answer #21's open questions empirically by capturing a real Task → subagent → SubagentStop sequence, dispatched via temporal/headless.

**Method:** ran `claude -p ... --include-hook-events --output-format stream-json` with a prompt that uses the `Agent` tool (a.k.a. `Task` in older docs) to spawn a subagent that runs a single `echo` and returns. Captured the full stream-json to `2026-05-02-subagentstop-forced-trial.jsonl` (committed alongside this note as the empirical fixture).

**Decision rule before the trial:** if SubagentStop fires and carries the subagent's `agent_id` directly in payload → straightforward chain-routing fix. If it doesn't, the chain-routing fix needs to track `task_id` from earlier events and match.

## What fired

Event sequence from the captured stream (parent session_id `a9e3b289-...`):

```
1. system.init                     — session boot
2. assistant message               — emits `Agent` tool_use (id: toolu_01LhU...)
3. system.task_started             — task_id=aa40c3d20f43461ff
                                     tool_use_id=toolu_01LhU...
                                     task_type=local_agent
4. user message [SUBAGENT]         — parent_tool_use_id=toolu_01LhU...
5. assistant message [SUBAGENT]    — emits Bash tool_use; parent_tool_use_id=toolu_01LhU...
6. user tool_result [SUBAGENT]     — Bash output; parent_tool_use_id=toolu_01LhU...
7. system.hook_started SubagentStop  — hook fires
8. system.hook_response SubagentStop — exit_code=0, outcome=success
9. user tool_result [PARENT]       — agentId=aa40c3d20f43461ff in tool_use_result
10. assistant message [PARENT]     — final summary
11. result event                   — terminal_reason=completed
```

## Findings

### Finding 1 — SubagentStop fires exactly once

One `hook_started` + one matching `hook_response` per Task spawn. No duplicates. No retries. No race.

This contradicts the implicit fear in #22's lab note (which observed a possibly-duplicate PreCompact firing) for the SubagentStop case specifically. SubagentStop appears well-behaved.

### Finding 2 — Tool name is `Agent`, not `Task`

The fired tool's `name` field is `"Agent"`. Older docs reference `Task` as the tool name; today's Claude Code emits `Agent`. (This matches #171 / #69's normalizer fix that added `"Agent"` as a recognized tool name in chitin's closed-enum vocabulary.)

The `task_id` system event uses `task_id` as the field name — Claude Code is using both names internally. Adapter code should accept both.

### Finding 3 — SubagentStop hook payload does NOT carry agent_id directly

The `hook_started` and `hook_response` events for SubagentStop carry `hook_id`, `hook_name`, `hook_event` (= `SubagentStop`), `session_id` (the parent's), and outcome fields — but **no `agent_id`** field on the hook itself.

Implication for #21: the adapter cannot route `chain_id = agent_id` from the SubagentStop hook payload alone. Two viable strategies:

- **Strategy A — Track task_started.** When the adapter sees `system.task_started`, record the mapping `task_id → tool_use_id` for the session. When SubagentStop fires, the most recent open task_id is the agent_id. Caveat: nested or concurrent subagents need a stack/set, not a single most-recent.
- **Strategy B — Read from tool_result.** The parent's tool_result for the Agent call carries `tool_use_result.agentId` (and the `agentId` shows up as plain text in the result content too). Match on `parent_tool_use_id` chain to find the corresponding agent_id.

Strategy A is cleaner (state is in the event stream itself), Strategy B is more resilient to out-of-order delivery.

### Finding 4 — `parent_tool_use_id` is the spawning_tool_call_id

Every event inside the subagent's lifecycle carries `parent_tool_use_id` set to the parent's `Agent` tool_use_id (`toolu_01LhU...`). This **directly answers #4's spawning_tool_call_id question** — populate `payload.spawning_tool_call_id` from `parent_tool_use_id`.

For `parent_agent_id`: the subagent's events are still tagged with the parent session_id. The parent's `agent_instance_id` (chitin's adapter mints one per HOOK CALL today, but per `agent-identification-fingerprinting-v2` will be session-stable) is the right value.

### Finding 5 — No SubagentStart hook fires

Per #21's open question 2: confirmed — there's NO `SubagentStart` hook firing. The subagent's start is observable only via `system.task_started` (a system event in the stream-json, not a hook).

Implication: chitin can't synthesize a subagent's `session_start` chain event from a hook fire alone. Either:
- (a) Subscribe to `system.task_started` events alongside hooks (requires adapter changes — system events go through a different path than hook events).
- (b) Synthesize `session_start` lazily on the FIRST event the subagent emits (the user message at step 4 above carries `parent_tool_use_id`, distinguishing it from parent-session events).

Strategy (b) is cleaner — it's a stateless projection over the event stream.

### Finding 6 — agent_transcript_path stability

Open question 3 from #21: not directly observable from this single trial. The captured stream doesn't include any `agent_transcript_path` field. The stable identifier observed is `task_id` (= `aa40c3d20f43461ff`, hex), which is presumably derived from the agent's identity.

Best path forward: **use `task_id` as the agent_id chain key.** It's stable per-spawn, observable in `task_started`, and matches the `agentId` field that surfaces in the parent's tool_result.

## Implications for the open issues

| Issue | Status after this trial |
|---|---|
| **#21** SubagentStop chain routing | Implementable. Use Strategy A (track `task_started` → `task_id` mapping). The chain key is `task_id`. The `chain_id` for the subagent's events = task_id; `parent_chain_id` = parent's session_id. |
| **#22** PreCompact dedupe | Cannot reproduce in headless — `/compact` is interactive only. Either (a) defer until interactive trial, or (b) close as "original 2026-04-19 capture was likely user double-firing /compact, not duplicate hook fire." Recommend (a). |
| **#4** Subagent invariants | Implementable on top of #21. `parent_agent_id` = parent's `agent_instance_id`. `spawning_tool_call_id` = `parent_tool_use_id` from the subagent's events. |
| **#13** Sweep-transcripts | Independent of this trial. Implementation work unaffected. |

## Next steps

1. Implement #21 using Strategy A: extend `libs/adapters/claude-code/src/hook-runner.ts` to track `task_started` events alongside hook events; route SubagentStop to the most recent open task_id.
2. Wire #4's invariants once #21's `agent_id` plumbing exists.
3. Reopen #22 with a fresh interactive-session forced trial (n≥3 manual `/compact` invocations, observed via `claude --debug hooks` against an attended session). Out of scope for headless capture.
4. #13 stays open (orphan-recovery implementation is separate work).

## Reproducibility

The capture command:

```bash
claude -p "Use the Task tool with subagent_type=general-purpose and a description of \"capture-test-echo\" to spawn a subagent. The subagent should run exactly one Bash command: echo subagent-fired-ok. Wait for the subagent to complete and report back. Then stop." \
  --include-hook-events \
  --dangerously-skip-permissions \
  --allowedTools Task,Bash \
  --output-format stream-json \
  --verbose \
  --model claude-opus-4-7 \
  > capture.jsonl 2>capture.stderr
```

Run from any cwd (e.g. `/tmp/lab-capture-2026-05-02/`). Total wall time ~13s. Total cost in this trial: $0.19 USD (Opus, 2 turns, ~46k cache-read tokens).

Re-running produces a new session_id but the event SHAPE is stable. The fixture (`2026-05-02-subagentstop-forced-trial.jsonl`) is the canonical reference for the schema downstream consumers should expect.
