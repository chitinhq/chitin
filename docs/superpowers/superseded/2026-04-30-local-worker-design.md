# Local 24/7 Worker — Design

> **Superseded (2026-05-08):** The Temporal control plane (`apps/runner`) was deleted in the 2026-05-06 scope narrowing. The worker concept (safe local-LLM autonomy via the gate) is still valid, but the specific architecture described here—Temporal orchestration, `apps/runner`, openclaw-worker-loop—is superseded. Worker shape should be redesigned using the current kernel architecture.

**Status:** ~~spec draft~~ superseded. First articulation of the chitin-governed local worker shape; reference rig for the "safe local-LLM autonomy" thesis. Post-talk implementation candidate.

> **PARTIALLY SUPERSEDED 2026-04-30 (same day).** The "openclaw owns the worker loop" framing in `## Positioning` (line 36), the openclaw-worker-loop-plugin component (`## Components in detail → Worker-loop plugin (openclaw)`), and the chitin-owned task queue + CLI (`## Components in detail → Task queue (chitin)`) are superseded by the three-plane decomposition recorded in `2026-04-30-local-worker-design-addendum.md`. Read the addendum first; treat the superseded sections of this spec as historical context for *why* the reframe was necessary. Invariants, bootstrap rules, observability loop, spike evidence, and acceptance criteria all stand.

**Author:** in-session sketch, 2026-04-30. Spike-driven — verified end-to-end before writing (see "Spike evidence" below).

**Trigger:** the user's question "we are not using the 3090 at all right now... how can we start leveraging it 24/7?" plus the thesis articulation later in the same session: chitin's real end goal is letting local LLMs run safely — determinism via policy gating, cloud calls for reasoning escalation. The 3090 is the reference rig for that thesis, currently idle.

---

## Positioning

