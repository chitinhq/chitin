---
status: draft
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: '2026-05-03'
effective_to: null
---

# Predictive Execution Policy + Audited Counterfactuals — Design

**Date:** 2026-05-03
**Status:** Design only. No code yet. Captures the next architectural layer above the openclaw-plugin slice (`2026-05-01-chitin-as-openclaw-plugin-design.md`) — what chitin's policy layer becomes once it stops being a flat allow/deny gate.
**Active lens:** da Vinci (open-ended cross-surface architecture, multiple concurrent invariants — `souls/canonical/davinci.md`). When implementation begins on Slice 1, swap to Knuth for the contract definition (one provable invariant per pass).
**Supersedes:** nothing yet — extends the three-plane architecture (control / execution / enforcement). Adds a *cognitive* layer to the enforcement plane.
**Constraints honored:** Anthropic ToS (`project_anthropic_tos_constraints.md`); OSS boundary (`feedback_chitin_oss_boundary.md`); kernel-authority rule (`docs/architecture/layer-contracts.md` v1).

---

## TL;DR

Chitin's policy layer today is a **flat gate**: `allow | deny` on raw tool calls, evaluated by pattern rules in the openclaw plugin (`apps/openclaw-plugin-governance`, `mode=observe` default). That's the floor. This spec defines the ceiling: chitin as a **predictive execution policy** that adjudicates tool calls against semantic envelopes and blast vectors, escalates to a tiered advisor when uncertain, audits decisions on the canonical chain, and supports counterfactual replay.

The category that emerges, compressed to one phrase: **predictive execution policy with audited counterfactuals**. Three legs (predictive, policy, counterfactual). No competing product has all three because none has the chain substrate that makes counterfactuals tractable.

This is the strategic Phase-2 ("policy") buildout, with breadcrumbs to Phase-3 ("ecosystem") via the SDK shape. It is **not** Phase-4 (cloud) — no hosted dashboard, no SaaS, no marketplace — until Phase 2 has dogfooded users.

---

## 1. The decision space

Today: `allow | deny`.

Spec: seven decisions, distinguished by what they do to the call and what they leave behind.

| Decision | Effect on the call | Chain artifact | Use when |
|---|---|---|---|
| `allow` | Execute as proposed | Allow event | Within budget, low blast, matches policy |
| `allow_with_auto_undo` | Execute; stage undo primitive; revert on PostExec failure or downstream verifier flag | Undo registered + reverted-or-confirmed | Reversible action, tolerable risk |
| `allow_observed` | Execute; mark for human review | Observed event with review marker | Low blast, unfamiliar pattern, want signal |
| `deny` | Block | Deny event with reason + alternatives | Hard rule violation, irreversible high-blast |
| `rewrite` | Modify args, then execute | Rewrite event with original + modified args | Known-safe variant exists (e.g., `npm` → `pnpm`) |
| `redirect` | Block this call, return alternatives the agent can take | Redirect event with proposed alternatives | Better path exists; want agent productive |
| `stage` | Execute in shadow (sandbox / dry-run / plan-only); surface diff for confirmation | Staged result event | High blast, recoverable confirmation worth the latency |

**Why this matters:** today a denied agent thrashes. A redirected one keeps moving. An auto-undo'd one crosses a reversible boundary safely. None of these are possible when the gate is binary.

The kernel is authority for which decision applies. Advisors and verifiers contribute inputs; the kernel's policy engine emits the decision.

---

## 2. The semantic envelope

Raw tool calls are too brittle for policy. Pattern matching `Bash` args produces fragile rules that obfuscation defeats.

The semantic envelope is the load-bearing abstraction. The type carrying it
is named `ToolCallRequest` to avoid collision with the existing
`ExecutionRequest` in `libs/contracts/src/execution-request.schema.ts`,
which is the *workflow-level* dispatch contract (what task should this
agent run?). `ToolCallRequest` is the *tool-call-level* adjudication
contract (should this specific tool call be allowed?). Different layers,
different concerns; deliberately distinct names so import sites are
unambiguous.

