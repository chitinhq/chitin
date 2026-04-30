# Grant-Request Protocol — Design

**Status:** spec draft — Milestone G of cost-governance kernel v3, deferred to post-talk (after 2026-05-07).

**Author:** in-session sketch, 2026-04-29.

**Parent plan:** `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md`. This spec adds Milestone G to that plan; B and D ship first.

**Trigger:** PR #68 added a hard exemption for `chitin-kernel` admin commands so the operator can recover from exhausted envelopes via `envelope grant` from inside the gated session. That solved the immediate deadlock but exposed a thinner surface: when an agent hits any gate denial — envelope-exhausted, policy-deny, lockdown — there is no in-band way for the agent to *ask* the operator (or another decision channel) for permission to continue. Operator approval, when it happens, is out-of-band, retroactive, and unstamped by agent identity.

This spec proposes a **Grant-Request Protocol**: a three-way gate outcome (allow / deny / request-pending) with structured request frames, agent fingerprint registration, pluggable approvers, and a model-visible decision-return path.

---

## Goals (in scope)

1. Generalize `gov.Decision.Allowed bool` into a three-state `Status` enum: `allow | deny | request-pending`.
2. Make agent fingerprint first-class: `{driver, model, session_id, driver_version}` resolves to a stable `fingerprint_id` on first observation, attached to every subsequent Decision and request.
3. Define request and decision frames that ride over chitin's existing JSONL substrate (sibling stream to `gov-decisions-<date>.jsonl`).
4. v1 operator UX: `chitin-kernel request list/show/approve/deny/tail`.
5. Pluggable approvers: v1 = single-operator (CLI). v2 = Slack/policy/supervisor-agent.
6. Address the model-visibility problem: leverage the ACP-refusal-shape spike's "synthetic tool response with reason" pattern so approval round-trips reach the model in a form it can act on.

## Non-goals (separate work)

- **Plan/spec conformance enforcement** ("agent's actions must bind to a step in an active plan"). Adjacent and complementary direction, separate spec — see *Related future work* below.
- **Multi-step plan budgets** (per-plan-step envelopes). Likely falls out of plan-enforcement, not this spec.
- **Federated decision channels** (multiple operators, quorum voting, decision-engine federation). v1 single-operator; multi is v2+.
- **Cross-machine fingerprint federation** (fingerprints synced across operator boxes). Local-first; defer.

---

## Frame shapes

### Request frame

Emitted by `gov.Gate` when a rule has `effect: request` (new effect type), or when an envelope is exhausted/closed and policy declares `on-envelope-exhausted: request` as fallback.

```json
{
  "kind": "permission_request",
  "request_id": "01J-ULID",
  "ts": "2026-04-29T22:50:00Z",
  "agent": {
    "id": "claude-code",
    "model": "claude-opus-4-7",
    "driver_version": "claude-code-1.x",
    "session_id": "<driver-supplied>",
    "fingerprint_id": "01J-FINGERPRINT-ULID"
  },
  "action": { "type": "shell.exec", "target": "...", "path": "..." },
  "reason": "envelope-exhausted",
  "requested": {
    "calls": "+50",
    "bytes": "+1048576",
    "duration_seconds": 600
  },
  "context": {
    "envelope_id": "01J-...",
    "recent_decisions": [ /* last N audit rows for the same fingerprint */ ]
  }
}
```

### Decision frame

Written by an approver (operator CLI, Slack webhook, policy auto-approver, supervisor agent).

```json
{
  "kind": "permission_decision",
  "request_id": "01J-...",
  "ts": "2026-04-29T22:50:30Z",
  "decision": "approve",
  "amended": { "calls": "+10" },
  "reason": "operator: ack — narrowing to +10 calls",
  "decided_by": "operator"
}
```

`decision`: `approve | deny | amend`. `amend` allows the approver to change the requested amount before approving (e.g., "yes but only +10 calls, not +50").

