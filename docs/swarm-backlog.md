# Swarm Backlog

Tier-tagged work the local 24/7 swarm chews through. Distinct from `roadmap.md`:
the roadmap is *strategy* (where chitin is going), this doc is *execution*
(what individual issues are ready to grab, sized for which tier).

**Source of authority:** this file. The actual GitHub issues are projections.
When a tier picks up an entry, the workflow records `swarm_backlog_id` in the
chitin event chain so audit can reconcile.

## Tier definitions

| Tier | Driver | Model (post slice 6c) | Use for |
|------|--------|-----------------------|---------|
| **T0** | `local-qwen` | `ollama/qwen3-coder:30b` on the 3090 (free, fast) | mechanical, single-file, <100 LOC |
| **T1** | `copilot` *or* `claude-code-headless` | Copilot GPT-4.1 (free) / `claude-haiku-4-5` | moderate, multi-file, clear pattern |
| **T2** | `local-glm` (rate-limited) *or* `copilot` *or* `claude-code-headless` | `ollama-cloud/glm-5.1:cloud` / Copilot Haiku 4.5 / `claude-haiku-4-5` | specialized reasoning |
| **T3** | `copilot` *or* `claude-code-headless` | Copilot GPT-5.4 / `claude-sonnet-4-6` | heavy / cross-cutting / architectural |
| **T4** | `claude-code-headless` | `claude-opus-4-7` | strongest programmatic — last resort before T5 |
| **T5** | Claude Code interactive (Jared in the loop) | n/a | strategy, ambiguous scope, irreversible decisions; **also: any edit chitin's `no-governance-self-modification` rule blocks** (governance config changes are T5 by design) |

**Activity dispatch (slice 6c):** the activity reads `ExecutionRequest.tier`
and threads `--model <id>` into the spawn args. Maps live in
`apps/temporal-worker/src/activity.ts` (`CLAUDE_TIER_MODEL`,
`COPILOT_TIER_MODEL`). Override per tier per driver via
`CHITIN_MODEL_<DRIVER_KEY>_<TIER>` env. Local-* drivers ignore tier — model
is set per openclaw agent at agent-creation time (slice 3).

**Escalation rule:** when a workflow at tier `T_n` returns non-zero or stalls
past `wall_timeout_s`, Temporal re-enqueues at `T_{n+1}` and tags the issue
`swarm-misclassified-by-T_{n-1}` so we can audit the grooming agent's hit rate.

**Grooming rule:** entries land here only after they're tier-classified. Raw
ideas live in `roadmap.md` ("Deferred") or as draft issues; they cross over
once a grooming pass (Copilot GPT-4.1 free, or interactive Jared+Claude Code)
breaks them down to tier-fit size.

**Self-governance rule (slice 6 lesson):** chitin's
`no-governance-self-modification` rule blocks all agent writes to
`chitin.yaml` and `.chitin/` paths regardless of tier. Governance changes
must come through T5 (a human path). This is a feature, not a friction —
the swarm cannot quietly grant itself broader permissions.

---

## Ready (claimable now)

### `normalize-decision-params-truthiness`

```yaml
id: normalize-decision-params-truthiness
tier: T0
status: ready
estimated_loc: 5
blocks: []
file: apps/openclaw-plugin-governance/src/index.mjs
references_issue: 82
```

`apps/openclaw-plugin-governance/src/index.mjs:48` returns
`decision.params ? { params: decision.params } : undefined`. Empty object `{}`
is truthy → would clobber the agent's args with empty params if the kernel
ever returns that. Fix: `Object.keys(decision.params ?? {}).length > 0`.
Add a test in `bridge.test.ts` covering empty-object case.

---

### `workflow-name-drift-test`

```yaml
id: workflow-name-drift-test
tier: T0
status: ready
estimated_loc: 8
blocks: []
file: apps/temporal-worker/test/activity.test.ts (new file or extend)
references_issue: 82
```

