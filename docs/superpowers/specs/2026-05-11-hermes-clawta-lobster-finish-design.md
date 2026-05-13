---
status: open
owner: jared
kanban: null
implementation_pr: null
superseded_by: null
effective_from: '2026-05-11'
effective_to: null
---

# Hermes → Clawta → Lobster → Frontier-Coder CLI — finish design

Date: 2026-05-11
Status: spec
Author: Jared (with Knuth-lens design pass)

## Goal

Finish wiring the operator-to-execution chain so that every frontier-coder
turn that originates at Hermes flows through exactly one path:

```
hermes → clawta → openclaw (kanban-dispatch.lobster) → frontier-coder CLI
```

with chitin emitting a chain event at every hop, keyed by a non-empty
`CHITIN_DRIVER`, and every `allowed:false` decision hard-blocking at the
leaf CLI.

"Finished" means: all four frontier-coder CLIs (claude-code, codex,
gemini, copilot) can be dispatched as Lobster workers via
`kanban-dispatch.lobster`, end-to-end, with chitin governance observed
AND enforced at every hop.

## Invariants

The design must force the following invariants. Each slice below either
establishes one or depends on one.

1. **Single-path invariant.** No `shell.exec` from `driver=hermes` can
   invoke a frontier-coder binary (`codex`, `claude`, `gemini`) directly.
   Copilot is the explicit exception, gated upstream by openclaw acpx.

2. **Driver-identity invariant.** Every chain event has a non-empty
   `CHITIN_DRIVER` matching the surface that produced it
   (`hermes` | `clawta` | `codex` | `claude-code` | `gemini`).

3. **Enforcement invariant.** Every chain event with `allowed:false`
   corresponds to an actually-blocked tool execution at the leaf CLI.
   Logging a deny without blocking the call is the violation codex's
   investigation surfaced today; closing that gap is the gating slice
   for the rest of the work.

4. **Single-role invariant (v1).** The Lobster pipeline assigns exactly
   one worker per ticket. No `programmer/reviewer/tester` split in v1;
   that is growth, not a finish-line requirement.

## Architecture

### The four hops

```
hermes
  │  PreToolUse hook (hermes-plugin) → chitin-router-hook --agent=hermes
  │  DENY: shell.exec of codex|claude|gemini binaries
  ▼
clawta  (/home/red/.local/bin/clawta — shell wrapper)
  │  exec openclaw agent --agent glm-agent --message "<intent>"
  ▼
openclaw glm-agent (Clawta on glm-5.1:cloud)
  │  runs kanban-dispatch.lobster workflow:
  │    fetch_ticket → classify → pick_driver → confirm →
  │    reassign → audit_comment → spawn_worker
  │  openclaw-plugin-governance gates each agent turn (outer hop)
  ▼
spawn_worker (Lobster step)
  │  reads ~/.openclaw/data/agent-cards/<id>.json
  │  substitutes {model} + {prompt} into card.invocation
  │  execs leaf CLI via chitin-router-hook-gated shell.exec
  ▼
leaf CLI  (claude | codex | gemini → PreToolUse/BeforeTool hook
           copilot                  → acpx-mediated, no PreToolUse)
  │  inner-hop chain events per tool call
  │  CHITIN_DRIVER ∈ { claude-code | codex | gemini }
  ▼
tool execution
```

### Why hybrid (agents upstream, hooks downstream)

Agent identity and runtime invocation live in openclaw's data path
(agent cards + Lobster workflow). Chitin contributes:

- The openclaw-plugin-governance gate at agent-turn boundaries
- `chitin-router-hook` at each inner tool call via each CLI's native
  hook protocol
- Driver-keyed policy rules in `chitin.yaml`
- Chain-event emission at every hop

No new TypeScript adapter packages. The frontier-coder CLIs are
spawned by Lobster's `spawn_worker` step using the existing
`card.invocation` template — no new abstraction layer is needed.

### Ground truth verified on 2026-05-11

