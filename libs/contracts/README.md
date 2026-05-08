# `@chitin/contracts`

The schemas every chitin component agrees on. If two layers disagree
about an envelope, event, or execution-request shape — they disagree
because of code drift, not contract drift; this package is the
single point of truth.

## Schemas

| File | Purpose |
|------|---------|
| `src/envelope.schema.ts` | The result envelope every workflow + activity produces (`tmp/result-*.json` shape). What the apply step + analyst recipes consume. |
| `src/event.schema.ts` | Canonical event shape on the chain (`session_start`, `pre_tool_use`, `decision`, `post_tool_use`, etc.). Hash-linked + surface-neutral. |
| `src/event.types.ts` | TypeScript types projected from the event schema; `EventType` enum + payload unions. |
| `src/execution-request.schema.ts` | `ExecutionRequest` — the typed dispatch contract: `{role, tier, allowed_drivers, bounds, parent_workflow_id, step_index, …}`. The dispatcher constructs these; the worker validates. |
| `src/payloads.schema.ts` | Per-event-type payload schemas referenced from `event.schema.ts`. |
| `src/hash.ts` | `hashEvent(map)` — the deterministic SHA-256 over canonical-keyed event JSON. Owns chain integrity. **Uses `node:crypto`** — must NOT be imported as a value from workflow code (Temporal's webpack bundler can't resolve `node:` imports). Workflow-side code uses type-only imports. |
| `src/chitindir-resolve.ts` | Resolves `~/.cache/chitin/<workspace>/` — the per-workspace state dir. Uses `node:fs`/`os`/`path`; same workflow-bundler caveat as `hash.ts`. |

## Workflow-bundle caveat

Temporal's workflow webpack bundle traces imports from `apps/
runner/src/workflow.ts`. Anything VALUE-imported there
(directly or transitively) goes into the bundle. `@chitin/contracts/
index.ts` re-exports `hash.ts` + `chitindir-resolve.ts`, both of
which import from `node:*` — which the bundler refuses.

Workflow-side files therefore use **type-only imports**:

```ts
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
```

Activity-side and CLI-side files can value-import freely.

## Test suite

```bash
pnpm exec vitest run libs/contracts
```

Schema round-trip tests + `hash.ts` determinism tests. Each schema
has a paired `tests/<name>.schema.test.ts`.

## Related

- `go/execution-kernel/internal/event/` — the Go-side mirror of the
  event schema; chitin-kernel writes events the TS schemas validate.
