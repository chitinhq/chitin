# Contract: Kernel Decision JSON (kernel → plugin)

**Feature**: 091-fix-clawta-lockdown-loop · **Phase**: 1 (Design & Contracts)

The JSON shape `chitin-kernel router evaluate --hook-stdin` (and `gate evaluate`) writes to stdout. The plugin's `parseRouterDecision()` and `evaluateHookGate()` consume this.

## Schema

```json
{
  "decision": "allow" | "block",
  "reason": "string (required iff decision==='block')",
  "rule_id": "string (required iff decision==='block')",
  "continue": false,
  "stopReason": "string (co-occurs with continue:false)"
}
```

| Field | Type | Required | Trigger |
|---|---|---|---|
| `decision` | `"allow"` \| `"block"` | yes | Top-level verdict |
| `reason` | string | yes when blocked | Human-readable deny reason |
| `rule_id` | string | yes when blocked | Policy rule identifier (e.g., `"lockdown"`, `"no-destructive-rm"`) |
| `continue` | `false` | conditional | **Present iff the deny is a hard stop** (currently: rule_id === "lockdown"). Absent for soft denies. |
| `stopReason` | string | conditional | Co-occurs with `continue: false`. Operator-facing stop-reason text. |

## Examples

### Allow

```json
{ "decision": "allow" }
```

(Plugin maps to `{ allow: true }`.)

### Soft block (agent may retry)

```json
{
  "decision": "block",
  "reason": "no-destructive-rm: refusing rm -rf",
  "rule_id": "no-destructive-rm"
}
```

(Plugin maps to `{ allow: false, reason, ruleId, continue: undefined }`.)

### Hard stop (lockdown — terminates agent loop)

```json
{
  "decision": "block",
  "reason": "chitin: agent in lockdown — contact operator",
  "rule_id": "lockdown",
  "continue": false,
  "stopReason": "chitin: agent in lockdown — session terminated. Reset with: chitin-kernel gate reset --agent=<agent>"
}
```

(Plugin maps to `{ allow: false, reason, ruleId: "lockdown", continue: false, stopReason }`.)

## Invariants

- `decision === "allow"` ⇒ all other fields absent / ignored.
- `decision === "block"` ⇒ `reason` AND `rule_id` MUST be present.
- `continue === false` ⇒ `stopReason` SHOULD be present (current emission always co-occurs).
- `continue` is `false` or absent. **It is never `true`**; soft denies omit the field rather than setting `continue: true`.
- The kernel MUST NOT emit `continue: true`. If a future rule needs an explicit "soft block — try again differently" signal, that's a new field, not `continue: true`.

## Compatibility

- Pre-spec-091 plugins ignored `continue` and `stopReason`; they continue to work (block on `decision === "block"`).
- Spec-091 plugin extracts both fields; missing fields don't break it.

## Source of truth

`go/execution-kernel/internal/driver/claudecode/format.go:52-54`. Tests at `format_test.go:102-140` (lockdown) and `format_test.go:157-175` (regular deny).
