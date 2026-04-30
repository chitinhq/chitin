# Plan-Enforcement — Design

**Status:** spec draft — Milestone H of cost-governance kernel v3, deferred to post-talk (after 2026-05-07). Sibling to grant-request (Milestone G); H builds on G's `request-pending` substrate.

**Author:** in-session sketch, 2026-04-29.

**Parent plan:** `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md`. Order: B → D → G → H.

**Trigger:** the user's question "should we work like Archon does, but enforce the plan and avoid drift via enforcement?" Reading Archon's README confirmed what the user suspected: Archon enforces *workflow shape* (DAG, isolation, deterministic-vs-AI node composition) but **not intra-AI-node content**. Inside a `prompt:` node, the AI is unconstrained — Archon dutifully proceeds to the next node regardless of what the AI did inside. Chitin's per-action gate is the missing piece. With plan-awareness, chitin can enforce conformance *inside* every AI node Archon orchestrates.

This spec proposes plan-enforcement: chitin reads an active plan, gates each action against the plan's current step, and triggers `request-pending` (per Milestone G) when actions drift.

---

## Positioning

**chitin is not Archon.** Don't rebuild the orchestrator. Position chitin as the inner-loop enforcer that composes with *any* orchestrator at the plan-file boundary.

| Layer | Owner | Enforces |
|---|---|---|
| Workflow shape (DAG, isolation, validation gates) | Archon (or hand-rolled, or `/superpowers` skill, or another orchestrator) | Phase ordering, fresh-context guarantees, deterministic-vs-AI node composition |
| **Action conformance to plan step** (this spec) | **chitin** | Every Bash/Edit/Write is checked against the active plan step's allowed actions |
| Permission policy (allow/deny per action) | chitin (today) | Whitelist/blacklist of action types and targets |
| Budget envelope (calls/bytes caps) | chitin (today, milestones A/C/E) | Real-time spend enforcement |

Together: workflow shape + action conformance + permission + budget = the determinism Archon advertises but only half-delivers.

---

## Goals (in scope)

1. chitin.yaml gains `plan_path: docs/superpowers/plans/<active>.md` (file pointer to the active plan).
2. Plan format: markdown checklist with frontmatter + per-step `allowed_actions` blocks. Operator-readable, parser-friendly.
3. `gov.Gate.Evaluate` gains a plan-conformance phase between policy and envelope-spend.
4. Drift counter — N consecutive non-conformances trigger `request-pending` via Milestone G.
5. Hybrid conformance judgment: rule-based (fast, T0) by default; classifier-based (slow, T1, opt-in) as fallback.
6. Step transitions: hybrid manual + agent-completion via `chitin-kernel plan complete <step-id>`.
7. Compose at file boundary — chitin doesn't know or care which orchestrator wrote the plan.

## Non-goals (separate work)

- Replacing or duplicating Archon. Chitin reads plans, doesn't orchestrate workflows.
- Plan authoring tools. Plans are written by humans, by orchestrators (Archon, `/superpowers`), or by the agent itself — chitin reads them.
- Workflow-level DAG semantics, parallel branches, fan-out. Out of scope.
- Cross-plan dependencies, plan-of-plans, nested plans. v2+.
- Plan amendment as a side effect of grant approval. v1 amendments live in the audit log; the plan file is read-only to chitin.

---

## Plan format

Markdown plan in any path the operator chooses; chitin.yaml's `plan_path` points at it. Frontmatter declares chitin metadata; body is a markdown checklist with per-step `allowed_actions` blocks.

