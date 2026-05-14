# Spec: Autonomous Agent Governance Re-approach

## Objective

Re-approach linked-worktree enforcement after PR #414 was reverted.

The problem exposed by #414 was not the worktree invariant itself. The
problem was rollout order: enforcement shipped before autonomous agents
had a clear, machine-readable explanation of kernel denials and before
Chitin had an explicit worker-vs-supervisor authority model.

The next version must let autonomous agents understand governance
boundaries from deterministic Chitin output, not from operator tribal
knowledge. Ordinary workers must be able to inspect their status and
recover by changing behavior, but must not be able to reset, weaken, or
edit their own governance.

## Tech Stack

- Go kernel: `go/execution-kernel/`
- Kernel gate authority: `internal/gov`, `cmd/chitin-kernel/gate_*`
- Hook/router wrapper: `cmd/chitin-kernel/router_hook.go`
- Driver response formatting: `internal/driver/claudecode/format.go`
- Policy: `chitin.yaml`
- Documentation: `docs/governance-setup.md`, `docs/driver-conformance.md`

## Commands

Build:

```bash
pnpm exec nx run execution-kernel:build
```

Install built kernel:

```bash
bash scripts/install-kernel-symlink.sh
```

Focused Go tests:

```bash
cd go/execution-kernel && go test ./internal/gov ./cmd/chitin-kernel ./internal/driver/claudecode
```

Full Go tests:

```bash
cd go/execution-kernel && go test ./...
```

Driver conformance smoke:

```bash
chitin-kernel gate evaluate --hook-stdin --agent=hermes --policy-file "$PWD/chitin.yaml" --no-record
```

## Project Structure

- `go/execution-kernel/internal/gov/` owns authoritative decisions,
  counters, policy, bounds, and future deterministic invariants.
- `go/execution-kernel/internal/driver/*/` owns driver-specific
  normalization and driver-specific response formatting.
- `go/execution-kernel/cmd/chitin-kernel/router_hook.go` owns advisory
  router telemetry and must not obscure the kernel verdict.
- `chitin.yaml` owns default policy. Enforcement changes must be staged
  through monitor/diagnostic visibility before becoming blocking defaults.
- `docs/design/` holds this re-approach spec. If the authority model
  becomes permanent architecture, promote it into `docs/architecture/`
  or `docs/decisions/`.

## Code Style

Prefer explicit, typed decision fields over string parsing. Router output
may include summaries, but the kernel verdict must remain structured and
recoverable.

Agent identity must also be explicit and typed. Do not infer authority
from a display name like `hermes` or `claude-code`; that conflates a
driver, a worker role, and an operator/supervisor privilege level.

Example blocked hook payload shape:

```json
{
  "decision": "block",
  "reason": "worktree-required: cwd is the primary git checkout, not a linked worktree",
  "rule_id": "worktree-required",
  "suggestion": "Create a task branch in a linked worktree, then retry from there: git worktree add ../<repo>-<task> -b <branch>"
}
```

Example router telemetry shape:

```json
{
  "component": "router-hook",
  "kernel_denied": true,
  "kernel_rule_id": "worktree-required",
  "kernel_reason": "cwd is the primary git checkout, not a linked worktree",
  "heuristic_outcome": {}
}
```

Example identity context shape:

```json
{
  "agent_instance_id": "01KQS...",
  "agent_fingerprint": "8b77a3e91c04",
  "driver": "hermes",
  "model": "qwen3.6:27b",
  "role": "worker",
  "authority": "worker",
  "workflow_id": "wf-20260509-d1",
  "station_prompt_hash": "sha256:...",
  "skills_tools_hash": "sha256:...",
  "soul_lens": "none"
}
```

`agent_fingerprint` identifies a reusable capability profile. It should
be stable for the same driver/model/role/prompt/tools/lens combination.
`agent_instance_id` identifies one live run/session/worker. Multiple
instances can share a fingerprint.

## Testing Strategy

Unit tests must cover:

- Driver formatter includes `rule_id` in blocked responses.
- Router telemetry preserves `kernel_rule_id` and `kernel_reason` when
  `kernel_denied=true`.
- Worker-safe read-only commands remain allowed under baseline policy.
- Worker-forbidden admin commands remain denied.
- Worktree requirement, when reintroduced, first emits monitor/diagnostic
  rows without blocking.
- Decision rows preserve both stable fingerprint and per-instance identity.
- Supervisor-only actions cannot be authorized by spoofing a display
  agent name.

Integration or smoke tests must cover:

- Hermes hook payload for a mapped read-only tool produces a non-unknown
  action.
- A denied action shows the kernel rule id in the model-visible response.
- Router heuristic output never replaces the kernel denial reason.

## Boundaries

Always:

- Keep the Go kernel as the only authority for allow/deny.
- Make every block response self-explanatory to an autonomous agent.
- Allow workers to inspect governance status and receive structured
  recovery guidance.
- Treat router signals as advisory metadata.
- Test formatter and router output before changing policy defaults.

Ask first:

- Adding new canonical `gov.ActionType` values.
- Changing default `chitin.yaml` enforcement behavior.
- Allowing any worker-visible command that mutates governance state.
- Adding a supervisor identity or role claim source.

Never:

