---
status: open
owner: claude-code
kanban: null
implementation_pr: null
superseded_by: null
effective_from: '2026-05-07'
effective_to: null
---

# Operator-approval escalation effect

Status: design (validated 2026-05-07 in collaborative brainstorm).
Implementation plan to follow in a sibling document.

Date: 2026-05-07

## Motivation

Today the chitin gate has three terminal decisions: `Allow`, `Deny`,
`Lockdown`. When a rule fires `effect: deny`, the agent's tool call is
blocked and the model is told "try something else." There is no
affordance for the operator to **approve** in real time.

This came up sharply on 2026-05-07 in two cases:

1. The `no-governance-self-modification` rule correctly blocked a
   policy edit that the operator (the human maintainer) wanted the
   agent to make. The replay analysis on the 28k-capture dataset had
   already flagged this exact pattern — 45 instances of operator-
   driven edits to chitin's own policy/plugin files, all denied.
2. Real-world hermes spawns where the agent's plumbing tool calls
   accumulate denials (kanban_show, etc.) — the operator might
   reasonably want to approve "let this agent do this thing for the
   next 15 minutes" without permanently broadening policy.

The pattern: certain denies are **not** "wrong action, refuse" — they
are "I want to confirm this with the operator before allowing." A
binary allow/deny isn't enough. We need a third effect that pauses the
agent and asks the human.

## Goals