`apps/temporal-worker/src/submit.ts:8` uses `WORKFLOW_NAME = 'executeRequestWorkflow'`
as a string, with `import type { executeRequestWorkflow }` for type safety.
If the export is renamed, the string goes stale silently. Add a unit test
asserting `executeRequestWorkflow.name === WORKFLOW_NAME`.

---

## Qwen-layer reliability (T0→copilot until these ship)

These five entries together aim to flip `TIER_DRIVER[T0]` back from
`copilot` to `local-qwen` in `dispatcher.ts`. Slice 7-tuning's first
live run with `qwen3-coder:30b` on the 3090 surfaced all the gaps; each
entry below targets one. Until they land, T0 routes to Copilot's free
GPT-4.1 — same cost ($0 under Jared's plan), reliable tool dispatch.

### `dispatcher-prompt-relative-path-prefix`

```yaml
id: dispatcher-prompt-relative-path-prefix
tier: T1
status: ready
estimated_loc: 8
blocks: []
file: apps/temporal-worker/src/dispatcher.ts
```

The slice-7-tuning prompt names the entry's `file` field as the
`TARGET FILE`. Live run: qwen3-coder:30b interpreted the relative path
`apps/openclaw-plugin-governance/src/index.mjs` as absolute (prepended
`/`), got `ENOENT` on `/apps/...`. Patch `buildPrompt` to prepend `./`
to the target file so it's an explicit relative path: `./apps/foo`.
Add a test asserting the prompt contains `./` + the path.

---

### `dispatcher-prompt-scope-discipline`

```yaml
id: dispatcher-prompt-scope-discipline
tier: T1
status: ready
estimated_loc: 15
blocks: []
file: apps/temporal-worker/src/dispatcher.ts
```

Slice-7-tuning live run: agent picked `test/bridge.test.ts` instead of
the entry's stated `src/index.mjs` — scope drift. Tighten
`buildPrompt`: forbid editing files not named in the entry's `file`
field, and instruct the agent to `read` ONLY the target file before
editing. Add an integration check post-run: if the diff touches files
outside the entry's `file` list, the apply step refuses to push and
flags scope drift in the chain.

---

### `activity-include-hook-events-flag`

```yaml
id: activity-include-hook-events-flag
tier: T1
status: ready
estimated_loc: 20
blocks: []
file: apps/temporal-worker/src/activity.ts
```

Add `--include-hook-events` to the `claude -p` invocation and the
openclaw `agent` invocation (where supported). When the agent's tool
calls fail (e.g., `ENOENT` on a misinterpreted path), the hook events
in the structured stream-json output give the operator visibility
without grepping verbose stderr. Update activity-types `ActivityResult`
to expose a parsed `hookEvents` summary.

---

### `qwen-ollama-stream-instability-investigation`

```yaml
id: qwen-ollama-stream-instability-investigation
tier: T2
status: ready
estimated_loc: 50
blocks: []
file: docs/observations/2026-05-XX-qwen-ollama-instability.md (new)
```

Slice-7-tuning live run errored: `Ollama API stream ended without a
final response model=qwen3-coder:30b`. Investigate: ollama logs
during the run, GPU memory pressure on the 3090, model load patterns,
ollama version. Output is an observation doc with the failure mode
characterized + a recommended fix (smaller model? quantization? other
local model?). Doesn't touch code — needs T2 reasoning to read logs
and characterize the failure.

---

### `dispatcher-flip-t0-back-to-local-qwen`

```yaml
id: dispatcher-flip-t0-back-to-local-qwen
tier: T0
status: blocked
estimated_loc: 4
blocks: [dispatcher-prompt-relative-path-prefix, dispatcher-prompt-scope-discipline, qwen-ollama-stream-instability-investigation]
file: apps/temporal-worker/src/dispatcher.ts
```

Final entry in the qwen-layer arc. Once the three blockers above ship,
flip `TIER_DRIVER[T0]` from `'copilot'` back to `'local-qwen'` in
dispatcher.ts. Add a smoke-test record showing a productive T0 run
end-to-end on local-qwen. Status `blocked` until the dependencies
land — the dispatcher's `pickEntryToDispatch` doesn't currently
respect blocks (slice 8 work) but a human reviewer will catch a
premature merge.

---

### `repo-regex-tighten`

```yaml
id: repo-regex-tighten
tier: T0
status: ready
estimated_loc: 4
blocks: []
file: libs/contracts/src/execution-request.schema.ts
references_issue: 82
```

`^[^/\s]+\/[^/\s]+$` accepts `..foo/..bar` because `..` matches `[^/\s]+`.
Tighten to forbid leading `.` — e.g., `^[\w][\w.-]*/[\w][\w.-]*$`. Add tests
that reject `../foo`, `..foo/bar`, `foo/../bar`, and accept `chitinhq/chitin`.

---

### `read-vs-read_file-file_path-alias`

```yaml
id: read-vs-read_file-file_path-alias
tier: T0
status: ready
estimated_loc: 6
blocks: []
file: go/execution-kernel/internal/gov/normalize.go
```

Slice 3 added `case "read"` with `path` → `file_path` alias fallback, but the
existing `case "read_file"` has no fallback. Make `read_file` use the same
alias logic for parity. Add a test that `read_file({file_path: "/x"})` and
`read_file({path: "/x"})` produce the same Action.

---

## In design (needs spec or breakdown before claimable)

### `wall-timeout-sigkill-propagation`

```yaml
id: wall-timeout-sigkill-propagation
tier: T2
status: ready
estimated_loc: 60
blocks: []
file: apps/temporal-worker/src/activity.ts
references_issue: 82
references_finding: 11
```

`setTimeout(() => child.kill('SIGKILL'), wall_timeout_s * 1000)` SIGKILLs
openclaw, but openclaw's child processes (model runners) inherit stdout pipes
and keep them open. Node's `'close'` event waits for all pipe FDs to close →
never fires → activity hangs until Temporal's 15-min `startToCloseTimeout`.

Two known-workable fixes; pick one and test:

1. `spawn(cmd, args, { detached: true })` then `process.kill(-pid, 'SIGKILL')`
   on timer (negative pid = process group, kills children too).
2. Force-close stdout/stderr in the timer callback after `child.kill()` —
   `child.stdout.destroy(); child.stderr.destroy()`. Less clean.

Needs an integration test that spawns a process with a hung grandchild and
confirms close fires within ~1s of the timer.

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, two solution paths are given, and integration test requirements are explicit. T2 fits due to process management and test complexity.

Implementation steps:
- Update activity.ts to use spawn with { detached: true } for child processes.
- Modify SIGKILL logic to use process.kill(-pid, 'SIGKILL') for group termination.
- Add fallback to force-close stdout/stderr if needed.
- Write an integration test that spawns a process with a hung grandchild.
- Verify that the 'close' event fires within ~1s after the timer.
- Document the chosen approach and rationale in code comments.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, two solution paths are given, and integration test requirements are explicit. T2 fits due to process management and test complexity.

Implementation steps:
- Update activity.ts to use spawn with { detached: true } for child processes.
- Modify SIGKILL logic to use process.kill(-pid, 'SIGKILL') for group termination.
- Add fallback to force-close stdout/stderr if needed.
- Write an integration test that spawns a process with a hung grandchild.
- Verify that the 'close' event fires within ~1s after the timer.
- Document the chosen approach and rationale in code comments.

### `tools-summary-structured-result`

```yaml
id: tools-summary-structured-result
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: apps/temporal-worker/src/activity-types.ts, src/activity.ts
references_issue: 82
references_finding: 12
```

`ActivityResult.stderr_tail` is a 2000-char string slice that drops the actual
tool list openclaw emits in its verbose JSON. Add a structured field like
`tool_summary?: { calls: number; tools: string[]; failures: number }` and
parse it from the openclaw JSON output (it already emits `toolSummary`).
Surface in the workflow result so reviewers don't have to grep stderr.

---

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear: add a structured field, parse existing JSON, and surface it. Multi-file but straightforward, fits T1.

Implementation steps:
- Locate where ActivityResult is defined and used.
- Add an optional tool_summary field to ActivityResult with the specified structure.
- Update the code that parses openclaw JSON output to extract toolSummary and populate tool_summary.
- Ensure tool_summary is surfaced in the workflow result object.
- Write or update tests to verify tool_summary is correctly parsed and exposed.

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear: add a structured field, parse existing JSON, and surface it. Multi-file but straightforward, fits T1.

Implementation steps:
- Locate where ActivityResult is defined and used.
- Add an optional tool_summary field to ActivityResult with the specified structure.
- Update the code that parses openclaw JSON output to extract toolSummary and populate tool_summary.
- Ensure tool_summary is surfaced in the workflow result object.
- Write or update tests to verify tool_summary is correctly parsed and exposed.

### `cron-subagents-image-granular-targets`

```yaml
id: cron-subagents-image-granular-targets
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: go/execution-kernel/internal/gov/normalize.go
references_issue: 82
```

Slice 3a maps `cron`, `subagents`, `image`, `image_generate` to action types
with `target=toolName` (literal). Loses granular fields. For policy
precision (e.g., "deny `cron action=add` outside business hours"), extract:

- `cron`: schema is `{action, name, schedule, ...}` → target = `<action>:<name>`
- `subagents`: `{action, agentId}` → target = `<action>:<agentId>`
- `image` / `image_generate`: target = path or prompt-prefix

Read each tool's actual schema from openclaw dist before writing.

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, multi-file but pattern-driven. Requires schema lookup and logic update, fits T1. No ambiguity or need for further breakdown.

Implementation steps:
- Review openclaw dist to obtain actual schemas for cron, subagents, image, and image_generate tools.
- Update normalization logic to extract granular target fields per tool type as described.
- Implement target formatting: cron as <action>:<name>, subagents as <action>:<agentId>, image/image_generate as path or prompt-prefix.
- Refactor mapping logic to use new granular targets instead of toolName literal.
- Add/adjust tests to verify correct extraction and mapping for each tool type.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear, multi-file but pattern-driven. Requires schema lookup and logic update, fits T1. No ambiguity or need for further breakdown.

Implementation steps:
- Review openclaw dist to obtain actual schemas for cron, subagents, image, and image_generate tools.
- Update normalization logic to extract granular target fields per tool type as described.
- Implement target formatting: cron as <action>:<name>, subagents as <action>:<agentId>, image/image_generate as path or prompt-prefix.
- Refactor mapping logic to use new granular targets instead of toolName literal.
- Add/adjust tests to verify correct extraction and mapping for each tool type.

### `task-validate-command-pre-activity-gate`

```yaml
id: task-validate-command-pre-activity-gate
tier: T3
status: ready
estimated_loc: 200
blocks: []
file: go/execution-kernel/cmd/chitin-kernel/main.go (new subcommand)
references_spec: docs/superpowers/specs/2026-04-30-local-worker-design-addendum.md
```

Spec addendum says: "Before Temporal dispatches the activity, chitin validates
the request — `chitin-kernel task validate <req.json>` — and may narrow
`allowed_drivers`." Subcommand doesn't exist yet. Slice 1 `submit.ts` zod-
parses locally and posts straight to Temporal — no policy narrowing.

Needs:
1. New `task` subcommand group with `validate` (and later `submit`)
2. Reads ExecutionRequest from stdin or file
3. Returns narrowed request (or rejection) on stdout
4. Wire `submit.ts` to shell out to it before `client.workflow.start`
5. Tests for narrow / reject / passthrough cases

T3 because cross-cutting (Go kernel + TS submit + Temporal flow + spec
alignment).

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: new CLI subcommand, Go/TS integration, and test cases. Cross-cutting but well-defined; ready for T3 claim.

Implementation steps:
- Add new 'task' subcommand group to chitin-kernel CLI in Go
- Implement 'validate' subcommand: read ExecutionRequest from stdin/file, apply policy narrowing logic
- Output narrowed or rejected request to stdout in correct format
- Update submit.ts to shell out to 'chitin-kernel task validate' before Temporal workflow start
- Write tests for narrow, reject, and passthrough scenarios
- Align implementation with referenced spec addendum

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: new CLI subcommand, Go/TS integration, and test cases. Cross-cutting but well-defined; ready for T3 claim.

Implementation steps:
- Add new 'task' subcommand group to chitin-kernel CLI in Go
- Implement 'validate' subcommand: read ExecutionRequest from stdin/file, apply policy narrowing logic
- Output narrowed or rejected request to stdout in correct format
- Update submit.ts to shell out to 'chitin-kernel task validate' before Temporal workflow start
- Write tests for narrow, reject, and passthrough scenarios
- Align implementation with referenced spec addendum

### `chitin-install-slice-3-agents`

```yaml
id: chitin-install-slice-3-agents
tier: T2
status: ready
estimated_loc: 80
blocks: []
file: go/execution-kernel/cmd/chitin-kernel/main.go (extend install)
```

PR #84's slice-3 demo required the operator to manually run
`openclaw agents add qwen-agent --model ollama/qwen3-coder:30b ...` and would
need the same for `glm-agent` and `deepseek-agent`. Reproducing this on every
new install is friction. Add a `chitin-kernel install --slice-3-agents`
flag (or `chitin-kernel openclaw bootstrap-agents`) that idempotently
ensures the three per-driver agents exist with the correct model bindings.

T2 because the right model per driver depends on local stack availability
(checking ollama / ollama-cloud / Copilot CLI presence and credentials).

---

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: add a flag/command to automate agent setup, with logic for stack detection and idempotency. No further breakdown needed.

Implementation steps:
- Add a --slice-3-agents flag to chitin-kernel install (or a new bootstrap-agents command).
- Detect local model stack availability (ollama, ollama-cloud, Copilot CLI, credentials).
- For each agent (qwen, glm, deepseek), determine the correct model binding based on stack.
- Check if each agent already exists; if not, create it with the correct model.
- Ensure idempotency: re-running does not duplicate or misconfigure agents.
- Add logging for actions taken and skipped.
- Test with various stack configurations to verify correct agent setup.

**Groomed 2026-05-02 (0.95 confidence):** Scope is clear: add a flag/command to automate agent setup, with logic for stack detection and idempotency. No further breakdown needed.

Implementation steps:
- Add a --slice-3-agents flag to chitin-kernel install (or a new bootstrap-agents command).
- Detect local model stack availability (ollama, ollama-cloud, Copilot CLI, credentials).
- For each agent (qwen, glm, deepseek), determine the correct model binding based on stack.
- Check if each agent already exists; if not, create it with the correct model.
- Ensure idempotency: re-running does not duplicate or misconfigure agents.
- Add logging for actions taken and skipped.
- Test with various stack configurations to verify correct agent setup.

### `openclaw-tool-coverage-audit`

```yaml
id: openclaw-tool-coverage-audit
tier: T1
status: ready
estimated_loc: 40
blocks: []
file: docs/observations/2026-05-XX-openclaw-tool-coverage.md (new)
```

Slice 3a + 3-fix mapped 21 openclaw tool names. PR #84's adversarial pass
caught that `web_search` / `web_fetch` (plain forms) were missing. Other
extensions might register tools we haven't enumerated. Write a script that
greps openclaw's dist for `name: "[a-z_]+"` in tool-registration call sites,
diffs against `gov.Normalize`'s switch cases, and reports missing mappings.
Run it as a CI check eventually.

---

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear, single-purpose, and multi-file but not complex. Steps are concrete and fit T1. No further breakdown needed.

Implementation steps:
- Grep openclaw's dist for tool-registration call sites with name: "[a-z_]+"
- Extract all tool names found in registration calls
- Parse gov.Normalize's switch cases to collect mapped tool names
- Diff the two sets to find unmapped tool names
- Output a report listing missing mappings

**Groomed 2026-05-02 (0.95 confidence):** The scope is clear, single-purpose, and multi-file but not complex. Steps are concrete and fit T1. No further breakdown needed.

Implementation steps:
- Grep openclaw's dist for tool-registration call sites with name: "[a-z_]+"
- Extract all tool names found in registration calls
- Parse gov.Normalize's switch cases to collect mapped tool names
- Diff the two sets to find unmapped tool names
- Output a report listing missing mappings

### `swarm-shared-memory-spike` (decomposed)

```yaml
id: swarm-shared-memory-spike
status: decomposed
decomposed_into: [event-chain-query-api, session-context-injector, failure-mode-logging]
decomposed_at: 2026-05-02
```

Today's swarm: each workflow is a fresh agent with no memory of previous
runs. Real cost to find out: qwen redoes setup work / re-fetches context /
re-derives decisions every invocation. claude-mem (or similar — chitin's
existing event chain has the data already, just not the retrieval API) is
the most defensible answer. Spike: query the chain for "what did this agent
last do for this repo" and inject as session-start context. T2 because the
right shape depends on what failure modes show up first — needs a week of
real swarm runs before we know.

---

**Groomed:** The spike's scope is cross-cutting and exploratory; needs decomposition into API, injection, and failure analysis before a concrete, claimable task emerges.

**Groomed:** The spike's scope is cross-cutting and exploratory; needs decomposition into API, injection, and failure analysis before a concrete, claimable task emerges.

### `event-chain-query-api`

```yaml
id: event-chain-query-api
tier: T1
status: in_design
parent: swarm-shared-memory-spike
```

Expose API to query event chain for agent/repo history

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

### `session-context-injector`

```yaml
id: session-context-injector
tier: T1
status: in_design
parent: swarm-shared-memory-spike
```

Inject retrieved memory into agent session start

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

### `failure-mode-logging`

```yaml
id: failure-mode-logging
tier: T2
status: in_design
parent: swarm-shared-memory-spike
```

Log and analyze failure modes from real swarm runs

(Decomposed from `swarm-shared-memory-spike` on 2026-05-02.)

---

### `rename-local-cloud-driver-misnomer`

```yaml
id: rename-local-cloud-driver-misnomer
tier: T2
status: ready
estimated_loc: 60
blocks: []
file: libs/contracts/src/execution-request.schema.ts, apps/temporal-worker/src/activity.ts, openclaw agent config
```

`local-glm` and `local-deepseek` driver ids are misnomers — `glm-5.1:cloud`
runs through Ollama Cloud and `deepseek` (in our setup) routes via cloud
too, neither is "local" in the same sense as `local-qwen` (which actually
runs on the 3090). Renaming options:

- `cloud-glm`, `cloud-deepseek` — paired with `local-qwen` keeps the
  prefix discoverable but introduces a third axis (cost / latency tier
  vs locality) the prefix doesn't fully capture.
- `glm`, `deepseek` (no prefix) + keep `local-qwen` as an exception —
  cleanest but breaks the convention.
- Tier-suffix vocabulary entirely: drop `local-*` and just name agents
  by model (`qwen-coder`, `glm-cloud`, `deepseek-cloud`) — biggest
  rename surface area, cleanest end state.

Touches: `DriverIdSchema` enum, `DRIVER_AGENT_MAP` in activity.ts, the
per-driver agent ids in openclaw config (`qwen-agent`, `glm-agent`,
`deepseek-agent` may also need renaming for consistency), CHITIN_AGENT_*
env var keys, all activity tests, `swarm-backlog.md` tier definitions
above. T2 because of the breadth (multi-file rename + downstream env
var docs).

---

## Strategic / user-only (T4)

These need Jared + Claude Code interactive — too ambiguous for any tier
below to groom further.

- **Slice 4 scope decision** — what's after slice 3? The roadmap-as-shipped
  doesn't define a slice 4. Options on the table: Copilot CLI v2 spike
  (post-talk per memory), terrain-B compute-fabric, A2/A4 audience expansion.
  Strategy call, not swarm work.
- **OTEL semconv full compliance** — `gen_ai.*` deferred per roadmap. Big
  scope, business value depends on talk reception.
- **octi v2 spec edits** — pre-plan-handoff, listed in roadmap deferred.

---

## Recently shipped (drop after 2 sprints)

- `slice-1-temporal-worker` — PR #81, merged 2026-05-01
- `slice-2-openclaw-plugin` — PR #81 (same), merged 2026-05-01
- `pr-81-tos-driver-fix` — `claude-code` removed from `DriverIdSchema`,
  PR #81 commit
- `slice-3a-pi-runtime-core-tools` — PR #83, merged 2026-05-01
- `slice-3-chat-domain-and-routing` — PR #84, merged 2026-05-01
- `slice-4-grooming-agent` — PR #92, merged 2026-05-02
- `slice-5-swarm-worktree` — PR #93, merged 2026-05-02
- `slice-5b-claude-code-headless` — PR #95, merged 2026-05-02 (corrected
  the 2026-04-30 ToS misread; brought claude-code back as a worker driver)