```
ToolCallRequest {
  // Identity
  request_id:     string  // ULID, chain-linkable
  session_id:     string
  agent_id:       string
  agent_tier:     T0..T5
  parent_event:   string?  // hash of preceding chain event

  // Raw call (verbatim, immutable)
  ingress:        'mcp' | 'claude_code_pretooluse' | 'openclaw_before_tool_call' | 'copilot_intercept' | ...
  tool_name:      string
  tool_args:      JSON
  tool_metadata:  JSON  // schema, server identity, declared capabilities (when MCP)

  // Derived (classifier output)
  semantic_envelope: SemanticEnvelope
  blast_vector:      BlastVector
  classifier_confidence: float
  classifier_version:    string
}

SemanticEnvelope {
  action_class:    'shell_exec' | 'file_write' | 'network_egress' | 'network_download' |
                   'git_op' | 'pr_op' | 'external_message' | 'database_write' |
                   'memory_write' | 'subprocess_spawn' | 'unclassified'
  target:          { kind: 'path' | 'host' | 'channel' | ..., value: string }
  artifact_type:   'shell_script' | 'binary' | 'source' | 'config' | ... | 'unknown'
  side_effect:     bool
  trust_assertion: 'agent_owned' | 'user_owned' | 'external_unverified' | 'external_verified'
}

BlastVector {
  reversibility:  'reversible' | 'reversible_with_effort' | 'irreversible'
  scope:          'local' | 'project' | 'cross_project' | 'external'
  visibility:     'silent' | 'logged' | 'observable' | 'public_broadcast'
  counterparties: 'self' | 'team' | 'external_users' | 'public'
}
```

**Two policy implications:**

1. **Policy matches the envelope, not the raw call.** A rule like "deny irreversible × public_broadcast unless approved" applies regardless of which tool surfaced the call. That's the abstraction win.
2. **Classifier confidence is a first-class signal.** Low-confidence classification is itself an escalation trigger, separate from blast magnitude.

### 2.1 Classification strategy

Three-tier estimation:

| Tier | Mechanism | Cost | Coverage |
|---|---|---|---|
| **C1** | Deterministic table: `(ingress, tool_name, arg_shape)` → envelope | ~ms | ~80% of calls (known tools, structured args) |
| **C2** | Sandbox dry-run / plan-only execution; observe declared effects | ~seconds | High-risk unknown patterns |
| **C3** | Model-assisted: classifier prompt over the raw call | ~seconds, $$ | Long-tail novel patterns |

Default to C1. Escalate to C2/C3 on heuristic match (e.g., `tool_name` not in C1 table, OR `action_class = unclassified`, OR explicit policy demand).

**Critical:** when C3 is invoked, the model's output is itself a chain event. The audit trail captures *why* this call was classified as it was — under which classifier version, against which prompt, with what confidence.

---

## 3. The kernel + advisor split

The kernel makes decisions deterministically. Advisors are consultative.

```
                  ToolCallRequest
                         │
                         ▼
                ┌────────────────┐
                │   Classifier   │  C1 default, escalates to C2/C3 on heuristic
                └────────┬───────┘
                         │
                         ▼
                ┌────────────────┐
                │  Policy Engine │  matches envelope+vector against rule library;
                └────────┬───────┘  produces candidate Decision
                         │
                         ▼
              ┌──────────┴───────────┐
              │ Escalation heuristic │  triggers advisor consultation when:
              └──────────┬───────────┘  - no exact policy match AND
                         │              - classifier confidence < threshold AND
                         │              - blast_vector non-trivial
                  ┌──────┴──────┐    OR
                  │             │    - N consecutive denies in session
                  │             │    - PostExec disagreed previously on similar
                  ▼             │    - blast budget would be exceeded
        ┌──────────────────┐    │
        │ Advisor (T_n+1)  │    │
        │ produces:        │    │
        │ - recommendation │    │
        │ - reason         │    │
        │ - artifacts      │    │
        └─────────┬────────┘    │
                  │             │
                  └──────┬──────┘
                         │
                         ▼
                ┌────────────────┐
                │ Final Decision │  kernel composes; chain event emitted
                └────────┬───────┘
                         │
                         ▼
              Allow / Undo-staged / Deny / Rewrite / Redirect / Stage
```