1. Generic policy effect, plugin-extensible. Any rule (operator's or a
   plugin's) can opt into operator approval via `effect: escalate`.
2. Synchronous block from the agent's perspective. The agent's tool
   call hangs (within timeout) and returns allow or deny based on the
   resolution. Async semantics deferred until v2.
3. Existing transports first. Notify via the running hermes-gateway
   (slack/whatsapp), with a `chitin-kernel approve` CLI fallback that
   always works. No new infrastructure unless required.
4. Full chain replayability. Every escalation, notification, and
   resolution is auditable in the chain — same provenance pattern as
   ordinary decisions.
5. Operator-friendly UX via short-lived grants. Approving once grants
   a configurable window for the same rule × agent so the operator
   isn't re-approving the same action ten times in a session.
6. Trust-the-surrounding-identity auth model. Hermes-gateway
   authenticates the chat-side reply; CLI fallback trusts unix uid.
   Operator-on-own-box pattern; no in-product key management for v1.

## Non-goals

- Async / deferred-resume tool calls (v2; needs agent-side retry
  protocol).
- Multi-channel-with-priority notification (v1 is single channel via
  hermes; channel field in policy is already there for future
  expansion).
- Cryptographic signing of approvals (overkill for the local-operator
  threat model).
- Web UI for approval (defer until there's a chitin web service to
  hang it off of).

## Architecture overview

### The new internal state

`gate.Evaluate`'s return value contract is unchanged — it still returns
one of three terminal outcomes (Allow, Deny, Lockdown) packaged in a
`gov.Decision`. What changes is what happens between policy match and
return when a rule fires `effect: escalate`.

Today's flow:

```
policy match → terminal Decision → return immediately
```

New flow when the matched rule's effect is `escalate`:

```
policy match (Effect=escalate, Allowed=false)
  → check remember_grants
       │
       ├─ hit  → Decision.Allowed=true (rule_id="escalate-remember-grant"), return
       │
       └─ miss → write pending_approvals row → notify operator → poll
                  → on resolution: Decision.Allowed=<resolved>, return
                  → on timeout:    Decision.Allowed=false, return
```

The "pending" state is internal to `gate.Evaluate` (specifically inside
the new `PendingApprovalStore.Wait` helper). Callers never see a
"pending" return value; they see the resolved Allow or Deny — possibly
seconds-to-minutes after they called, depending on how fast the
operator responds.

### Lifecycle of a single escalation

```
        gate.Evaluate hits a rule with `effect: escalate`
                              │
                              ▼
        check remember_grants for unexpired (rule_id, agent)
              ├─ hit  → resolve immediately as allow,
              │          rule_id = "escalate-remember-grant"
              │          (no notification, no waiting)
              ▼
        write pending_approvals row (id, agent, action, target, rule_id,
                                     created_ts, channel, timeout_seconds,
                                     remember_window_seconds)
                              │
                              ▼
        send notify message via hermes-gateway
        (or skip if channel = cli-only)
                              │
                              ▼
                  ┌───── poll loop (every 2s) ─────┐
                  │                                 │
                  ▼                                 │
       row.resolved_ts is set?                      │
              ├─ no  ──── timeout reached? ─ no ───┘
              │                  │
              │                  └─ yes → write deny resolution
              │                              (resolution=timeout)
              ▼
       row.resolution: approved | denied | timeout
                              │
                              ▼
        if approved + remember_window > 0:
            write remember_grant row (rule_id, agent, expires_ts)
                              │
                              ▼
        return Decision{Allowed: <resolved>, RuleID: rule_id, ...}
                              │
                              ▼
                normal Decision flow continues
            (escalation counter, log, OnDecision)
```

### Components introduced

Five chitin-internal pieces:

1. **New Decision-flow state**: `EscalationPending` (and helpers to
   detect it and wait for resolution).
2. **SQLite tables** (in `~/.chitin/pending_approvals.sqlite`, separate
   file from `gov.db`): `pending_approvals` + `remember_grants`.
3. **CLI subcommands**: `chitin-kernel pending list|approve|deny`.
4. **Outbound notification dispatcher**: shells out to
   `hermes message send` (with the CLI flag set TBD pending
   confirmation of hermes's surface).
5. **Inbound reply listener**: a polling watcher
   (`chitin-kernel pending watch-hermes`) that reads new hermes
   messages on the operator channel, parses `approve` / `deny`
   replies, and writes resolutions to the table.

Two operator-managed pieces:

6. **Policy DSL extension** (`effect: escalate` + four optional
   fields).
7. **Operator config**: `~/.chitin/operator.yaml` declaring the
   default operator channel name(s) and the hermes binary path.

### Plugin extension surface

Per the v1 scope choice (generic effect), plugins can emit
`effect: escalate` rules in their own policy snippets, loaded via the
existing plugin policy mechanism. The gate's escalate-handling path
is plugin-agnostic — a plugin's escalate rule and an operator's
escalate rule are indistinguishable at runtime.

## Policy DSL extension

The chitin.yaml schema gets one new effect value and four optional
fields. Existing rules keep working unchanged.

```yaml
rules:
  - id: governance-self-mod-operator-approval
    action: file.write
    target_regex: "(^|/)chitin\\.yaml$|/internal/gov/|/cmd/chitin-kernel/"
    effect: escalate            # new value alongside allow/deny/guide/monitor

    # All optional, with sensible defaults:
    channel: hermes             # transport: "hermes" (default) | "cli-only"
    timeout_seconds: 600        # default 600 (10 min). On timeout → deny.
    remember_window_seconds: 300  # default 300 (5 min). On approve → grant for this window. 0 = single-call only.
    notify_template: |          # optional override; defaults to a built-in template
      Operator approval needed: agent={{.Agent}} wants to {{.Type}} on {{.Target}}
      Reason: {{.Reason}}
      Reply: `approve` / `approve 30m` / `deny <reason>` / ignore (auto-deny in {{.TimeoutMinutes}}m)
```

### Validation rules (parse-time)

- `effect: escalate` requires that the action type is recognized.
  Escalate on `unknown` is rejected — that should still hard-deny so
  we don't sleep on garbage tool-name spam.
- `timeout_seconds` ∈ [30, 86400]. <30 is unusable (operator can't
  physically respond); >86400 (1 day) gets weird with hermes worker
  timeouts.
- `remember_window_seconds` ≥ 0. 0 means "no grant; escalate every
  time."
- `channel` ∈ {`hermes`, `cli-only`}. Other values → policy_invalid
  (fail closed).

### Defaults when fields are omitted

```yaml
- id: foo
  action: shell.exec
  effect: escalate
  # implicit: channel=hermes, timeout_seconds=600, remember_window_seconds=300
```

## Internals

### Storage: `pending_approvals.sqlite`

Separate file from `gov.db` so escalation state has its own lifecycle
and can be rotated/cleaned independently.

```sql
CREATE TABLE pending_approvals (
  id              TEXT PRIMARY KEY,        -- ULID, e.g. 01JCN6X7...
  agent           TEXT NOT NULL,
  rule_id         TEXT NOT NULL,
  action_type     TEXT NOT NULL,
  action_target   TEXT NOT NULL,
  action_params   TEXT,                    -- JSON of Action.Params
  cwd             TEXT NOT NULL,
  reason          TEXT NOT NULL,           -- the rule's reason text
  channel         TEXT NOT NULL,           -- "hermes" | "cli-only"
  timeout_seconds INTEGER NOT NULL,
  remember_window_seconds INTEGER NOT NULL,
  created_ts      INTEGER NOT NULL,        -- unix epoch
  notified_ts     INTEGER,                 -- nullable: when notify dispatch succeeded
  notify_msg_id   TEXT,                    -- nullable: hermes-side message id (used to correlate replies)
  notify_failed_reason TEXT,               -- nullable: set when notify dispatch failed
  resolved_ts     INTEGER,                 -- nullable: set on resolution
  resolution      TEXT,                    -- nullable: "approved" | "denied" | "timeout"
  resolution_by   TEXT,                    -- nullable: "operator-cli" | "hermes-reply" | "timeout-watcher"
  resolution_reason TEXT,                  -- nullable: operator's "deny <reason>" text
  remember_grant_seconds INTEGER           -- nullable: if approved with a window, the actual seconds granted
);

CREATE INDEX idx_unresolved ON pending_approvals (resolved_ts)
  WHERE resolved_ts IS NULL;

CREATE TABLE remember_grants (
  rule_id    TEXT NOT NULL,
  agent      TEXT NOT NULL,
  granted_ts INTEGER NOT NULL,
  expires_ts INTEGER NOT NULL,
  PRIMARY KEY (rule_id, agent)
);

CREATE INDEX idx_remember_unexpired ON remember_grants (expires_ts);
```

The `idx_unresolved` partial index keeps the polling hot path cheap as
the table accumulates resolved history.

WAL mode (matching the gov.db handles).

File permissions: `chmod 600` at create time. Only the operator's uid
can read.

### Gate behavior — the new branch in `Evaluate`

A new step inserted between policy evaluation (existing step 2) and
counter recording (existing step 6):

```go
// 4.5 (new): if Decision is escalate-shaped, check remember_grants
//            then either short-circuit to allow (grant exists) OR
//            wait for operator (block until resolved or timeout)
if d.Effect == EffectEscalate && !d.Allowed {
    if g.RememberGrants.HasUnexpired(d.RuleID, agent) {
        d.Allowed = true
        d.RuleID = "escalate-remember-grant"  // distinct rule_id for chain audit
        // proceeds normally to counter + log
    } else {
        // Real escalation. Block until resolved.
        resolution := g.PendingApprovals.Wait(
            d.RuleID, agent, a, d.Reason,
            ruleEscalateConfig,  // channel, timeout, window from the policy rule
        )
        d.Allowed = resolution.Approved
        d.RuleID = resolution.OutcomeRuleID()  // "escalate-approved" | "escalate-denied" | "escalate-timeout"
        d.Reason = resolution.OperatorReason   // operator's text, or "operator did not respond within Xs"
        if resolution.Approved && resolution.GrantedWindowSeconds > 0 {
            g.RememberGrants.Insert(d.RuleID, agent, resolution.GrantedWindowSeconds)
        }
    }
}
```

`PendingApprovals.Wait()`:

```go
// Wait writes the pending row, dispatches notification, polls every 2s
// until resolved or timeout, returns the final resolution.
func (pa *PendingApprovalStore) Wait(...) Resolution {
    id := newULID()
    pa.insert(id, /* fields */)
    if channel == "hermes" {
        go pa.notifyHermes(id)  // fire-and-forget; row stamps notified_ts on success
    }
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()
    deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
    for {
        select {
        case <-ticker.C:
            row, _ := pa.get(id)
            if row.ResolvedTs != nil {
                return row.toResolution()
            }
            if time.Now().After(deadline) {
                pa.resolveTimeout(id)
                return Resolution{Approved: false, ...}
            }
        }
    }
}
```

### Decision shape changes

Two new fields on `gov.Decision`:

```go
type Decision struct {
    // ... existing fields ...
    EscalationID  string `json:"escalation_id,omitempty"`  // ULID of the pending_approvals row, when this decision came from one
    Effect        string `json:"-"`                         // "allow" | "deny" | "escalate" — internal, drives gate flow
}
```

`Effect` is internal (`json:"-"`) because the chain only needs the
resolved outcome (`allowed: true|false` + `rule_id`). `EscalationID`
IS in the chain so an auditor can join chain rows back to the
`pending_approvals` table.

Three new rule_id values appear in the chain:

- `escalate-remember-grant` — allowed because of an unexpired grant
- `escalate-approved` — operator approved this specific request
- `escalate-denied` — operator explicitly denied (or timeout)

These are purely for audit; no policy matches against them.

## Approval handlers

Two ways the operator's response gets into `pending_approvals`. Both
write the same `resolved_ts`/`resolution`/`resolution_reason` fields
the gate's `Wait` poll loop is watching.

### Handler A — CLI

Three new subcommands:

```bash
chitin-kernel pending list                        # show unresolved rows (id, age, agent, action, target, reason)
chitin-kernel pending list --json                 # machine-readable
chitin-kernel approve <id> [--window <duration>]  # approve; optional window override
chitin-kernel deny    <id> [--reason <text>]      # deny; reason ends up in the agent's tool-error
```

Behavior:

- `approve` sets resolution=approved, resolved_ts=now,
  resolution_by=operator-cli, remember_grant_seconds=<flag-or-rule-default>.
- `deny` sets resolution=denied with optional resolution_reason.
- Both refuse to re-resolve an already-resolved row (`resolved_ts IS NOT NULL`
  → exit 1 with a clear message).
- Both verify identity via unix uid: only the uid that owns the
  sqlite file can resolve. Hermes-side approval comes through Handler
  B (separate trust path).
- `pending list` sorts by created_ts ASC. Includes a column showing
  seconds-remaining-until-timeout.

This is the **always-works** path — even if hermes-gateway is down or
the operator's phone is dead, terminal access is sufficient.

### Handler B — hermes reply parser

**B1 — outbound notify** (called from `notifyHermes(id)` in the
kernel):

```go
func notifyHermes(id string, row pendingApprovalRow) error {
    msg := renderTemplate(row.notify_template, row)
    out, err := exec.Command("hermes", "message", "send",
        "--channel", operatorChannelFromConfig(),
        "--body", msg,
        "--reply-to-correlation-id", id,
    ).Output()
    if err != nil {
        markNotifyFailed(id, err)
        return err
    }
    var sent struct{ MessageID string `json:"message_id"` }
    json.Unmarshal(out, &sent)
    setNotifyMsgID(id, sent.MessageID)
    return nil
}
```

> **Open item:** the exact `hermes message send` flag set above is the
> contract we'll need to confirm against hermes's actual CLI surface.
> If hermes doesn't support `--reply-to-correlation-id`, fallback is to
> embed the escalation id in the message body (`...reply with: approve abc12345`)
> and have B2 parse it from there. Investigate before B1 implementation.

**B2 — inbound reply listener** — runs as
`chitin-kernel pending watch-hermes`, scheduled via systemd timer
(every 30s for v1). Polls hermes for new messages on the operator
channel since the last cursor; for each, parses the body:

- `approve` / `approve <duration>` → resolution=approved + window
- `deny` / `deny <reason>` → resolution=denied + reason
- anything else → ignore

Correlation strategies in priority order:

1. `--reply-to-correlation-id` if hermes exposes it (cleanest)
2. Embedded escalation id token in message body (`approve 01JCN6X7...`
   or just `approve` if exactly one row is unresolved)
3. Multiple unresolved + no token → post a clarification reply
   ("which one? reply with id: 01...")

Writes resolution to the table with `resolution_by=hermes-reply`. The
gate's `Wait` poll picks it up.

### Identity / auth

Trust the surrounding identity model:

- **CLI path**: unix uid check — sqlite file is `chmod 600`, owned by
  operator uid. Anyone else gets `pending_unauthorized` (exit 2).
- **Hermes path**: hermes-gateway is the one verifying that the
  inbound reply came from the operator's verified whatsapp/slack
  identity. Chitin trusts that.

Threat model for v1: single-operator local box. Multi-user host,
remote operator, and adversarial hermes are out of scope.

## Edge cases

**E1. Hermes-gateway down at notify time.**
Notify fails; row is stamped with `notify_msg_id=NULL` + a
`notify_failed_reason`. The kernel's `Wait()` keeps polling normally
— operator can resolve via `chitin-kernel approve <id>` from a
terminal. The `pending list` CLI surfaces these visibly so the
operator knows hermes is silent.

**E2. Operator approves an already-timed-out request.**
CLI refuses with `pending_already_resolved`. Hermes-reply parser does
the same check + posts a polite "too late" reply.

**E3. Kernel process dies mid-Wait.**
In-memory state gone; sqlite row persists. Two things on next gate
startup: (a) sweeper resolves rows past `created_ts + timeout_seconds`
as `resolution=timeout`, (b) the agent that originally made the call
gets a "block this tool call" response on its retry — agent never
sees the orphaned state. The dead row is preserved for audit.

**E4. Same (rule_id, agent) escalates twice in quick succession.**
v1: just creates two pending rows. Operator gets two notifies, can
resolve independently. Risk of notify spam if agent loops; mitigation
is the existing escalation counter — too many denials → lockdown,
which short-circuits the escalate path entirely. So a runaway loop
self-throttles.

**E5. Operator approves "session" but the session ends before the
window does.**
Grant stays in the table, expires by time not by session. Acceptable
for v1. v2 could add session-id-bound grants.

**E6. Two kernel processes (e.g., during redeploy) racing on the
same pending row.**
SQLite atomic `WHERE resolved_ts IS NULL` update by id. Losing
process's update is a no-op; both processes' polls see the same
resolution. Confirmed-safe via WAL mode.

**E7. `effect: escalate` rule on `action: unknown`.**
Refused at policy parse time. Otherwise the gate would block waiting
for operator approval on garbage tool-name spam.

**E8. CLI invoked with the wrong uid.**
`chitin-kernel approve` checks the sqlite file's owner uid against
current uid. Mismatch → exit 2 with `pending_unauthorized`.

**E9. Notify template renders to something dangerous.**
Templates only have sandboxed access to escalation-row fields; no
shell execution, no arbitrary disk reads. Go stdlib `text/template`
with a fixed function set.

## Testing strategy

### Unit tests

`internal/gov/escalate_test.go`:
- Wait returns approved/denied/timeout per resolution.
- Wait honors remember_grants and short-circuits without calling notify.
- Wait writes the right rule_id (escalate-approved/-denied/-timeout/-remember-grant).
- Concurrent Wait for same (rule_id, agent) creates two rows.
- Sweeper resolves past-deadline rows.

`internal/gov/policy_escalate_test.go`:
- Policy parse accepts `effect: escalate` with all field combinations.
- Validation rejects: timeout < 30, timeout > 86400, unknown channel,
  escalate on unknown action.
- Defaults are applied when fields are omitted.

`cmd/chitin-kernel/pending_test.go`:
- `pending list` outputs the unresolved set ordered by created_ts.
- `approve` writes the right fields, refuses re-resolution.
- `deny` writes reason text.
- Wrong uid → exit 2.

### Integration tests

`cmd/chitin-kernel/escalate_e2e_test.go`:
- Spawn a goroutine that calls `gate.Evaluate` on an escalate-rule
  action; from main test thread `approve` via the public API; assert
  goroutine returns Allowed=true within 5s.
- Same but `deny`; assert Allowed=false with reason text.
- Same but no resolution; assert times out at the configured deadline
  (use 5s for the test).
- Spawn two parallel `Evaluate` calls; approve first, deny second;
  assert each returns the correct outcome.

### Hermes-side handlers — mocked

- B1: replace `exec.Command("hermes", ...)` with a `notifyFn func(...) error`
  field on the store; default is the real shell-out, tests inject a
  mock.
- B2: same shape — a `replySource` interface backed by hermes-poll in
  production, by an in-memory queue in tests.

### No live-hermes test in CI

(Would need a configured hermes-gateway with a test channel.)
Operator runbook will include a manual shake-out smoke for the
hermes path post-deploy.

### Latency benchmarks

`BenchmarkWait_RememberGrantHit` and `BenchmarkWait_FullPollLoop` to
track that the remember-grant path stays sub-ms and the full poll
loop adds <2.1s on top of the operator's actual response time.

## Open items

1. Confirm hermes's `message send` CLI surface (specifically whether
   it supports a `--reply-to-correlation-id` flag or equivalent). If
   not, fall back to body-embedded id. **Investigate before B1.**
2. Confirm hermes's reply-listing surface (subscribe-vs-poll). If
   poll, the systemd timer cadence is fine. If subscribe, B2 becomes
   a long-running daemon. **Investigate before B2.**
3. Decide on the `~/.chitin/operator.yaml` schema (which channels are
   configured, how the default is selected). For v1 a single channel
   field is enough; multi-channel routing is a v2 concern.

## Companion follow-up

After this lands, file as a separate task: **operator-tier carve-out
in chitin's existing rules**. Specifically, `no-governance-self-modification`
should switch from blanket deny to `effect: escalate` so the operator
can approve their own legitimate edits. That's a one-line policy
change once this design is implemented.