- `~/.openclaw/workflows/kanban-dispatch.lobster` exists in scaffold
  form (154 lines, with explicit TODO markers from today's smoke run).
- `~/.openclaw/data/agent-cards/{claude-code,codex,gemini,copilot}.json`
  all exist with `id`, `capabilities`, `models`, `invocation` fields.
- `_pick_driver.py` filters by capability and ranks by cheapest cost.
- Hooks installed and confirmed via chain events:
  - claude-code: `~/.claude/settings.json` PreToolUse →
    `chitin-router-hook --agent=claude-code` ✅
  - gemini: `~/.gemini/settings.json` BeforeTool →
    `chitin-router-hook --agent=gemini` ✅
  - codex: `~/.codex/config.toml` `[[hooks.PreToolUse]]` →
    `chitin-router-hook --agent=codex` ✅ (the TOML config, NOT
    `hooks.json` which only contains `atuin hook codex`)
- `openclaw agents add` natively supports only model-provider agents
  (e.g., `ollama-cloud/glm-5.1:cloud`). It does NOT support spawning
  a CLI as runtime. The agent-card + Lobster `spawn_worker` path is
  the correct mechanism, not `openclaw agents add`.

## Work list

### Slice 0 — Commit today's foundation

Gating precondition for everything below.

**Changes (already in working tree, uncommitted):**
- `bin/chitin-router-hook`: stamps `CHITIN_DRIVER` from `--agent=<cli>`
  flag, defers to caller-set value if already exported.
- `chitin.yaml`: `hermes-no-frontier-spawn` rule denies `driver:hermes`
  `shell.exec` matching the regex
  `(?:^|[;&|]\s*|\s)(?:[\w./-]+/)?(?:codex|claude|gemini)(?:\s|$)`.

**New test:** `chitin.yaml` policy-eval test with two cases:
- `driver=hermes` + `shell.exec` of `codex|claude|gemini` → deny
- `driver=hermes` + `shell.exec` of `clawta` → allow

**Acceptance:** PR merges to main with policy-eval test passing.

### Slice 1 — Close the enforcement leak (the real gate)

Codex's investigation on 2026-05-11 found two distinct failure modes:

1. `chitin-router-hook` exited with the block code on at least one deny
   path without writing the rule + reason to stderr ("PreToolUse hook
   exited with code 2 but did not write a blocking reason to stderr").
2. A denied tool call (rule `governance-mutation-authority-required`)
   was logged as `allowed:false` in
   `~/.chitin/gov-decisions-2026-05-11.jsonl` but the command still
   produced output — i.e., the deny was observed, not enforced.

**Work:**

- Audit every code path in `bin/chitin-router-hook` (and the Go gate it
  calls) that exits with the block code. Each path MUST write
  `rule_id: <id>\nreason: <text>\n` to stderr before exiting.
- Add per-CLI conformance test: trigger a known deny rule via each
  leaf CLI; assert both (a) chain event has `allowed:false` AND (b)
  leaf CLI did not execute the underlying tool. One test each for
  claude-code, codex, gemini. (Copilot's leg is acpx-mediated and
  tested separately at the openclaw-plugin-governance boundary.)
- If codex's hook ABI turns out to require JSON output rather than
  exit-2-plus-stderr for hard-block semantics, branch the hook output
  format on `$CHITIN_DRIVER` and document the asymmetry in
  `bin/chitin-router-hook` header comments.

**Acceptance:** the conformance test passes for all three CLIs that
have a PreToolUse-class hook. Without this, Slices 2–5 ship
observation, not enforcement.

### Slice 2 — Wire the `classify` step to clawta

`kanban-dispatch.lobster` step `classify` is currently a hardcoded JSON
stub. The existing TODO in the file recommends option (b): refactor to
a `clawta --text` shell call.

**Work:**

Replace:

```yaml
- id: classify
  description: Classify ticket (SCAFFOLD stub - hardcoded)
  run: >
    echo '{"complexity":"low","capabilities":["go","review"],...}'
```

with:

```yaml
- id: classify
  description: Classify ticket via clawta
  run: >
    clawta --text "Classify this ticket and reply with ONLY a JSON
    object (no prose): {\"complexity\": \"low|med|high\",
    \"capabilities\": [\"go\"|\"ts\"|\"python\"|\"refactor\"|...],
    \"estimated_loc\": <int>, \"needs_frontier\": <bool>}.
    Ticket: $fetch_ticket.stdout"
```

**Acceptance:** smoke run produces a valid JSON classification that
`_pick_driver.py` accepts as input.

### Slice 3 — Fix step-output interpolation

Today's smoke run discovered that args (`${ticket_id}`) interpolate
correctly in shell `run:` and `approval:` strings, but step outputs
(`$pick_driver.json.driver`) do NOT.

**Work:**

- Read Lobster's source/docs to confirm the correct mechanism. Three
  candidates from the existing TODO:
  - `$pick_driver.json` syntax (without braces) inline
  - A `parse:` step that explodes JSON into named workflow-scope vars
  - Env-var injection: `$STEP_<id>_STDOUT` style
- Apply uniformly across `confirm`, `reassign`, `audit_comment`,
  `spawn_worker`.
- Document the working pattern in the `kanban-dispatch.lobster`
  header comment so this isn't rediscovered.

**Acceptance:** smoke run completes all steps without "Bad
substitution" or literal-template-text-in-output bugs.

### Slice 4 — Implement `spawn_worker`

`spawn_worker` step is currently a stubbed echo. It must read the
picked agent's card, substitute `{model}` and `{prompt}` into the
`invocation` template, and exec the leaf CLI through chitin's gate.

**Work:**

- Read `~/.openclaw/data/agent-cards/<driver>.json` where `<driver>`
  is the output of `pick_driver`.
- Pick the appropriate model from `card.models` (default: cheapest
  model that meets `needs_frontier`).
- Substitute `{model}` → chosen model id, `{prompt}` → ticket body
  (escaped for shell), into `card.invocation.args`.
- Exec `card.invocation.cmd <substituted-args>`. The shell.exec
  itself is gated by the clawta-driver hook (chain hop). The leaf
  CLI's own PreToolUse/BeforeTool hook handles inner-hop events.

**Acceptance:** running `kanban-dispatch` with a real ticket spawns
the correct leaf CLI with correct args; chain ledger shows the
spawn event with `driver=clawta` and subsequent inner events with
`driver=<cli>`.

### Slice 5 — End-to-end smoke test (one card at a time)

Run `kanban-dispatch.lobster` once for each of the four agent cards
and verify the chain ledger.

**Per-CLI acceptance:**

For `claude-code`, `codex`, `gemini`:

1. Seed a test ticket on the hermes kanban board.
2. Run:
   ```
   pnpm exec lobster run \
     --file ~/.openclaw/workflows/kanban-dispatch.lobster \
     --args-json '{"ticket_id":"<seeded>"}'
   ```
3. Approve at the `confirm` step.
4. Assert chain ledger shows the sequence:
   - clawta entry event (`driver=clawta`)
   - openclaw agent-turn event (`driver=clawta`, outer hop)
   - `classify`, `pick_driver`, `reassign`, `audit_comment`,
     `spawn_worker` step events
   - N × inner tool-call events with `driver=<cli>`
   - Worker completion event
5. Assert no events with `allowed:false` were followed by tool
   execution.

For `copilot`:

Same as above except step (4) has zero inner tool-call events (copilot
has no PreToolUse surface). This is the known telemetry asymmetry —
documented, not a bug. The copilot leg is gated at the
openclaw-plugin-governance boundary only.

## Non-goals (v1)

- Multi-role pipelines (programmer + reviewer + tester). Single role
  per ticket in v1.
- Reviewing or testing the worker's PR output. The `kanban-dispatch`
  workflow ends at `spawn_worker`. PR review and merge gating are
  separate workflows.
- Replacing copilot's acpx gating mechanism. Acpx is the right tool
  for copilot's protocol; chitin's job is to keep the upstream
  governance plugin in place.
- Solving the `llm.invoke --provider openclaw` 404 (tool not
  registered upstream). Slice 2 routes around it via clawta.
- Auto-merge of worker output. Per
  `project_self_improving_swarm_landscape_2026.md`: every 2026
  swarm system keeps a human on merge.

## Known asymmetries (documented, not bugs)

- **Copilot has no inner-hop chain events.** Its ACP protocol has no
  PreToolUse surface; gating happens at the agent-turn boundary via
  openclaw-plugin-governance.
- **Hermes may directly invoke copilot.** Explicit exception in
  `hermes-no-frontier-spawn` for copilot as Hermes's sanity-check
  helper.

## Open questions to verify during implementation

- Does codex's hook ABI honor exit-2-plus-stderr the same way Claude
  Code's does, or does it need JSON output? Slice 1 resolves this
  empirically.
- What is Lobster's correct step-output interpolation syntax? Slice 3
  resolves this by reading source/docs.
- For `spawn_worker`, should the leaf CLI run in an isolated worktree
  (per the openclaw pattern `pipeline:<project>:<role>`) or in the
  current workspace? Current scaffold doesn't specify; pick one in
  Slice 4 implementation and document.

## Dependencies and ordering

```
Slice 0 ── (commit foundation)
   │
   ▼
Slice 1 ── (close enforcement leak — gating)
   │
   ├──► Slice 2 ── (classify via clawta)
   │       │
   ▼       ▼
Slice 3 ── (fix interpolation)
   │
   ▼
Slice 4 ── (implement spawn_worker)
   │
   ▼
Slice 5 ── (end-to-end smoke test)
```

Slices 2 and 3 are independent of each other; both depend on Slice 1.
Slices 4 and 5 are strictly sequential after that.

## References

- Today's working-tree diff: `bin/chitin-router-hook` (CHITIN_DRIVER
  stamping), `chitin.yaml` (hermes-no-frontier-spawn rule).
- Existing scaffold: `~/.openclaw/workflows/kanban-dispatch.lobster`,
  `~/.openclaw/data/agent-cards/*.json`,
  `~/.openclaw/workflows/_pick_driver.py`.
- Codex investigation findings: 2026-05-11 session
  `019e1849-5b78-7e90-9181-691cccd314e6`; events at
  `~/.chitin/events-019e1849-5b78-7e90-9181-691cccd314e6.jsonl`;
  gov decisions at `~/.chitin/gov-decisions-2026-05-11.jsonl`.
- Architectural priors: three-plane architecture (Temporal control /
  OpenClaw execution / Chitin enforcement); two-driver design pattern
  (open vendors = in-process extension; closed = wrapping
  orchestrator); driver+model tier map (2026-05-04).
