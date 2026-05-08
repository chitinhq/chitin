# `apps/advisor` — chain-consumer scaffold (operator-implemented)

Scaffold landed alongside the audit Tier 6 cull on 2026-05-08 that
removed the in-gate `claude -p` subprocess from
`internal/router/advisor.go`. See
[`docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md`](../../docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md)
for the rationale (Sun Tzu lens: spawning an LLM inside the kernel's
hot path was a symmetric duplicate of hermes'
`approvals.mode: smart`; the kernel's moat is signal computation +
the gate, not LLM-running).

This directory is the operator's surface for "what to do when the
kernel's heuristics fire." It is intentionally NOT IMPLEMENTED — the
operator wires the implementation that fits their environment
(kanban-driven, hermes-cron-driven, standalone CLI, etc.).

## What it consumes

The kernel's router-hook stamps four heuristic signal fields onto
the gov.Decision rows in `~/.chitin/gov-decisions-<utc-date>.jsonl`:

| Field | Source | Meaning |
|---|---|---|
| `predicted_blast` | `internal/router/blast_radius.go` | 0.0–1.0; combined irreversibility/scope/visibility/counterparties score |
| `floundering_score` | `internal/router/floundering.go` | 0.0–1.0; loop / stall / denial-cascade detection |
| `drift_score` | `internal/router/drift.go` | 0.0–0.8; out-of-scope writes vs. declared intent |
| `routing_decision` | `internal/router/route_for.go` (when `chitin-routes.yaml` is wired) | candidate `<driver>/<model>` for a peer-escalation spawn |

Rows are emitted with `rule_id` prefix `router-heuristic:` so a tail
filter can pick them out: `jq 'select(.rule_id | startswith("router-heuristic:"))'`.

## What it does

Up to the operator. Examples that compose cleanly with the rest of
the workspace:

1. **File a kanban ticket** when `floundering_score > 0.8`.
   `pnpm exec tsx apps/advisor/src/index.ts` reads recent rows,
   shells out to `chitin kanban create` (or whatever the operator's
   ticket surface is), and exits.

2. **Ask another model** when `predicted_blast > 0.7`.
   The operator wires a Claude / GPT / Gemini call here — the
   kernel deliberately stays out of model-running.

3. **Spawn a peer at a higher tier** when `routing_decision` is
   present. Wraps the kernel's `RouteFor` candidate, spawns a
   sibling driver (Codex, Gemini, Hermes), and threads the result
   back as a chat message / chain event.

4. **Page the operator over WhatsApp / Slack / email** when
   `drift_score == 0.8` (out-of-scope high-blast write — the
   pattern that historically eats hours of unsupervised drift).

## What it does NOT do

- It does NOT mutate gov.Decision rows in-place. The chain is
  append-only; signal-stamped rows have `rule_id`
  `router-heuristic:<deny|allow|pre-action-block:...>` and live
  alongside the kernel's enforcement rows for the same tool call.
- It does NOT gate tool calls. By the time this app runs, the
  kernel's PreToolUse verdict has already been returned to the
  driver. Any influence on future tool calls flows through chain
  events the operator's downstream wiring picks up — not through
  the gate.
- It does NOT live in the kernel binary. Build + ship cadence is
  the same as `apps/cli` (TypeScript, runs via `tsx` or compiled
  via the operator's preferred bundler).

## Wiring shapes

### Hermes-cron-driven

```yaml
# ~/.hermes/cron.yaml
- name: chitin-advisor
  schedule: '*/5 * * * *'   # every 5 min
  command: 'pnpm exec tsx apps/advisor/src/index.ts --since 5m'
```

The advisor reads rows newer than `--since`, batches them, decides
what to act on, exits. Hermes restarts it on the next tick.

### Standalone CLI (operator-triggered)

```bash
$ chitin-advisor --since 24h --dry-run
floundering_score > 0.8: 2 rows; would file kanban tickets:
  - 2026-05-08T14:23:11Z run-abc123 (Bash: pnpm install)
  - 2026-05-08T14:31:02Z run-abc123 (Bash: pnpm install)
```

### Kanban-profile-spawned

The operator's kanban router has a profile that runs the advisor on
a per-card basis. When a chain row arrives with
`drift_score == 0.8`, the kanban runtime spawns a profile against
the originating run id; that profile is `chitin-advisor` configured
to act on a single row.

## Layer

`@chitin/advisor` is a chain-consumer app, parallel in layer to
`apps/cli`. It does not depend on the kernel package — it reads
audit rows off disk and acts. This means it's safe to add LLM SDKs,
HTTP clients, kanban-system bindings, etc. WITHOUT bloating the
kernel.

## Status

**Scaffold only.** The CLI prints `not yet implemented; see README`
and exits 64. Operator builds out the implementation as the wiring
needs land.

## Related

- `go/execution-kernel/internal/router/blast_radius.go` — kernel signal computer
- `go/execution-kernel/internal/router/floundering.go` — kernel signal computer
- `go/execution-kernel/internal/router/drift.go` — kernel signal computer
- `go/execution-kernel/internal/router/route_for.go` — kernel routing decision
- `go/execution-kernel/cmd/chitin-kernel/router_hook.go` — stamps the signals onto chain rows
- `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md` — the cull this scaffold accompanies