`decided_by`: free-form identity string; recommended values `operator`, `policy:<rule-id>`, `supervisor:<agent-fingerprint-id>`.

---

## Storage

Three additive sqlite tables in `~/.chitin/gov.db` (existing `envelopes`, `envelope_grants`, escalation `counter` tables unchanged).

```sql
CREATE TABLE agent_fingerprints (
  id TEXT PRIMARY KEY,                  -- ULID, first-seen
  driver TEXT NOT NULL,
  model TEXT NOT NULL,
  session_id TEXT,
  driver_version TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  request_count INTEGER NOT NULL DEFAULT 0,
  approved_count INTEGER NOT NULL DEFAULT 0,
  denied_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE permission_requests (
  request_id TEXT PRIMARY KEY,          -- ULID
  ts TEXT NOT NULL,
  fingerprint_id TEXT NOT NULL REFERENCES agent_fingerprints(id),
  action_type TEXT NOT NULL,
  action_target TEXT,
  reason TEXT NOT NULL,                 -- envelope-exhausted | policy-request | lockdown
  requested_json TEXT,                  -- {calls, bytes, duration_seconds, ...}
  context_json TEXT,                    -- {envelope_id, recent_decisions, ...}
  status TEXT NOT NULL DEFAULT 'pending', -- pending | approved | denied | amended | expired
  expires_at TEXT
);

CREATE TABLE permission_decisions (
  request_id TEXT PRIMARY KEY REFERENCES permission_requests(request_id),
  decided_at TEXT NOT NULL,
  decision TEXT NOT NULL,               -- approve | deny | amend
  amended_json TEXT,
  reason TEXT,
  decided_by TEXT NOT NULL
);

CREATE INDEX idx_requests_fingerprint ON permission_requests(fingerprint_id);
CREATE INDEX idx_requests_status ON permission_requests(status);
```

---

## CLI surface (v1)

```
chitin-kernel fingerprint list [--driver=...] [--model=...]
chitin-kernel fingerprint show <id>

chitin-kernel request list [--status=pending|all] [--fingerprint=<id>]
chitin-kernel request show <request_id>
chitin-kernel request approve <request_id> [--amend-calls=N] [--amend-bytes=N] [--reason=...]
chitin-kernel request deny <request_id> [--reason=...]
chitin-kernel request expire-stale [--older-than=1h]
chitin-kernel request tail [--status=pending]
```

Exemption rule from PR #68 covers all of these — operator-recovery surface is reachable from inside a gated session.

---

## Integration with gov.Gate

`gov.Gate.Evaluate` gains the request-pending outcome:

```go
type Status int
const (
    StatusAllow Status = iota
    StatusDeny
    StatusRequestPending
)

type Decision struct {
    Status      Status
    RequestID   string  // populated when Status == StatusRequestPending
    // ... existing fields ...
}
```

Trigger paths:
1. **Policy rule with `effect: request`**: e.g., `target: "git push --force.*"` → request operator approval. Replaces the binary deny in cases where context-dependent approval is the right model.
2. **Envelope exhaustion fallback**: `chitin.yaml` declares `cost.envelope.on-exhausted: request` (vs the current implicit `deny`). When the envelope auto-closes, gate emits a request instead of a deny.
3. **Lockdown breach** (v2): instead of bricking the agent for N consecutive denies, escalate to a request-pending state. Operator can override with one approval.

Driver behavior on `StatusRequestPending`:
- **Claude Code hook**: exit 2 with structured reason: `"Permission requested — request_id=01J-...; await approval and retry."` Same channel as existing deny reasons.
- **Copilot ACP shim** (per ACP-refusal-shape spike): inject synthetic tool response with the same payload. Refusal frames are model-visible but carry no free-form reason field — synthetic-response remains the only path.