- `slice-6-cheaper-driver-gating-and-tier-routing` — PR #96, merged
  2026-05-02 (closed the audit-gap finding from slice 5b)
- `gov-policy-allow-pr-merge` — PR #97, merged 2026-05-02 (manual; can't
  go through swarm by self-governance rule)
- Closed from issue #82: `#4 driver-id-contract-theater` (slice 3b),
  `#13 normalizer-informational` (PR #83)
- Closed audit-gap PR #94 — superseded by #97 (its content was correct
  but it was produced by an unaudited slice-5b run, before slice 6 fixed
  the cwd-scoped hook gap)

---

## Tier counts (snapshot 2026-05-02 post slice-6 merge)

```
T0 ready:    4   (decision.params, workflow-name-drift, repo-regex, read/read_file alias)
T1 ready:    3   (tools-summary, cron-targets, openclaw-tool-coverage)
T2 ready:    3   (wall-timeout-sigkill, install-slice-3-agents, rename-cloud-misnomer)
T3 ready:    1   (task-validate command)
T1 in_design: 2  (event-chain-query-api, session-context-injector — sub-entries
                  of swarm-shared-memory-spike)
T2 in_design: 1  (failure-mode-logging — same parent)
T4 strategic: 3  (slice-7 scope, OTEL semconv, octi v2)
T5 only:     ∞  (governance-config edits, ambiguous strategy)
```