```markdown
---
plan_id: 2026-04-29-grant-request-protocol-impl
status: active
chitin_enforce: true
drift_threshold: 3
---

# Plan: implement grant-request protocol milestone G

## Step 1 — schema

- step_id: schema
- tag: setup
- allowed_actions:
  - file.write target: "**/internal/gov/budget.go"
  - file.write target: "**/internal/gov/budget_test.go"
  - shell.exec target: "go test*"

Implement the three sqlite tables (agent_fingerprints, permission_requests,
permission_decisions) per the spec.

## Step 2 — gate integration

- step_id: gate
- tag: impl
- depends_on: [schema]
- allowed_actions:
  - file.read
  - file.write target: "**/cmd/chitin-kernel/gate_hook.go"
  - file.write target: "**/internal/gov/gate.go"
  - shell.exec target: "go test*"

Extend gov.Gate.Evaluate with the StatusRequestPending outcome.

## Step 3 — CLI surface

- step_id: cli
- tag: impl
- depends_on: [gate]
- allowed_actions:
  - file.write target: "**/cmd/chitin-kernel/request.go"
  - file.write target: "**/cmd/chitin-kernel/request_test.go"
  - shell.exec target: "go test*"

Implement chitin-kernel request {list, show, approve, deny, tail}.

## Step 4 — ship

- step_id: ship
- tag: ship
- depends_on: [cli]
- allowed_actions:
  - git.commit
  - git.push
  - github.pr.create
  - shell.exec target: "go test*"
```

**Parser semantics:**
- Frontmatter is YAML; body sections start with `## ` headers
- Each step's metadata is a flat key list (step_id, tag, depends_on, allowed_actions)
- `allowed_actions` items are policy rules — same vocabulary as chitin.yaml's top-level rules
- `target` patterns use chitin's existing glob semantics (matches `gov.Action.Target`)
- Step bodies (description text) are documentation, ignored by enforcement
- Plans without frontmatter `chitin_enforce: true` are not enforced even if `plan_path` points at them — explicit opt-in

**Lifecycle:**
- Active step is tracked in `~/.chitin/current-step` (atomic write-tmp + rename, kubectl-style)
- Default starts at the first step with no completed predecessors
- Operator advances: `chitin-kernel plan step <step-id>`
- Agent completes (and auto-advances): `chitin-kernel plan complete <step-id>`
- Plan completion: when last step is `complete`, plan auto-deactivates; chitin falls back to policy-only mode

---

## gov.Gate integration

New evaluation phase between policy and envelope:

```go
// gov.Gate.Evaluate sequence (today):
// 1. Lockdown
// 2. Policy
// 3. Bounds (push-shaped only)
// 4. Monitor-mode override
// 5. Envelope spend
// 6. Counter increment on deny
// 7. Stamp envelope/tier/cost fields
// 8. Log

// Insert new phase 4.5: plan conformance
if g.Plan != nil && d.Allowed && g.Plan.IsActive() {
    currentStep := g.Plan.CurrentStep()
    cd := g.Plan.CheckConformance(a, currentStep)
    if !cd.Conforms {
        drift := g.Plan.IncrementDrift(currentStep.ID)
        if drift >= g.Plan.DriftThreshold {
            // Trigger request-pending via Milestone G substrate
            req := createPermissionRequest(PermissionRequest{
                Reason:      "plan-drift",
                Action:      a,
                StepID:      currentStep.ID,
                DriftCount:  drift,
                Requested:   nil, // no budget delta — this is a conformance request
            })
            d.Status = StatusRequestPending
            d.RequestID = req.ID
            d.RuleID = "plan-drift"
            d.Reason = fmt.Sprintf("Action does not conform to step %s; drift counter at %d. Request_id=%s", currentStep.ID, drift, req.ID)
        } else {
            // Below threshold — emit warning, allow proceed
            d.RuleID = "plan-drift-warned"
            d.Reason = fmt.Sprintf("Plan drift warning (step %s, drift %d/%d)", currentStep.ID, drift, g.Plan.DriftThreshold)
            // Stay Allowed
        }
    } else {
        g.Plan.ResetDrift(currentStep.ID)
    }
}
```

`Plan.CheckConformance(action, step) ConformanceDecision`:
- Match `action` against each rule in `step.allowed_actions`
- First-rule-wins semantics, same as policy
- Returns `{Conforms: bool, RuleID: string, MatchedRule: *Rule}`

The grant-request frame for a drift event includes the step ID and the diverging action. Approval semantics:
- `decision: approve` — the specific action passes; drift counter resets; no plan amendment
- `decision: deny` — hard deny; drift counter stays
- `decision: amend` — interesting case: operator extends the step's `allowed_actions` for the rest of the session (ephemeral, not written back to plan file). Logged. Drift counter resets.

---