When the operator approves, the request row transitions to `status='approved'`. The next time the agent retries the same action, the gate's pre-evaluation pass checks for an approval matching `(fingerprint_id, action_fingerprint)` and short-circuits to `StatusAllow`. Amended budget deltas apply via `envelope.Grant(deltas)` atomically with the approval write.

**Polling vs push (v1 = poll):** the gate re-evaluates each tick; the agent naturally retries denied actions; on retry the approval is observed. v2 may add daemon-mode push if poll latency is unacceptable.

---

## Model-visibility (the hard part)

Per `docs/observations/acp-refusal-shape.md`: ACP `ToolCallStatus` is a closed enum (`pending|executing|completed|error`) with no `request-pending` terminal. `RequestPermissionOutcome` carries `optionId` only — no free-form reason. The Copilot SDK path (rung 3/4 spike) lands the agent's "tool refused" event with no operator-supplied text reaching the model.

Implication: structured request_id + suggestion text must ride in the **synthetic tool response body**, not in protocol metadata. The driver shim wraps the protocol-level deny with a tool-response that says: `Permission requested. ID=01J-... To wait, call chitin-kernel request show <id>. To proceed differently, retry with a smaller scope or different action.`

The agent is now a participant in its own permission flow. This unlocks chains like:
- Agent: requests +50 calls
- Operator: amends to +10 calls, reason "narrow scope"
- Agent (next tick): observes approval, sees amendment, narrows the work it was about to do, proceeds with +10 calls

This is the loop the user's strategic roadmap memory calls "soul routing" — but bootstrapped from the cheaper substrate of permission grants.

---

## Pluggable approvers

```go
type Approver interface {
    Decide(req PermissionRequest) (PermissionDecision, error)
}
```

Wire via `chitin.yaml`:
```yaml
governance:
  approvers:
    - kind: cli            # default v1 — operator runs `chitin-kernel request approve`
    - kind: policy         # auto-approve based on rules
      rules:
        - if: "fingerprint.model == 'claude-haiku-*' and requested.calls < 5"
          decision: approve
    - kind: slack          # v2
      webhook: "https://hooks.slack.com/..."
      channel: "#chitin-grants"
    - kind: supervisor     # v2 — another agent decides
      agent_fingerprint_id: "01J-OPUS-SUPERVISOR"
```

Approvers chain: first non-`pending` decision wins. CLI is always last (manual fallback if no automated approver fires).

---

## Relationship to existing primitives

