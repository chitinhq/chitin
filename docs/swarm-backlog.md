# Swarm Backlog

Tier-tagged work the local 24/7 swarm chews through. Distinct from `roadmap.md`:
the roadmap is *strategy* (where chitin is going), this doc is *execution*
(what individual issues are ready to grab, sized for which tier).

**Source of authority:** this file. The actual GitHub issues are projections.
When a tier picks up an entry, the workflow records `swarm_backlog_id` in the
chitin event chain so audit can reconcile.

## Tier definitions

| Tier | Driver | Model | Use for |
|------|--------|-------|---------|
| **T0** | `local-qwen` | `ollama/qwen3-coder:30b` (3090) | mechanical, single-file, <100 LOC. Free, fast. |
| **T1** | `copilot` | GPT-4.1 (free) or Haiku | moderate, multi-file, clear pattern. Cheap. |
| **T2** | `local-glm` *or* `copilot` | `ollama-cloud/glm-5.1:cloud` *or* Copilot mid | specialized reasoning. glm rate-limited so use sparingly. |
| **T3** | `copilot` | GPT-5.4 | heavy / cross-cutting / architectural. Workhorse. |
| **T4** | Claude Code (interactive, with Jared) | n/a | strategy, ambiguous scope, irreversible decisions, anything the swarm can't groom further. |

**Escalation rule:** when a workflow at tier `T_n` returns non-zero or stalls
past `wall_timeout_s`, Temporal re-enqueues at `T_{n+1}` and tags the issue
`swarm-misclassified-by-T_{n-1}` so we can audit the grooming agent's hit rate.

**Grooming rule:** entries land here only after they're tier-classified. Raw
ideas live in `roadmap.md` ("Deferred") or as draft issues; they cross over
once a grooming pass (Copilot GPT-4.1 free, or interactive Jared+Claude Code)
breaks them down to tier-fit size.

---

## Ready (claimable now)

### `gov-policy-allow-pr-merge`

```yaml
id: gov-policy-allow-pr-merge
tier: T0
status: ready
estimated_loc: 3
blocks: []
file: chitin.yaml
```

`default-allow-github-write` rule lists `github.pr.create` and `pr.close` but
omits `pr.merge`. Result: every `gh pr merge` invocation by claude-code or
swarm dispatcher gets denied with `policy default is deny` and we have to
fall back to `gh api PUT /pulls/N/merge`. Add `github.pr.merge` to the rule's
action list in `chitin.yaml`. Bound by existing escalation policy — not a
broadening, just closing a vocabulary gap that should always have been allowed.

---

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
status: in_design
estimated_loc: 30-60
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

### `tools-summary-structured-result`

```yaml
id: tools-summary-structured-result
tier: T1
status: in_design
estimated_loc: 20
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

### `cron-subagents-image-granular-targets`

```yaml
id: cron-subagents-image-granular-targets
tier: T1
status: in_design
estimated_loc: 30
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

### `task-validate-command-pre-activity-gate`

```yaml
id: task-validate-command-pre-activity-gate
tier: T3
status: in_design
estimated_loc: 200+
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

### `chitin-install-slice-3-agents`

```yaml
id: chitin-install-slice-3-agents
tier: T2
status: in_design
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

### `openclaw-tool-coverage-audit`

```yaml
id: openclaw-tool-coverage-audit
tier: T1
status: in_design
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

### `swarm-shared-memory-spike`

```yaml
id: swarm-shared-memory-spike
tier: T2
status: in_design
estimated_loc: 100+
blocks: []
file: docs/superpowers/specs/2026-05-XX-swarm-shared-memory-spike.md (new)
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
- Closed from issue #82: `#4 driver-id-contract-theater` (slice 3b),
  `#13 normalizer-informational` (PR #83)

---

## Tier counts (snapshot)

```
T0: 5 ready
T1: 3 in_design
T2: 4 in_design (1 wall-timeout, 1 install, 1 audit, 1 shared-memory spike)
T3: 1 in_design (task-validate command)
T4: 3 strategic
```

Bias: there's a lot of T0 ready right now — good warmup load for the swarm.
Once those are drained, T1 entries need a grooming pass to break them into
implementation steps before they're claimable.