### 3.1 Advisor tiers

Advisors map to the existing driver tier table (`docs/swarm-backlog.md` § Tier definitions):

- T2 advisor: `ollama-cloud/glm-5.1` or Claude Haiku 4.5 — for moderate-complexity policy gaps
- T3 advisor: Claude Sonnet 4.6 / Copilot GPT-5.4 — for cross-cutting / architectural escalations
- T4 advisor: Claude Opus 4.7 — last resort before T5 (human)

Routing is **direct**, not strict-chain: `(action_class, blast_vector)` → tier table chooses the right advisor for the question, possibly skipping intermediate tiers.

### 3.2 Structured artifacts (the training loop)

Every advisor turn produces structured artifacts, not freeform text:

```
AdvisorResponse {
  recommendation:  Decision
  reason:          string
  agent_guidance:  string   // returned to the agent if denial/redirect
  artifacts:       Artifact[]
}

Artifact = PolicyDiff | ClassifierUpdate | MemoryWrite | HarnessSuggestion | CoverageReport
```

These artifacts are queued for human review (default), or auto-applied when an explicit acceptance threshold is met. Each artifact, applied or not, is a chain event — so advisor effectiveness is measurable over time.

This is what turns the system from "expensive linter" into **feedback-trained execution policy**. Without artifacts, the advisor only blocks. With them, every advisor turn is a candidate improvement to the policy library, classifier table, or harness prompt.

### 3.3 Junior-can-push-back

If an advisor recommends `deny` and the executing agent disagrees, the agent records dissent as a chain event and complies with the kernel's enforced decision. The dissent is *data for advisor improvement*, never a bypass route. Authority remains with the kernel.

### 3.4 Multi-advisor consensus

When two advisors at the same tier disagree (different specialties), use the existing canonical-souls quorum substrate (`docs/observations/quorums/`). This is reuse, not net-new infrastructure.

### 3.5 Reverse flow

The advisor's output isn't always recommendation-only. The executing agent at tier T_n may surface codebase-specific facts that the advisor (T_n+1) missed. These flow back as candidate `MemoryWrite` artifacts and are themselves chain events. Without reverse flow, advisor blind spots become permanent.

---

## 4. Intent layer + drift

Everything above is reactive to *what the agent is doing*. Without intent, "agent in the weeds" is undetectable, redirection has no destination, blast budget has no scope.

```
TaskDeclaration {
  task_id:                string
  goal:                   string
  scope:                  string[]   // file paths, package names, action classes
  success_criteria:       string[]
  declared_blast_budget:  BlastBudget
}

BlastBudget {
  tokens:               int          // model spend
  file_modifications:   int
  external_calls:       int
  irreversible_ops:     int
  wall_time_s:          int
}
```

Each session opens with a `TaskDeclaration`, periodically re-declared on state change. The kernel computes a **drift score** — divergence between declared scope and observed actions — using simple set distance over `action_class × target` tuples seen in the chain since declaration.

Drift threshold breach → escalation. Concretely: prompt-injection attacks structurally diverge from declared intent within a few actions; the signal sits in the chain.

`declared_blast_budget` consumes from each `predicted_blast` × cost factor. When the cumulative spend would exceed remaining budget, escalate to advisor (or human, if at top of tier).

---

## 5. Predicted vs observed blast — the calibration loop

Every PreExec emits `predicted_blast`. Every PostExec emits `observed_blast`. The delta is signal:

| Predicted | Observed | Meaning |
|---|---|---|
| Match | Match | Predictor calibrated, policy reasonable |
| Small | Huge | Classifier missed (training signal) **or** agent attempted bypass (security signal) — chain replay disambiguates |
| Huge | Small | Predictor over-fit; needs adjustment, possibly via advisor |