**Recommended next-session sequence (cheap → expensive):**

1. **Drain T0 ready (4 entries)** via `local-qwen` — free, ~5 min each.
   The slice-6 worktree path makes this straightforward; one workflow
   per entry, apply step PRs each. Each PR is single-file mechanical.
2. **Run a grooming pass on the T1/T2 in_design sub-entries** to flesh
   out implementation steps so they're claimable.
3. **Pick one T2 ready** — `wall-timeout-sigkill` is highest value
   because it unblocks slow models from timing out at Temporal's
   15-min cap. `rename-cloud-misnomer` is lower value but smaller.

**How to dispatch a T0 from this backlog:**

```bash
# Worker must be running:
CHITIN_REPO_ROOT=/home/red/workspace/chitin \
  pnpm exec tsx apps/temporal-worker/src/worker.ts &

# Submit:
PROMPT='<from swarm-backlog entry implementation_steps>' \
WORKFLOW_ID=swarm-<entry-id>-$(date +%s) \
BASE_REF=main DRIVER=local-qwen TIER=T0 \
WALL_TIMEOUT_S=120 MAX_TOOL_CALLS=10 \
  pnpm exec tsx apps/temporal-worker/src/submit.ts

# Apply:
pnpm exec tsx apps/temporal-worker/src/grooming/apply-workflow-result.ts \
  --result tmp/result-<workflow-id>.json --apply
```