- Let a worker reset its own lockdown counter.
- Let a worker edit `chitin.yaml` or Chitin hook/plugin governance files.
- Let router heuristics override or hide the kernel verdict.
- Reintroduce approvals, scheduling, or orchestration into Chitin.
- Depend on LLM judgment in the kernel hot path.

## Success Criteria

Phase 1 is complete when:

- A blocked command visibly reports the kernel `rule_id` and reason.
- Router telemetry includes `kernel_rule_id` and `kernel_reason`.
- Tests prove router output does not mask kernel output.

Phase 2 is complete when:

- Chitin has a typed agent identity envelope that distinguishes driver,
  role, authority, stable fingerprint, and live instance.
- Chitin distinguishes worker-safe introspection from supervisor-only
  governance mutation using that identity envelope.
- `gate status`-style reads are worker-safe.
- `gate reset`, lockdown changes, policy edits, and hook/plugin mutation
  remain supervisor/operator-only.
- Denials for supervisor-only operations tell the worker that self-reset
  is not permitted.

Phase 3 is complete when:

- Linked-worktree requirements can run in monitor/diagnostic mode first.
- Diagnostic rows identify actions that would have been blocked without
  blocking normal agent operation.
- Operators can review chain data before enabling enforcement.

Phase 4 is complete when:

- Linked-worktree enforcement can be enabled for side-effect actions
  without trapping ordinary agents in unexplained denials.
- The default rollout path is documented in `docs/governance-setup.md`.

## Proposed Task Order

- [ ] Task 1: Preserve kernel denial details in model-visible responses.
  - Acceptance: blocked hook response includes `rule_id` and reason.
  - Verify: focused tests for `internal/driver/claudecode`.
  - Files: `internal/driver/claudecode/format.go`, formatter tests.

- [ ] Task 2: Add kernel denial details to router telemetry.
  - Acceptance: `kernel_denied=true` telemetry includes kernel rule and
    reason.
  - Verify: router hook tests.
  - Files: `cmd/chitin-kernel/router_hook.go`, router hook tests.

- [ ] Task 3: Promote agent identity/fingerprinting to a typed kernel
  context.
  - Acceptance: decision rows can carry `agent_instance_id`,
    `agent_fingerprint`, `driver`, `model`, `role`, `authority`, and
    `workflow_id` without overloading the existing `agent` display field.
  - Verify: gov decision JSON tests and hook-constructor tests.
  - Files: `internal/gov`, `cmd/chitin-kernel/gate_hook.go`,
    `libs/contracts/src/fingerprint.ts`, schema docs.

- [ ] Task 4: Specify worker vs supervisor command classes.
  - Acceptance: docs and policy comments define read-only introspection
    versus governance mutation, keyed by typed authority rather than
    string agent name.
  - Verify: policy tests or command classification tests.
  - Files: `internal/gov`, `cmd/chitin-kernel`, `docs/governance-setup.md`.

- [ ] Task 5: Reintroduce worktree requirement as diagnostic-only.
  - Acceptance: chain rows show would-block worktree violations without
    blocking execution.
  - Verify: gov tests and smoke evaluation.
  - Files: `internal/gov`, `chitin.yaml`, docs.

- [ ] Task 6: Enable enforcement only after diagnostic data is reviewed.
  - Acceptance: explicit operator decision; no default silent escalation
    from diagnostic to enforcement.
  - Verify: CI plus manual smoke from primary checkout and linked worktree.
  - Files: `chitin.yaml`, docs.

## Open Questions

1. What is the trusted source for `authority=supervisor`? Candidate
   options: signed local token, operator-only CLI flag rejected from hook
   paths, hermes-supplied supervisor identity, or root-owned config.
2. Should `gate reset` be completely unavailable through governed hooks,
   or available only when a signed/operator-local condition is present?
3. Should linked-worktree diagnostics be represented as a new rule id,
   such as `worktree-required-diagnostic`, or as metadata on an allow row?
4. Should Hermes-specific safe introspection commands be normalized as
   existing action types or get a distinct `governance.status` action?

## Identity And Fingerprinting Notes

Current state:

- `libs/contracts/src/fingerprint.ts` computes a 12-character SHA-256
  fingerprint from driver, model, role, station prompt hash, skills/tools
  hash, and soul lens.
- `gov.FingerprintContextFromEnv()` stamps model, role, workflow id, and
  fingerprint onto decision rows when `CHITIN_*` or `CHITIN_DISPATCH_*`
  variables are present.
- The existing `Decision.Agent` field is still overloaded. In practice it
  often means driver surface (`hermes`, `claude-code`, `codex`) rather
  than a unique worker instance or authority-bearing identity.

Target state:

- `agent` remains a backwards-compatible display/source field.
- `agent_instance_id` identifies one live autonomous worker/session.
- `agent_fingerprint` identifies the reusable capability profile.
- `driver` identifies the tool-call surface: Claude Code, Codex, Gemini,
  Hermes, OpenClaw, Copilot.
- `role` identifies workload role: worker, reviewer, architect, ci-fixer,
  system, external.
- `authority` identifies what governance mutations are permitted:
  worker, supervisor, operator, system.
- `workflow_id` joins decisions back to hermes/openclaw/swarm task state.

Authority is not the same as role. A `reviewer` can still be a worker for
governance purposes. A `supervisor` may review, reset, and redeploy only
when the trusted authority source says so.