## Conformance judgment — hybrid

### v1: rule-based (fast, T0)

Each step's `allowed_actions` is a rule list. `CheckConformance` walks the rules, first match wins. Sub-millisecond per check, fits cold-start budget.

Covers ~95% of cases — most actions are unambiguously bound to a step's allowed glob (e.g., editing files under a specific path, running `go test`).

### v2: classifier fallback (slow, T1, opt-in)

When no rule matches, optionally consult a small local classifier model:

```yaml
# chitin.yaml
plan_enforcement:
  classifier:
    enabled: true
    model: qwen3-coder:30b
    endpoint: http://localhost:11434
    budget_ms: 500
    cache_ttl_s: 300  # cache (action, step) → conformance decision
```

Prompt shape:
```
You are evaluating whether an agent's proposed action belongs to the current plan step.

Plan step: {step.description}
Allowed action patterns: {step.allowed_actions}
Proposed action: type={action.type} target={action.target}

Does this action contribute to the step? Answer one of: yes / no / unclear
```

Constraints:
- `budget_ms` is hard — if classifier exceeds, default to non-conformance (drift counter increments). Cold-start latency must stay bounded.
- Classifier needs daemon mode (gate.sock) to amortize model load. Without daemon mode, every gate eval cold-starts the binary AND queries the classifier — unworkable.
- Cache decisions per (action.fingerprint, step.id) for cache_ttl_s. Same action against same step within window short-circuits.

Default v1: classifier disabled. Rules-only is the shipping mode.

---

## Drift handling

Per-step drift counter persisted in sqlite:

```sql
CREATE TABLE plan_drifts (
  plan_id TEXT NOT NULL,
  step_id TEXT NOT NULL,
  fingerprint_id TEXT NOT NULL,
  drift_count INTEGER NOT NULL DEFAULT 0,
  last_drift_at TEXT,
  PRIMARY KEY (plan_id, step_id, fingerprint_id)
);
```

Per `(plan, step, fingerprint)` triple — multiple agents on the same plan don't share drift counters.

Threshold: default 3, operator-tunable per-plan via frontmatter `drift_threshold: N`.

Reset: drift counter resets when the agent emits a conforming action OR the operator approves a drift request.

---

## CLI surface (v1)

```
chitin-kernel plan show              # current plan + step
chitin-kernel plan list              # all plans seen, current first
chitin-kernel plan inspect <plan_id> # full plan with step states
chitin-kernel plan step <step-id>    # operator advance current step
chitin-kernel plan complete <step-id># mark complete + auto-advance
chitin-kernel plan disable           # turn off enforcement (audit-only)
chitin-kernel plan enable
chitin-kernel plan tail              # stream conformance/drift events
```

All exempt from envelope spend per PR #68's chitin-admin pattern.

---

## Composition with Archon (and any orchestrator)

The composition point is the **plan file**. chitin's `plan_path` is just a file path — whatever writes that file is the orchestrator.

**Archon path:** Archon's workflow runs produce per-run plan files (Archon already tracks workflow state in sqlite). A small adapter writes the active workflow's current node + node spec into a chitin-readable plan markdown at the path chitin.yaml expects. Run starts → plan file rewritten → chitin re-reads on next gate eval. Run ends → plan file deleted → chitin falls back to policy-only.

**`/superpowers` skill path:** the plan file is the skill's plan output (already markdown). chitin.yaml points at `docs/superpowers/plans/<active>.md`. The skill writes the plan; chitin enforces it.

**Hand-rolled path:** operator writes a plan markdown manually. Same.

Chitin doesn't know which path is in use. The contract is: a markdown file at the configured path, conforming to the format above. Adapters live outside chitin (in Archon, in the skill, in operator scripts).

Adapter shape (Archon example):
```go
// archon-chitin-bridge.go (lives in Archon, not chitin)
func (r *Run) WriteChitinPlan(path string) error {
    plan := PlanMarkdown{
        Frontmatter: Frontmatter{
            PlanID: r.ID,
            Active: true,
            Enforce: true,
            DriftThreshold: 3,
        },
        Steps: r.Workflow.Nodes.Map(toStep),
    }
    return os.WriteFile(path, plan.Render(), 0o644)
}
```