Both predicted and observed are recorded with `classifier_version` and `policy_version`. Calibration is measurable across versions.

---

## 6. Counterfactual replay

Given the canonical chain, ask: *what if we had blocked event N? What's the predicted divergent trajectory?*

The replay is implemented over chain history + classifier + policy:

1. Pick a historical chain segment.
2. Override one decision: switch event N's effective decision from `allow` to `deny` (or any other).
3. Re-run the agent's reasoning trace against the recorded responses prior to event N, then against synthesized "denied" response from event N forward.
4. The model produces a divergent trajectory — *what the agent would have done* under the alternative decision.

This is **only tractable because the chain is canonical and append-only**. Gateway logs and SIEM feeds can't do this; they record what happened, not in a structure that supports replay.

Use cases:
- Audit ("show the alternative timeline where the agent didn't ship X")
- Policy retrospection ("would policy P, if active last week, have prevented incident Y?")
- Advisor evaluation ("did this advisor's recommendations actually reduce blast over the next week?")

Skip in v1; earn it after enough chain history exists to replay against.

---

## 7. Trust calibration

Agents earn trust per `(agent_id, project, action_class)`. The score is input to the escalation threshold and the advisor tier router.

```
TrustScore {
  scope:                 (agent_id, project, action_class)
  successes:             int
  incidents:             int
  advisor_overrides:     int
  last_success:          timestamp
  decay_rate:            float  // per day with no activity
  current:               float  // [0, 1]
}
```

- High trust → fast-path more action classes (lower escalation threshold).
- Low / new agent → heavier oversight (lower threshold for advisor consultation).
- Decay: trust expires with disuse; a stale trust score is not load-bearing.

Existing analogues: `sudo` timestamp, GitHub risk-based MFA, Stripe fraud scoring. Production-ready concept; cheap to build; high leverage.

---

## 8. Escalation to human (T5)

At the top of the advisor tier, when no automated layer can decide, the human is authority. Three modes:

| Mode | Behavior | Use for |
|---|---|---|
| **Sync block** | Agent halts, human prompted, decision returns synchronously | High-blast irreversible |
| **Async queue** | Agent backgrounds; human reviews when convenient; agent resumes | Moderate blast, progress can pause |
| **Parallel notify** | Agent proceeds with `allow_observed`; human notified; can intervene | Low blast, unfamiliar pattern |

Per action class, with default mode declared in policy. Each mode is a distinct chain event type. Without this, "kernel as authority" is a fiction at the top of the tier — somebody has to be final authority for unbounded blast, and that's a human.

---

## 9. Reversibility primitives

Each action class declares its undo primitive. The kernel can `allow_with_auto_undo` only when a real undo exists.

| Action class | Undo primitive | Reversibility |
|---|---|---|
| `file_write` (untracked) | snapshot before; restore | reversible |
| `file_write` (tracked) | git revert / git restore | reversible |
| `git_commit` (local) | git reset (with safety check) | reversible |
| `git_commit` (pushed) | git revert + push | reversible_with_effort |
| `pr_op:open` | gh pr close + delete branch | reversible |
| `pr_op:merge` | gh pr revert | reversible_with_effort |
| `worktree_commit` | discard worktree | reversible |
| `external_message:slack` | mark; not undoable | irreversible |
| `database_write` | per-tool: requires schema knowledge | varies |
| `external_api_call` | per-tool: refund/cancel/compensate | varies |

The reversibility column flows into the blast vector. Most products can't offer auto-undo because they don't know how to undo. Chitin can, *per action class, by design*.

---

## 10. Threat model

The kernel must be implementable assuming the agent is hostile.

