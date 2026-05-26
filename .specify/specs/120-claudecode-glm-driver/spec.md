---
spec_id: 120
title: claudecode-glm driver — Claude Code CLI + local glm-5.1 via `ollama launch claude` for zero-cost whole-spec dispatch
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 075
  - 119
related:
  - 070
  - 094
  - 113
  - 116
---

# Spec 120 — claudecode-glm driver

## Why

Spec 119 (#1124, merged 2026-05-26) made `--whole-spec` the default
dispatcher mode. The first validation dispatch picked the existing
claudecode driver (which routes to opus-4.7) and was cancelled
because per-invocation cost at the whole-spec context-payload scale
(full spec.md + tasks.md + plan.md) is prohibitive at our dispatch
rate. PR #1127 stripped `CapSpecImplement` from the default claudecode
driver; codex (gpt-5.x-codex) is now the sole declarer.

This leaves the loop dependent on a single paid cloud model. A
zero-cost local-model alternative gives the swarm:
  - a fallback when codex is rate-limited or quota-exhausted
  - the first concrete step toward the local-LLM autonomy thesis
    (memory: project_chitin_end_goal_local_llm_safety)
  - measurable cost telemetry — operators see "what % of dispatches
    ran local vs cloud" and can tune the routing preference

The integration is already trivial because ollama 0.21+ ships a
built-in `ollama launch claude` integration. Verified on the
operator-host (2026-05-25):

```
$ ollama launch claude --help
Examples:
  ollama launch claude
  ollama launch claude --model <model>
  ollama launch codex -- -p myprofile (pass extra args to integration)
```

Inspecting the `ollama` binary's string table:
  - `ANTHROPIC_BASE_URL=` — set automatically to the local gateway
  - `ANTHROPIC_AUTH_TOKEN=ollama` — set automatically
  - `CLAUDE_CODE_SUBAGENT_MODEL=` — set automatically per `--model`
  - `gateway did not start on %s` — ollama runs its own gateway,
    no separate proxy install needed

So the driver is just: `ollama launch claude --model <local-model> -- <extra-claude-args>`.
The gateway, the env setup, the model routing are all handled by
ollama. The chitin driver shell out a single command. No litellm
install. No systemd unit. No yaml config.

This spec ships a `claudecode-glm` driver that:
  - Shells out to `ollama launch claude --model <env-configured-model>`
    passing the same `--print` / prompt args the existing claudecode
    driver uses
  - Declares `CapSpecImplement` so the whole-spec router treats it
    as a peer of codex
  - Tier=TierLocal + CostClass=CostZero so future cost-aware routing
    can prefer it when Ready

Composability: the same Claude Code harness (skill loading, MCP,
hooks, factory-loop integration) wired to a free local model. The
driver becomes the reference implementation for any future local
model — same pattern, different `--model`.

## User stories

### US1 (P1) — claudecode-glm declares CapSpecImplement and routes for whole-spec dispatches

> As the operator running `chitin-orchestrator schedule <spec-ref>`
> in `--whole-spec` mode, the scheduler picks between codex (cloud,
> paid) and claudecode-glm (local, free). When claudecode-glm is
> Ready (ollama running, target model pulled), it is a valid
> CapSpecImplement candidate alongside codex.

**Independent test:** Schedule a small spec under `--whole-spec`
with the registry-allow filter pinned to claudecode-glm
(`CHITIN_DRIVER_ALLOW=claudecode-glm`). Inspect the
`scheduler_started` chain event for `driver_id: "claudecode-glm"`.
The work-unit must produce a PR or surface a typed failure status.

### US2 (P1) — claudecode-glm's Ready() returns false when ollama is down or the target model is missing

> As the scheduler routing the whole-spec work unit, the registry's
> selection layer MUST skip claudecode-glm when ollama isn't running
> OR the target model isn't pulled. Routing falls back to codex
> transparently.

**Independent test:** Stop the ollama service. Schedule a dispatch
with claudecode-glm + codex both registered. claudecode-glm.Ready()
returns false with a one-line reason; the scheduler picks codex.
Restart ollama; the next dispatch is again multi-candidate. Verify
likewise that with ollama running but the configured model not
present (`ollama list` doesn't show it), Ready returns false.

### US3 (P2) — Tier=Local + CostClass=Zero on the capability card

> As the future cost-aware router (a follow-up spec; today's
> selector doesn't read cost), claudecode-glm advertises itself as
> Tier=Local + CostClass=Zero so a future preference rule "prefer
> Zero cost when Ready" naturally picks the local driver.

**Independent test:** Inspect claudecode-glm's CapabilityCard via
`driver.Registry.Drivers()` — assert `Tier == driver.TierLocal` and
`CostClass == driver.CostZero`.

## Functional requirements

- **FR-001** A new driver package `go/orchestrator/driver/claudecodeglm/`
  implements `driver.AgentDriver` (spec 075 FR-001). The driver shells
  out to `ollama launch claude` (NOT `claude` directly); ollama
  handles the gateway + env setup. Where invocation logic can be
  factored with the existing claudecode driver (worktree handling,
  prompt assembly, output parsing), share via a small `claudecodeshared`
  helper rather than copy-paste.

- **FR-002** The driver's `Card()` returns:
    - `DriverID: "claudecode-glm"`
    - `AgentRuntime: "claude-code"` (same as claudecode — the harness
      identity is what matters for telemetry; the model varies)
    - `Model: <env CHITIN_CLAUDECODE_GLM_MODEL, default "glm-5.1">`
    - `Capabilities: [CapCodeImplement, CapSpecImplement]`
      (claim only the two capabilities the local model is genuinely
      good at; do NOT claim CapCodeReview / CapSpecAuthor without
      empirical evidence — those need qwen3.6 / qwen3-coder judgement
      quality)
    - `Tier: TierLocal` (new constant; see FR-007)
    - `CostClass: CostZero`
    - `Constraints.NetworkRequired: false` (local-only)
    - `Constraints.MaxContextTokens: <env CHITIN_CLAUDECODE_GLM_CONTEXT, default 32768>`
    - `Constraints.WorktreeRequired: true` (same as claudecode)

- **FR-003** The driver's `Ready()` MUST verify all preconditions:
    - the `ollama` binary is on `$PATH`
    - the `ollama` daemon responds 2xx to `GET http://localhost:11434/api/tags`
      (the standard ollama health surface)
    - the configured model name appears in the `/api/tags` response
      (i.e., `ollama pull <model>` has been run)
    - the `claude` CLI binary is on `$PATH` (ollama launches it as
      a child process; missing binary = launch fault)
  Returns `(true, "")` on success; `(false, "<one-line reason>")`
  on any failure. The probe has a 2s timeout — a slow ollama MUST
  NOT block scheduling.

- **FR-004** The driver's `Invoke()` MUST:
    - Build the command line: `ollama launch claude --model <card.Model> -- <claude-print-args>`
    - The `<claude-print-args>` portion matches what the existing
      claudecode driver passes (the prompt, `--print`, `--output-format`,
      etc.) so output parsing stays shared
    - Honor the env override `CHITIN_OLLAMA_BIN` for the ollama
      binary path (defaults to `ollama` via $PATH lookup)
    - Otherwise behave identically to claudecode (same prompt
      assembly, same worktree handling, same Result shape from spec
      075 FR-006)

- **FR-005** No separate proxy install is required (REPLACES the
  pre-2026-05-25 plan to ship litellm + a systemd unit). The
  `ollama launch claude` integration ships built-in starting at
  ollama v0.21+, which is the operator-host's current version. The
  operator runbook (T010 / FR-008) MUST flag the v0.21+ requirement.

- **FR-006** The `scheduler_started` chain event payload's `driver_id`
  field (already present per the spec 094 / 099 patterns) MUST carry
  the selected driver's ID. When claudecode-glm wins routing, the
  chain shows `driver_id: "claudecode-glm"`. Operators querying the
  chain by `driver_id` can compute "what fraction of whole-spec
  dispatches routed local vs cloud" for cost telemetry. If the
  current `scheduler_started` payload doesn't include `driver_id`
  (the spec 097 shape may not yet), extend it via the same
  backwards-compat pattern spec 119 used for `mode`.

- **FR-007** Define a new `Tier` constant `TierLocal` in
  `go/orchestrator/driver/taxonomy.go` for drivers running against
  a local model. Distinct from the existing `TierFrontier` (cloud
  T4) so the selector can express "prefer local when local is
  Ready". Also define `CostZero` constant if not already present.

- **FR-008** Operator runbook `docs/runbooks/spec-120-claudecode-glm.md`
  documents:
    - Prerequisites (ollama v0.21+, `ollama pull glm-5.1` once,
      claude CLI installed)
    - One-line smoke test: `ollama launch claude --model glm-5.1 -- -p "say hi"`
    - How to verify the driver is in the registry: grep the
      `chitin-orchestrator` worker-host startup log for
      "claudecode-glm" in the "drivers registered" line
    - How to force routing to claudecode-glm vs codex for testing
      via `CHITIN_DRIVER_ALLOW=claudecode-glm` (existing spec 097
      pattern)
    - Troubleshooting: ollama down, model not pulled, claude CLI
      missing, ollama version too old

## Success criteria

- **SC-001** A whole-spec dispatch with claudecode-glm and codex
  both registered + Ready completes successfully against either
  driver, with the chain event showing the chosen `driver_id`.
  Measured by running a small fixture spec twice with the registry
  filter pinned to each driver explicitly.

- **SC-002** When ollama is stopped, `chitin-orchestrator schedule`
  with a claudecode-glm-only allowlist exits with `unroutable`. With
  ollama running, the same dispatch succeeds. Measured by toggling
  the ollama service.

- **SC-003** Token cost per whole-spec dispatch via claudecode-glm
  is $0 (local inference). Measured by chain event payloads (no
  cost field need fire) plus operator-host cloud-API spend log
  showing no claudecode-glm invocation contributed.

- **SC-004** A representative spec (e.g. spec 117) implemented
  end-to-end by claudecode-glm produces a PR that passes CI and
  addresses every unchecked task. This is the "is glm-5.1 actually
  good enough" empirical check; if the PR is materially worse than
  codex's output, the routing infrastructure is still a success —
  the operator just doesn't enable it as the preferred default.

## Scope

In:
  - `go/orchestrator/driver/claudecodeglm/driver.go` — new driver
    (shells out to `ollama launch claude`)
  - `go/orchestrator/driver/claudecodeglm/driver_test.go` — unit
    tests covering Card()/Ready()/Invoke() with mocked `ollama` +
    `claude`
  - `go/orchestrator/driver/taxonomy.go` — TierLocal + CostZero
    constants (FR-007)
  - `cmd/chitin-orchestrator/main.go` — register claudecodeglm.New()
    in buildRegistry()
  - `docs/runbooks/spec-120-claudecode-glm.md` — operator runbook
    (FR-008)

Out:
  - Cost-aware routing (preferring TierLocal when Ready). This spec
    adds the data; the selector change is a follow-up.
  - A general "multi-backend Claude Code" driver shape (env-gated
    model on the existing claudecode driver). The cost policy is
    strict enough that two distinct drivers is cleaner.
  - Local-model evals — "is glm-5.1 actually good enough for spec
    117" is the SC-004 empirical check, not an a-priori design
    choice.
  - A litellm proxy + systemd unit (REPLACED by `ollama launch claude`
    which ships its own gateway).

## Edge cases

  - **Ollama running but target model not pulled**: Ready returns
    false with `model glm-5.1 not present in ollama (try: ollama pull glm-5.1)`.
  - **Ollama running but version <0.21**: `ollama launch` subcommand
    doesn't exist; Invoke() returns Result with explanation
    `ollama v0.21+ required for launch subcommand`. The runbook
    flags the version requirement.
  - **Ollama daemon running but unreachable on default port**:
    Ready's `GET /api/tags` returns connection refused; reports
    `ollama daemon not reachable at http://localhost:11434`.
  - **Multiple local models** (glm-5.1, qwen3-coder, qwen3.6,
    etc.): out of scope. The driver routes to ONE model per
    registration. A future operator can register multiple driver
    instances each pinned to a different model, then express
    preference via the selector. This spec ships glm-5.1 as the
    named default.
  - **claude CLI installed but configured with an opus API key**:
    Doesn't matter — `ollama launch claude` overrides
    `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` for the child
    process. The opus key is unused.
  - **Ollama gateway crashes mid-invocation**: Invoke() returns
    Result{Status: StatusFailed, Explanation: "..."} per the spec
    075 FR-007 contract. The scheduler can re-route the failed
    work unit to the next CapSpecImplement candidate (codex) on
    the next iteration of the work-unit workflow.

## Composability

  - **Spec 075** (Agent Driver Contract) — claudecode-glm is one
    more AgentDriver implementation; nothing in the contract
    changes.
  - **Spec 119** (whole-spec dispatch) — claudecode-glm becomes the
    second CapSpecImplement declarer alongside codex. The router's
    behaviour is unchanged: any Ready CapSpecImplement driver is a
    candidate.
  - **Spec 113** (PR iteration loop) — fires on the resulting PR's
    Copilot reviews unchanged. Driver identity doesn't affect
    iteration.
  - **Spec 094** (dialectic review) + **spec 116** (internal
    re-review) — still fire across drivers; claudecode-glm becomes
    a third reviewer candidate when its capabilities expand in a
    follow-up.
  - **Local-LLM autonomy thesis** (memory:
    project_chitin_end_goal_local_llm_safety) — this spec is the
    first concrete step toward "no cloud dependency for the
    implementation layer". claudecode-glm proves the driver-side
    integration; future specs (cost-aware routing, model evals,
    parallel-model orchestration) build on it.