---

## Open questions (resolve at impl)

1. **Plan format vs structured YAML.** Markdown-with-frontmatter is operator-friendly; pure YAML/JSON is parser-rigid. Default to markdown for v1; allow structured-format opt-in via frontmatter `format: yaml`.
2. **Step auto-completion semantics.** Should the agent be able to mark its own steps complete, or only the operator? v1: agent can complete via `chitin-kernel plan complete <step-id>`, but the chitin admin-exemption (PR #68) means it goes through the gate — operator can deny via policy if they want manual gating.
3. **Plan amendment durability.** When operator amends a step's `allowed_actions` via grant approval, does the plan file get rewritten? v1: ephemeral (amendments live in sqlite + audit log only). v2 may add `chitin-kernel plan amend <step> --add-allowed-action=...` that writes back.
4. **Multi-agent same-step.** Two agents on the same plan, same step. Drift counters are per-(plan, step, fingerprint), so they don't interfere. But conformance allow/deny is shared per step rules. Probably fine — operator can encode agent-specific rules via `allowed_actions: ... when: fingerprint.id == X` if needed.
5. **Plan disable / fail-open.** When chitin.yaml has `plan_path` but the file is missing or malformed, what's the default? v1: warn + fail-open (no plan enforcement, log warning). With `--require-plan` flag, fail-closed (block all actions). Same shape as `--require-policy` from milestone C.
6. **Step-step transitions on dependency.** A step's `depends_on` predecessors must be `complete` before it can be the active step. What if the agent skips? v1: skipping is itself a drift (the action belongs to a future step, not the current one). Operator can advance manually if intentional.
7. **Classifier-LLM provenance.** When the classifier renders a conformance decision, is its prompt + answer logged? v1 yes — append to `gov-decisions-<date>.jsonl` with `decided_by: "classifier:qwen3-coder"`. Audit-trail integrity.

---

## Acceptance criteria (Milestone H)

- **E2E rules-only:** active plan with 3 steps, agent on Claude Code intentionally writes a file outside step 1's `allowed_paths` 3 times. After threshold, gate emits `request-pending` with reason `plan-drift`. Operator approves; action runs; counter resets.
- **E2E completion flow:** agent finishes step 1, runs `chitin-kernel plan complete step-1`, current step auto-advances to step 2 (which lists step-1 as `depends_on`). Step 2's rules now apply.
- **Plan parser:** arbitrary plan markdown produces a stepwise rule overlay; round-trips through `plan inspect`.
- **Drift counter integrity:** counter persists across gate-eval cold-starts; multi-agent same-plan does not interfere across (fingerprint) lanes.
- **Composition test:** small Archon-adapter mock (or hand-written plan file mimicking what an orchestrator would produce) drives a chitin session through 3 step transitions cleanly.
- **Fail-open default:** chitin.yaml with `plan_path` pointing at a missing file emits a warning and runs in policy-only mode. With `--require-plan`, fails closed.
- **Classifier fallback (if v2 ships):** classifier opinion is logged with provenance; cache hits short-circuit; `budget_ms` is enforced.

---

## Schedule

**Not before 2026-05-15.** Order in v3 plan: B → D → G → H. H builds on G's `request-pending` substrate — don't start until G ships.

H probably splits into two tickets:
- H1 — rules-only enforcement (plan parser, gate integration, drift counter, CLI). Self-contained.
- H2 — classifier fallback (daemon mode, ollama integration, prompt design, caching). Depends on daemon-mode work (likely a Phase D side-effect from cold-start latency stress testing).

---

## Related

- `docs/superpowers/specs/2026-04-29-grant-request-protocol-design.md` — Milestone G; this spec depends on it
- `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md` — parent plan (B/D before G/H)
- `coleam00/Archon` — workflow-shape orchestrator chitin composes with at the plan-file boundary
- `~/.claude/projects/-home-red-workspace-chitin/memory/project_strategic_roadmap.md` — plan-enforcement is the **determinism** output of the 3-output thesis (fix / determinism / soul routing)
- `docs/roadmap.md` Phase 2 — "drift detection" line item this spec instantiates