This spec sits one step downstream of governance v1 / cost-gov v3 / decisions-stream (PR #78). Those primitives govern *human-driven* sessions today. The worker generalizes the same primitives to *autonomous* sessions running on the local model — without changing the gate, the chain, or the analysis layer.

```
              human                                 worker (this spec)
              ─────                                 ──────────────────
         user's terminal                       openclaw daemon
                │                                       │
                │ claude --print …                      │ for each task in queue:
                │                                       │   provision worktree
                ▼                                       │   acpx.spawn("claude-code", env, args)
       Claude Code session ─────────┐         ┌────────▼──────────────
                │                   │         │ Claude Code session (worktree)
                │ tool calls        │         │   model: qwen3-coder:30b via ollama :11434
                ▼                   │         │   tool calls
       chitin gate ─────────────────┴─────────┴───► chitin gate
       (PreToolUse hook)                            (same hook, same gov.Gate)
                │                                       │
                ▼                                       ▼
            gov-decisions JSONL ←──────────────── gov-decisions JSONL
                          (one stream — workers and humans interleave;
                           analysis layer reads both)
```

The chitin gate doesn't care who initiated the session — same `gov.Gate.Evaluate()` API across human and worker traffic. **The only thing that's net-new is the loop that produces autonomous traffic.** Per the architectural rule established when hermes was killed (chitin = governance, not tick-loop), **the loop lives in openclaw**, not in chitin.

---

## Invariants (must hold)

1. **Gate authority.** Every tool call the worker emits is evaluated by `gov.Gate.Evaluate()` in `mode: enforce` before execution. Worker mode never falls back to monitor.
2. **Worktree isolation.** One git worktree per task. Worker never touches the trunk working tree. Force-kill, crash, or bad merge is recoverable by deleting the worktree.
3. **Bounded autonomy.** Every worker run terminates by one of: task complete (draft PR opened), envelope exhausted (cost-gov v3 cap), or wall-clock timeout. No unbounded loops.
4. **Single source of policy.** Chitin's `gov.Gate` is the only policy authority on tool calls. Openclaw's own `exec-approvals.json` / `exec-policy` is **not** layered on top — two policy authorities is the failure mode the kernel-authority rule already forbids.
5. **Output is review-gated.** Worker outputs go to a draft PR. No worker writes to `main` directly. Human review is the merge.

---

## Architecture

### Three components, three responsibilities

| Component | Role | What's already there | What's net-new |
|---|---|---|---|
| **openclaw** | owns the worker loop: poll queue, provision worktree, spawn Claude Code, handle outcome | `acpx` plugin (built-in agent registry includes `claude-code`); daemon mode (`onboard --install-daemon`); plugin runtime (in-process via jiti) | A worker-loop plugin (or flow definition) — this spec |
| **ollama** | serves the model via Anthropic-compat endpoint at `:11434` | Anthropic Messages API compat shipped in v0.14; local `qwen3-coder:30b` and cloud `qwen3-coder:480b-cloud`, `glm-5.1:cloud` available | Nothing |
| **chitin** | owns the queue (task schema + classification); gates every tool call via PreToolUse hook; accumulates gov-decisions for the analysis layer | Claude Code hook driver (PR #66); `gov.Gate` (PR #64); cost-gov v3 envelope (in flight); decisions stream (PR #78) | Queue surface (`~/.chitin/worker-queue.jsonl` + CLI claim/complete commands) |

No proxy layer (no CCR, no LiteLLM). Claude Code talks Anthropic-compat to ollama directly. Verified in the spike.

### Data flow

```
~/.chitin/worker-queue.jsonl                      ← chitin owns queue
        │
        │ openclaw worker-loop plugin polls
        ▼
openclaw daemon (port 18789, existing)            ← openclaw owns loop
        │ for each claimed task:
        │   1. classify (mechanical | judgment)
        │   2. provision worktree from main
        │   3. seed worktree's .claude/settings.json with chitin gate hook
        │   4. acpx.spawn("claude-code", {
        │        cwd: worktree,
        │        env: {
        │          ANTHROPIC_BASE_URL: "http://127.0.0.1:11434",
        │          ANTHROPIC_API_KEY:  "ollama"
        │        },
        │        args: ["--print", "--dangerously-skip-permissions",
        │               "--model", classification === "judgment"
        │                          ? "qwen3-coder:480b-cloud"
        │                          : "qwen3-coder:30b",
        │               taskPrompt]
        │      })
        ▼
Claude Code session (in worktree)                 ← Claude Code as ACP child
        │ talks Anthropic-compat → ollama → 3090 (or Ollama Cloud)
        │ emits tool calls
        ▼
PreToolUse hook → chitin-kernel gate evaluate     ← chitin governs
        │ allow/deny + bounds + envelope check
        ▼
gov-decisions-<date>.jsonl  ───►  analysis layer (PR #78) ──► candidate rules ──► human review
```

---

## Components in detail

### Worker-loop plugin (openclaw)

A new openclaw plugin (TypeScript, in-process via jiti) or a flow definition under `~/.openclaw/flows/`. Distribution candidate: `openclaw-plugin-chitin-worker` so non-chitin openclaw users can install it without pulling chitin's full kernel.

Responsibilities:

- Poll `~/.chitin/worker-queue.jsonl` (or `chitin-kernel queue claim` CLI) on a tick (default 30s).
- Claim one task at a time (single 3090, single worker; multi-worker is a future spec).
- Classify task: read `classification` field from queue entry; if absent, default to `mechanical`.
- Provision worktree at `~/.chitin/worker-worktrees/<task-id>/` via `git worktree add`.
- Seed worktree's `.claude/settings.json` with the gate hook + tighter bounds (template below).
- Spawn Claude Code via `acpx.spawn("claude-code", { cwd, env, args })`.
- On completion: open draft PR via `gh pr create --draft`, mark task done in queue.
- On failure (timeout, envelope-exhausted, terminal deny): mark task escalated, leave worktree for inspection, retain logs.

### Task queue (chitin)

**v1: local JSONL** at `~/.chitin/worker-queue.jsonl`. Append-only, status mutated via JSONL rewrite (consistent with existing chitin patterns). Schema:

```json
{
  "id": "WT-2026-04-30-001",
  "title": "rename FooBar → BarBaz across libs/contracts",
  "body": "<task body / prompt>",
  "classification": "mechanical",
  "bounds": {
    "max_tool_calls": 50,
    "max_cost_usd": 0.50,
    "wall_timeout_s": 600
  },
  "status": "pending",
  "created_at": "2026-04-30T12:00:00Z"
}
```

CLI surface:

- `chitin-kernel queue add --title … --body … --classification …`
- `chitin-kernel queue claim` → returns oldest pending, marks `claimed`
- `chitin-kernel queue complete <id> --pr-url …` / `chitin-kernel queue fail <id> --reason …`
- `chitin-kernel queue list [--status pending|claimed|done|failed]`

**v2: GitHub issues** with label `worker:eligible` on `chitinhq/chitin`. Hides queue state in GitHub's surface; useful when the worker is processing public-issue work. Out of scope for v1 because the local queue is enough to prove the loop.

### Worktree provisioning + seeded settings

Per task, openclaw runs:

```
git worktree add ~/.chitin/worker-worktrees/<task-id> -b worker/<task-id> main
```

Then writes `~/.chitin/worker-worktrees/<task-id>/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash|Edit|Write|NotebookEdit|Read|WebFetch|WebSearch|Task|Glob|Grep|LS|TodoWrite",
      "hooks": [{
        "type": "command",
        "command": "chitin-kernel gate evaluate --hook-stdin --agent=claude-code --worker-mode --task-id=<task-id>"
      }]
    }]
  }
}
```

`--worker-mode` is a new flag on `gate evaluate` that:
- Forces `mode: enforce` regardless of global config (invariant 1).
- Tags every audit row with `worker_task_id`.
- Activates the worker-bootstrap rule set (cold-start safety, below).

### Model routing — per task, no proxy

Classification → `--model` mapping at spawn time:

| Classification | Model | Rough cost | When to use |
|---|---|---|---|
| `mechanical` | `qwen3-coder:30b` (local 3090) | ~free | Renames, deletions of dead code, mechanical refactors, test backfill, doc updates |
| `judgment` | `qwen3-coder:480b-cloud` (Ollama Cloud) | per-token | Anything requiring architectural judgment |
| `judgment+` | `glm-5.1:cloud` | per-token | Reserved for cases where qwen3-coder underperforms on judgment work; default to 480B-cloud first |

The classification lives in the queue entry. Re-classification happens by failing the task and re-queuing with a different `classification` value.

**Why per-task and not per-request:** mid-session model swaps break conversation coherence. Cleaner to scope: one model per task; if mechanical fails its bounds, requeue as judgment.

---

## Cold-start safety: bootstrap rules

The decisions-stream (PR #78) derives candidate rules from observed denials. Worker traffic doesn't exist yet, so the ledger is empty for worker-flavored patterns. Hand-written bootstrap rules carry the load until signal accumulates.

These rules apply when `worker-mode` flag is set on `gate evaluate`:

- **`worker:no-trunk-write`** — deny edits where path is outside the task's worktree (`!path.startsWith(worktree_root)`). Worker boundary enforcement.
- **`worker:no-git-push-non-worker-branch`** — deny `git push` to branches not matching `worker/<task-id>` pattern.
- **`worker:no-pr-merge`** — deny `gh pr merge` from worker mode. Workers open drafts; humans merge.
- **`worker:no-recursive-delete`** — deny `rm -rf` and `find … -delete` patterns. (Already exists as a global bootstrap rule per the spike — verified firing during this session's cleanup.)
- **`worker:no-network-egress-out-of-allowlist`** — deny outbound network calls except: ollama (`localhost:11434`), GitHub API (`api.github.com`), npm/pnpm registries, `pypi.org` for python tasks. Worker shouldn't surprise-talk to arbitrary endpoints.
- **`worker:acpx-spawn-allowlist`** — deny `acpx.spawn` from worker mode for any agent other than `claude-code`. Workers shouldn't spawn `gemini`, `codex`, etc.

These rules are killable once the ledger derives equivalents; the point is not to be drift-naive on day 1.

---

## Observability loop (already mostly shipped)

- Worker emits `session_start` with `worker_task_id` in `payload`.
- Every `pre_tool_use` / `post_tool_use` lands in the chain (PR #1).
- gov-decisions accumulate with `worker_task_id` tag (new field on the audit row).
- Decisions stream (PR #78) reads gov-decisions; with worker tag, can filter to "patterns from autonomous workers vs from humans". The two streams may suggest different rules — autonomous traffic is drift-prone in different ways than human traffic.
- F4 OTEL emit projects everything to existing observability stack (no worker-specific change needed).

The signal worker traffic generates is **exactly what the analysis layer's debt + souls streams (post-talk follow-ons) are designed to consume.** 24/7 worker traffic + chitin governance = the highest-quality input data the analysis pipeline can get. This is the aggregate→policy loop closing on itself: the worker produces the governance signal that improves the rules that govern the worker.

---

## Spike evidence (this session, 2026-04-30)

End-to-end verification before writing the spec, run twice (once via CCR, once direct):

- `claude --print --model qwen3-coder:30b --dangerously-skip-permissions "Use the Bash tool to run: echo direct-ollama-no-ccr"` with `ANTHROPIC_BASE_URL=http://127.0.0.1:11434`.
- Local 3090 served the model.
- chitin gate fired in `enforce` mode.
- gov-decision logged: `{"action_target":"echo direct-ollama-no-ccr", "rule_id":"default-allow-shell", "envelope_id":"01KQDMTYWT14VVHZSJ749WF06E", "tier":"T2", "cost_usd":0.00001875}`.
- Cross-process envelope (cost-gov v3) shared between parent and inner session: same `envelope_id`. The cap mechanism crosses worker process boundaries already.

**Two safety primitives verified firing on real traffic during the same session:**

- Recursive-delete denial: `rm -rf ~/.claude-code-router/` blocked by chitin gate. Confirms `worker:no-recursive-delete` shape works.
- Envelope exhaustion: chitin denied a Read at `calls=800/800`. Confirms invariant 3 (bounded autonomy) is enforceable end-to-end.

---

## Out of scope (v1)

- Multi-worker concurrency (one 3090, one worker — concurrent workers would step on each other on the queue and on the GPU).
- Non-Claude-Code drivers in the worker. Copilot CLI worker is a separate spec; it would use the same openclaw-loop shape but spawn via the Copilot-CLI extension path (post-talk roadmap).
- AI-generated tasks. Workers consume tasks; producing tasks is a separate loop. The decisions-stream's debt+souls follow-ons are the natural producers, and that's a future spec.
- Web UI for queue management. `gh issue` + `chitin-kernel queue list` is enough.
- Ollama-Cloud-only mode (no local 3090). Possible but defeats the thesis — the local-mechanical / cloud-judgment split is the point.

---

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Local model emits malformed tool calls; worker stalls | Wall-clock timeout per task (`bounds.wall_timeout_s`); fail and requeue |
| Cloud escalation runs away with cost | Per-task cost cap (`bounds.max_cost_usd`); envelope hard limit (cost-gov v3) |
| Worker drift produces destructive output before debt ledger has rules | Hand-written bootstrap rules above; gate runs in `enforce` for worker mode |
| openclaw daemon crash → loop dies silently | systemd/launchd unit (`onboard --install-daemon` already does this); worker plugin should heartbeat to chitin so missing heartbeats raise an alert via OTEL |
| Worktree leaks fill disk | Cleanup on success; cap retained worktrees (`max_retained = 5`); old worktrees garbage-collected by a chitin sweep |
| Worker plugin and gate-hook drift apart on schema | `worker_task_id` carried end-to-end in a single typed field; schema lives in `libs/contracts` (zod); Go-side regenerated; openclaw plugin pins the contract version |
| acpx env injection isn't supported | Verify during implementation. Fallback: openclaw plugin shells out to `claude` directly with env set, bypasses acpx for the spawn step |

---

## Open questions

1. **Trunk merging cadence.** Does each task fork a fresh `worker/<task-id>` from current `main`, or does the worker rebase its worktree before each task? Fresh-from-main is simpler for v1 (single worker, no concurrent task collisions). Concurrent-worker future will need a strategy.
2. **Worker identity for git commits.** Per `project_git_identity.md` memory, chitin uses `jpleva91@gmail.com`. Worker commits should be distinguishable from human commits in PR review — proposed: `worker+jpleva91@gmail.com` (Gmail sub-addressing keeps the constraint while marking provenance). Decide v1.
3. **Where worker plugin code lives.** Outside chitin's monorepo entirely (a separate `openclaw-plugin-chitin-worker` repo) keeps openclaw users from having to vendor chitin's full kernel. Inside chitin's monorepo (e.g. `apps/openclaw-plugin/`) keeps the implementation discoverable. Tradeoff is distribution vs cohesion.
4. **Heartbeat schema.** Worker plugin should emit a heartbeat that chitin can monitor. Reuse the chain (a periodic `worker_heartbeat` event type) or a separate channel?
5. **Re-classification policy.** When a mechanical task fails its bounds, what's the auto-policy: requeue as judgment, or hand back to human? Lean: hand back to human in v1 (auto-escalation is itself a policy decision worth deferring until we see real failure modes).

---

## Acceptance criteria (when this spec is "done shipping")

- `chitin-kernel queue add` / `claim` / `complete` / `list` CLIs implemented and tested.
- openclaw worker plugin published (location TBD per open question 3).
- A `worker-mode` gate flag implemented; bootstrap rules above wired and unit-tested against fake gov-decisions traffic.
- One end-to-end task processed by the worker on this box: queue add → openclaw spawn → Claude Code on local 3090 → tool calls gated by chitin → draft PR opened → human merges or rejects.
- Decisions stream (PR #78) shown reading worker-tagged gov-decisions and producing at least one candidate rule from worker traffic.
- `worker_task_id` round-trips end-to-end through the chain and the gov-decisions audit row.
- This spec referenced from `docs/roadmap.md` as the post-talk worker initiative.