| Primitive | Today | With grant-request |
|---|---|---|
| `gov.Counter` (escalation) | Locks down agent after N denies | Same; v2 may flip lockdown into a request-pending escalation rather than hard brick |
| `gov.BudgetEnvelope` | Spend-or-deny | Spend, deny, or request-with-budget-amendment; `Grant` becomes the approve-amended hook |
| Decision audit log (`gov-decisions-<date>.jsonl`) | Every gate decision | Sibling stream `gov-requests-<date>.jsonl`; cross-referenced by `request_id` |
| Tier classification | T0/T1/T2 label on Decision | Tier-aware default approver routing (T0 auto-approve, T2 operator-required) |
| Chitin admin exemption (PR #68) | Operator-recovery commands skip envelope spend | Same exemption applies to the new `chitin-kernel request approve/deny` surface |

---

## Open questions (resolve at impl)

1. **Synchronous vs asynchronous request semantics.** Does a pending request block the agent (gate spins until decision), or deny-with-pending-marker (agent moves on, retries later)? Leaning asynchronous — synchronous breaks the "gate is fast cold-start" property the v3 plan requires (p95 ≤ 100ms).
2. **Default expiration.** Stale requests should expire so the queue doesn't accumulate. Sensible default: 1h. Operator-tunable per chitin.yaml.
3. **Concurrent fingerprint registration race.** Two agents from the same `(driver, model, session_id)` tuple opening within the same second — share an id or not? Likely share via UPSERT-by-natural-key in the `agent_fingerprints` table; `id` is the ULID stamped on first insert.
4. **`recent_decisions` bounding.** What's a useful context window? Last 5 by fingerprint? Last 5 within the active envelope? Probably the latter; the envelope is the natural session boundary.
5. **Driver coverage for synthetic-response injection.** Claude Code hook is straightforward (exit 2 + JSON body). Copilot ACP needs the synthetic-response path proven against a captured fixture. Other future drivers (openclaw direct, GitHub Actions agent) need their own integration tests.
6. **Decision visibility back to the audit log.** Does the original `Decision` row get amended when the request is approved, or does a new Decision row get appended? Amend is cleaner for analytics; append is simpler. Probably append-with-link-back.

---

## Acceptance criteria (Milestone G)

- **End-to-end live test (operator-approves):** envelope set to 1 call, agent in Claude Code hits exhaustion, gate emits `permission_request`, operator runs `chitin-kernel request approve <id> --amend-calls=+10`, agent's next call succeeds, envelope shows +10 caps and `closed_at` cleared.
- **End-to-end live test (operator-denies):** same setup, operator runs `chitin-kernel request deny <id> --reason="end of budget"`, agent's next call denies cleanly with deny reason in the audit log, no infinite retry loop.
- **Fingerprint registry:** opening 3 sessions of 3 distinct `(driver, model)` tuples produces 3 fingerprint rows. Opening 3 sessions of the same tuple produces 1 row with `request_count=N`.
- **Audit-log integrity:** `gov-requests-<date>.jsonl` survives 8 concurrent shim writers (matches Milestone D stress test).
- **Operator UX:** `request list` shows pending in real-time. `request tail` streams approvals/denials. Both honor the chitin admin exemption from PR #68.
- **Driver coverage:** Claude Code hook + Copilot ACP shim both surface request_id to the model in a way verified by a capture-replay test.

---

## Schedule

**Not before 2026-05-08.** Phase G is post-talk. Order of milestones in the v3 plan, updated:

- ✅ A — kernel primitives (`feat/cost-governance-kernel-A`, merged #64)
- ✅ C — Claude Code hook driver (merged #66)
- ✅ E — operator surface / envelope CLI (merged #67)
- ⬜ B — Copilot ACP shim (post-spike, ready to start)
- ⬜ D — multi-agent envelope coordination (after B)
- ⬜ **G — grant-request protocol (this spec, post-talk)**

---

## Related future work

**Plan/spec conformance enforcement.** Sibling direction. Today chitin gates *actions* (file write, shell exec, http request). The plan-enforcement direction extends this to gate *plan-conformance*: does this action belong to an active plan/spec? If not, deny or escalate via a request. Connects to:
- The roadmap.md Phase 2 mention of "drift detection"
- The strategic-roadmap memory's "3 analysis outputs: fix / determinism / soul routing" — plan-enforcement is the *determinism* output
- The grant-request protocol here — natural to combine: "agent wants to deviate from plan → emits permission_request with `reason: plan-drift` and the diverging action → operator/supervisor decides"

A plan-enforcement spec would need to answer:
- Where does the active plan live? (proposal: chitin.yaml declares `plan_path: docs/superpowers/plans/<active>.md`)
- How is conformance checked? (proposal: a small classifier-step — possibly a tier-T0 local model — answers "does this action fit a step in the plan?")
- What's the drift-tolerance ladder? (proposal: same shape as escalation counter — N drifts → request-pending → operator decides)

**Recommend:** file a sibling spec `docs/superpowers/specs/2026-05-XX-plan-enforcement-design.md` after the post-talk dust settles. Don't conflate with grant-request — they're complementary, not the same protocol.

---

## References

- `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md` — parent plan
- `docs/observations/acp-refusal-shape.md` — model-visibility constraint
- PR #68 — admin exemption that opened the design space for this
- `~/.claude/projects/-home-red-workspace-chitin/memory/project_strategic_roadmap.md` — soul-routing thesis this spec bootstraps from