| Threat | Defense |
|---|---|
| Compromised agent (prompt injection, jailbreak, malicious instruction in fetched content) | Deterministic classifier on raw bytes; drift detection against declared intent; observed blast (not declared); chain audit |
| Compromised classifier (C3 model output poisoned) | Fall back to C1; mark uncertain classifications as escalation triggers; never auto-trust C3 |
| Compromised advisor | Kernel never delegates authority; advisor recommendations are inputs not commands; multi-advisor consensus on high-stakes |
| Compromised memory (poisoned to manipulate future decisions) | Memory writes are themselves chain events with provenance; reads use memory only with verified authorship; structured types with TTL and confidence floor |
| Compromised hook config | Hooks installed and verified at chitin install time; runtime verification on each invocation; configuration changes go through T5 |
| Compromised kernel | Out of scope (chitin can't defend against itself being subverted); OS-level concern; recommend OS sandbox + binary signing |

Load-bearing invariant: **trust the chain, not the agent**. Every primitive above must be implementable under the assumption that any single component except the chain may be hostile.

---

## 11. The layer cake (where this fits)

Chitin governs the **harness layer**. Other layers exist; this design does not replace them.

| Layer | What it sees | Bypass cost | Chitin's role |
|---|---|---|---|
| Network gateway (MCP gateway, etc.) | MCP traffic only | Trivial (agent picks non-MCP path) | Complementary, not core |
| **Harness hooks** | Every tool the harness routes — Bash, Edit, MCP, Task | Hard (requires escaping harness) | **Core** — chitin lives here |
| OS sandbox (gVisor, Bubblewrap, container) | Every syscall | Very hard (kernel exploit) | Recommend, but separate concern |
| Model refusal | What the model chooses to refuse | Trivial (jailbreak / rephrase) | Not a layer, not a defense |

Chitin does **not** see what a child process does after `Bash` returns. For that, OS sandbox is required, orthogonal to this design. Honest scope: chitin governs harness-routed tool calls + their declared effects; downstream syscalls of an allowed process are out of scope without OS-level pairing.

---

## 12. Slice ordering (implementation roadmap)

This spec describes a complete architecture. The implementation lands in slices, each independently shippable and dogfoodable.

### Slice 1 — `ToolCallRequest` + `decide()` + one ingress

**Goal:** prove the substrate. One canonical contract, one synchronous decision call, two ingress paths sharing one adjudicator.

Ingredients:
- `libs/governance/contracts/` — `ToolCallRequest`, `Decision`, `BlastVector`, `SemanticEnvelope` (Go + TS, schema-locked)
- `libs/governance/classifier/` — C1 deterministic table for one action class (`shell_exec` via Bash)
- `libs/governance/decide/` — synchronous policy evaluation, three rules:
  - `curl|sh` → `redirect` with alternatives
  - `npm install` → `rewrite` to `pnpm install`
  - default → `allow`
- Two ingresses sharing one decision path:
  - Claude Code `PreToolUse` hook script (Bash + jq + chitin-kernel call)
  - Existing `apps/openclaw-plugin-governance` extended to call `decide()`
- Decision becomes a chain event with `policy_version` + `classifier_version`

**Dogfood target:** chitin's own swarm uses these policies. Day-1 user.

### Slice 2 — Blast vector + observed-vs-predicted

Add `BlastVector` to `ToolCallRequest`. Classifier emits `predicted_blast`. PostExec emits `observed_blast`. Chain captures both. CLI report (`chitin policy report --since=24h`) aggregates by class and surfaces deltas.

### Slice 3 — `allow_with_auto_undo` for one action class

Pick `worktree_commit` (cheapest undo). Implement undo primitive; gate one rule on `auto_undo` instead of `deny`. Demonstrate the qualitative shift from blocking to reversible-allowing.

### Slice 4 — Tiered advisor with structured artifacts

T2 advisor (Claude Haiku 4.5 or local-glm). One artifact type: `PolicyDiff`. Queue for human review at `docs/policy-diffs/`. Measure advisor effectiveness against accepted vs rejected diffs.

### Slice 5 — Intent layer + drift

`TaskDeclaration` schema + drift score. One rule: drift score > 0.5 escalates to advisor. Test against a synthesized prompt-injection trace.

### Slice 6 — Trust calibration

Track trust per `(agent_id, project, action_class)`. Lower escalation thresholds with high trust. Decay schedule.

### Slice 7 — Escalation to human, all three modes

Sync block, async queue, parallel notify. Slack + CLI as ingress.

### Slice 8 — Counterfactual replay

Against accumulated chain history. Skip until enough data.

---

## 13. SDK shape (Phase-3 breadcrumb)

The shippable artifact users import:

```ts
import { Adjudicator, ToolCallRequest } from '@chitin/governance'

const adj = new Adjudicator({
  classifier: builtin,
  policies: [
    { name: 'no-curl-pipe-sh',
      match: { action_class: 'shell_exec', pattern: /curl.*\|\s*sh/ },
      decision: 'redirect',
      alternatives: ['Save the script, audit, run with --dry-run first'] },
    { name: 'pnpm-not-npm',
      match: { action_class: 'shell_exec', pattern: /^npm install/ },
      decision: 'rewrite',
      rewrite: req => ({ ...req, args: { command: req.args.command.replace(/^npm/, 'pnpm') } }) },
  ],
})

const decision = await adj.decide(executionRequest)
```

Users write *policy*, not plumbing. Classifier, chain audit, redirect mechanics, alternatives surfaced to the agent — all provided. Three default policies ship as examples (written as if a user wrote them) for credibility.

The SDK + the report CLI together prove the platform shape with no new infra. Hosted dashboards and policy marketplaces wait for ≥3 real users.

---

## 14. What this design rejects

Briefly, to make boundaries explicit:

- **MCP-specific hooks** (`PreMCPCall`, `PostMCPCall`) — violates the "MCP is just one ingress" insight. There is one canonical `PreExecution` gate over `ToolCallRequest`; MCP normalizes into it like any other ingress.
- **Cloud-first / SaaS-first roll-out** — Phase 4, not Phase 2. A hosted dashboard before users have policies is putting the cart before the horse.
- **Marketplace of community policies** — same. Earn it after users exist.
- **Auto-applied advisor recommendations** — the advisor is consultative; auto-applying its outputs reintroduces the nondeterminism we banned. Queue for review.
- **A single "risk score"** — blast vector is a vector; folding into a scalar loses information policy needs.
- **Network-layer enforcement as primary** — gateways are complementary; the harness is the only layer that sees every action chitin must govern.

---

## 15. Open questions

These do not block Slice 1 but need answers before Slices 4+:

1. **Async advisor coordination.** Synchronous `decide()` caps advisor latency. Truly async advisor (agent does other work, kernel resumes when verdict lands) requires a different control flow — likely Temporal-mediated. Decide on Slice 4.
2. **C3 classifier provider.** Cost and latency of a model-assisted classifier per call is non-trivial. Local model (Qwen3-coder on the 3090) vs cloud API. Decide on Slice 1's classifier work.
3. **Memory scope rules.** Per-project, per-task, per-agent? Privacy and pollution risk if too broad. Decide on Slice 4 (when first artifact is `MemoryWrite`).
4. **Performance budget.** p95 fast-path latency target — 10ms? 50ms? Determines what fits in C1 vs spills to C2/C3. Decide on Slice 1.
5. **Drift score formula.** Set distance over action-class × target tuples is one option; sequence-based drift is another. Decide on Slice 5.

---

## 16. Provenance

This spec emerged from a session with Jared on 2026-05-03 starting from a critique of MCP-gateway-vendor pitches and progressing through:

- Layer cake of policy enforcement (network / harness / OS / model)
- Verifiability via hash-chained record (not deterministic execution)
- Adjudicator framing (allow/deny/rewrite/redirect)
- Pair-programmer advisor with structured artifacts
- Blast vector + predicted-vs-observed + budget-per-task
- Intent layer + drift
- Reversibility primitives + auto-undo
- Trust calibration
- Escalation to human
- Threat model

The category that compresses everything: **predictive execution policy with audited counterfactuals**. Three legs, no competing product covers all three.

Companion implementation plan for Slice 1 will be filed separately at
`docs/superpowers/plans/2026-05-03-predictive-execution-policy-slice-1.md`
once Slice 1 lands.
