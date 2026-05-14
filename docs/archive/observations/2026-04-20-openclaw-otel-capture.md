# SP-0 — openclaw OTEL capture and schema inventory

**Date:** 2026-04-20
**Workstream:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md`, Sub-project SP-0
**Box:** chimera-ant (Linux 6.17, x86_64), `red` user
**openclaw version:** `2026.4.15` (`041266a`)
**Plugin version:** `@openclaw/diagnostics-otel@2026.4.15-beta.1`
**OpenTelemetry SDKs bundled:** `@opentelemetry/sdk-node@^0.214.0`, `@opentelemetry/semantic-conventions@^1.40.0`, OTLP-proto exporters for traces/metrics/logs.

## Question SP-0 was asked to answer

> Does openclaw's `diagnostics-otel` plugin emit spans that conform to
> the **OpenTelemetry GenAI semantic conventions** (`gen_ai.*` attribute
> namespace)?

The meta-spec's translator design (SP-1+) depends on the answer. If
the plugin emits `gen_ai.*` spans, the translator maps
`gen_ai.request.model`/`gen_ai.usage.input_tokens`/etc. to chitin's
envelope. If it does not, the meta-spec's escape hatch (§"Meta-level
escape hatch") considers the framing invalid.

## Answer — one sentence

**No**: the plugin emits valid OTLP over OpenTelemetry's SDK, but
its attribute vocabulary is entirely vendor-branded
(`openclaw.*`, 75 occurrences in plugin source) with **zero**
occurrences of `gen_ai.*`. The plugin uses OTEL *as a transport and
generic-span-context convention* (standard `code.filepath` /
`code.function` / `code.lineno` for log-event provenance) but not as
a *GenAI-semconv consumer*.

## Evidence

### Static source inspection

All quantified counts below are from
`/home/red/.vite-plus/js_runtime/node/24.15.0/lib/node_modules/openclaw/dist/extensions/diagnostics-otel/index.js`
(the installed v2026.4.15-beta.1 bundle).

```bash
$ grep -c "gen_ai\."        index.js    # 0
$ grep -c "openclaw\."      index.js    # 75
$ grep -c "BatchSpanProcessor\|BatchLogRecordProcessor" index.js  # 86
```

### Metric instrument inventory (from plugin source lines 53504–53572)

Every metric name is `openclaw.*`. No instruments use OTEL GenAI
metric names (`gen_ai.client.token.usage`,
`gen_ai.client.operation.duration`, etc.).

| Instrument                         | Kind      | Unit | Purpose                           |
| ---------------------------------- | --------- | ---- | --------------------------------- |
| `openclaw.tokens`                  | Counter   | `1`  | Token usage by type               |
| `openclaw.cost.usd`                | Counter   | `1`  | Estimated model cost (USD)        |
| `openclaw.run.duration_ms`         | Histogram | `ms` | Agent run duration                |
| `openclaw.context.tokens`          | Histogram | `1`  | Context window size and usage     |
| `openclaw.webhook.received`        | Counter   | `1`  | Webhook requests received         |
| `openclaw.webhook.error`           | Counter   | `1`  | Webhook processing errors         |
| `openclaw.webhook.duration_ms`     | Histogram | `ms` | Webhook processing duration       |
| `openclaw.message.queued`          | Counter   | `1`  | Messages queued                   |
| `openclaw.message.processed`       | Counter   | `1`  | Messages processed                |
| `openclaw.message.duration_ms`     | Histogram | `ms` | Message processing duration       |
| `openclaw.queue.depth`             | Histogram | `1`  | Queue depth                       |
| `openclaw.queue.wait_ms`           | Histogram | `ms` | Queue wait time                   |
| `openclaw.queue.lane.enqueue`      | Counter   | `1`  | Lane enqueue events               |
| `openclaw.queue.lane.dequeue`      | Counter   | `1`  | Lane dequeue events               |
| `openclaw.session.state`           | Counter   | `1`  | Session state transitions         |
| `openclaw.session.stuck`           | Counter   | `1`  | Session stuck events              |
| `openclaw.session.stuck_age_ms`    | Histogram | `ms` | Stuck session age                 |
| `openclaw.run.attempt`             | Counter   | `1`  | Agent run attempts                |

### Span name inventory

Span names emitted by the plugin:

- `openclaw.model.usage` — emitted on model-usage diagnostic events
  (line 53686).
- `openclaw.webhook.processed` — emitted on successful webhook
  processing (line 53713).
- `openclaw.webhook.error` (span with error status) — emitted on
  webhook failures (line 53727).
- `openclaw.session.stuck` — emitted when a session crosses the
  stuck-age threshold (line 53792).

No span uses a `gen_ai.*` operation name
(`chat`, `text_completion`, `embeddings`, `execute_tool`).

### Attribute key inventory

Attributes set by the plugin fall into two categories:

**1. openclaw-branded (vendor-specific).** All GenAI-adjacent data:

- Session: `openclaw.sessionKey`, `openclaw.sessionId`
- Model / provider: `openclaw.provider`, `openclaw.model`,
  `openclaw.channel`
- Token usage: `openclaw.tokens.input`, `openclaw.tokens.output`,
  `openclaw.tokens.cache_read`, `openclaw.tokens.cache_write`,
  `openclaw.tokens.total`, and token-type discriminator
  `openclaw.token` ∈ `{input, output, cache_read, cache_write, prompt, total}`
- Context: `openclaw.context` ∈ `{limit, used}` (discriminator on the
  `openclaw.context.tokens` histogram)
- Webhook: `openclaw.webhook`, `openclaw.chatId`, `openclaw.error`
- Logging: `openclaw.log.level`, `openclaw.log.args`,
  `openclaw.logger`, `openclaw.logger.parents`,
  `openclaw.code.location`
- Dynamic passthrough: `openclaw.${anyUserSuppliedKey}` via log-event
  bindings (line 53617) — arbitrary attribute keys scoped under
  `openclaw.`

**2. Standard OTEL semconv (transport + generic-span-context only):**

- `code.filepath` — line 53621
- `code.lineno` — line 53622
- `code.function` — line 53623
- `service.name` (resource attribute) — line 53464

These are generic span-context attributes, not GenAI-specific. Every
*GenAI-specific* concept (model, provider, tokens, session) is in the
openclaw-branded namespace.

## Wire-format and transport facts

- Protocol: **`http/protobuf` is hard-locked** (line 53452–53454).
  Setting `OTEL_EXPORTER_OTLP_PROTOCOL=http/json` causes the plugin to
  log a warning and return without starting. This matters for chitin:
  the ingest station must decode OTLP/protobuf, not rely on OTLP/JSON.
- Default endpoint: `http://localhost:4318/` (line 22985, general
  OTLP-HTTP default).
- Signal paths: `v1/traces`, `v1/metrics`, `v1/logs` (line 53465–53467).
- Endpoint source of truth:
  `diagnostics.otel.endpoint` in `openclaw.json` overrides
  `OTEL_EXPORTER_OTLP_ENDPOINT` env (line 53456).
- Service name: `diagnostics.otel.serviceName` OR `OTEL_SERVICE_NAME`
  env OR a plugin default (line 53458). Set to `openclaw-gateway` in
  this capture.
- Batch/flush: `diagnostics.otel.flushIntervalMs` controls metric
  export cadence (line 53478), clamped to ≥1000 ms. Span batching
  uses the OTEL SDK defaults.
- Log export is gated by `diagnostics.otel.logs = true`; default is
  *logs disabled* (line 53462). Traces and metrics default on.

## Plugin-activation requirements

The plugin will **not start** unless **both** `diagnostics.enabled`
and `diagnostics.otel.enabled` are true in `openclaw.json` (line
53450). The plugin's own `configSchema` is empty
(`openclaw.plugin.json`: `"properties": {}`) — config is
root-level under `diagnostics`, not scoped to the plugin.

Enabling via `openclaw plugins enable diagnostics-otel` only sets
`plugins.entries.diagnostics-otel` and does NOT flip
`diagnostics.enabled` / `diagnostics.otel.enabled`. Both must be set
manually. During SP-0 both were set via:

```bash
python3 -c "
import json, pathlib
p = pathlib.Path.home() / '.openclaw/openclaw.json'
d = json.loads(p.read_text())
d.setdefault('diagnostics', {})['enabled'] = True
d['diagnostics'].setdefault('otel', {}).update({
    'enabled': True,
    'endpoint': 'http://127.0.0.1:4318',
    'logs': True,
    'flushIntervalMs': 2000,
})
p.write_text(json.dumps(d, indent=2))
"
```

## Runtime capture

A stdlib Python HTTP receiver was started on `127.0.0.1:4318`
(`/tmp/otel-capture/receiver.py`, committed under the `fixtures/`
subdirectory of this observation). The gateway was restarted via
`systemctl --user restart openclaw-gateway` with a drop-in
overriding `OTEL_EXPORTER_OTLP_ENDPOINT` to the receiver.

**What arrived at the receiver:**

- Periodic metric-exporter heartbeats to `/v1/metrics` every ~2 s
  (per the configured `flushIntervalMs`).
- Periodic log-exporter heartbeats to `/v1/logs`.
- Content-Type: `application/x-protobuf` on every request, confirming
  the hard-lock on `http/protobuf`.
- Every body captured during the SP-0 session was **0 bytes** across
  272 POSTs — batch processors flushing empty batches. No agent turn
  succeeded: local ollama returned 404 on the configured
  `qwen2.5-coder:7b` model, which was not pulled, and after pulling
  `qwen2.5:0.5b` and switching `agents.defaults.model.primary`, the
  embedded runtime still attempted `qwen2.5-coder:7b` — the persisted
  agent profile (`profile=sha256:9c018ec112cf`, per the
  `agent/embedded` log) references its own model independent of the
  top-level default, and further config surgery was not pursued
  within SP-0's scope.
- Notably, the failed run (`stage=assistant decision=surface_error
  reason=model_not_found`) **emitted no `openclaw.model.usage` span**
  either — confirming from the opposite direction that the plugin's
  instrumentation fires only on successful model events (per line
  53651+ in the source, which gates counter/histogram calls on a
  present `usage` object). Error-path capture would need its own
  instrumentation hook; the plugin does not provide one today.

The 0-byte bodies are still informative: they confirm the wire-level
contract (endpoint paths, content-type, cadence, batch cadence) that
a chitin ingest station will face. A non-empty capture driven through
a real *successful* model turn is deferred to SP-1's dogfood test,
since SP-0's question (semconv compliance) is fully answered by
static analysis of the emit-side code — the attributes a span
*would* carry are exactly those the source writes, and the source
has been inventoried above.

Captured files (under `fixtures/`):

- `v1-metrics-*.pb`, `v1-logs-*.pb` — empty-batch protobuf bodies,
  committed as wire-format samples.
- `v1-traces-1776738724912.json` — the receiver's smoke-test POST
  (`{"hello":"world"}`), NOT from openclaw, retained only as
  evidence the receiver parsed JSON correctly.

## Consequences for the meta-spec

This finding fires the meta-spec's **"Meta-level escape hatch"**
(§"Error handling stance") *conditionally*: the framing "chitin OTEL
**GenAI** ingest with openclaw as first consumer" is invalidated.

Two honest follow-up framings remain live. Picking between them
belongs to the next brainstorming session (before SP-1 is spec'd).

### Framing A — pivot first consumer to a `gen_ai.*`-compliant surface

The meta-spec's translator design (map `gen_ai.*` → chitin envelope)
stays intact; openclaw moves out of the "first consumer" role to a
later sub-project (whenever / if openclaw adopts `gen_ai.*`
semconv). A `gen_ai.*`-compliant first consumer needs to be
identified (candidates: LiteLLM, agents instrumented with
OpenInference, Langfuse-SDK-emitting agents, any agent using the
OpenLLMetry library).

Tradeoff: **delays openclaw-specific Lane-②** (cross-surface drift
between Claude Code and openclaw) because the first consumer
becomes a different surface.

### Framing B — retain openclaw as first consumer; widen translator scope

The translator maps **openclaw's native `openclaw.*` schema** to
chitin's envelope rather than `gen_ai.*`. This schema is stable,
well-defined (fully inventoried above from source), and uses the
same OTEL transport — so the four-station architecture holds
unchanged. The only change is that the translator's mapping table
is vendor-specific (openclaw.*) instead of standardized (gen_ai.*).

Tradeoff: **loses the ecosystem-compat positioning claim** from the
original meta-spec ("chitin speaks OTEL GenAI for any compliant
producer") — chitin would now speak "OTEL-with-openclaw-vendor-
schema" and gen_ai compliance becomes a *later* sub-project per
surface. The governance substance (hash-chain linkage across CC +
openclaw) is unaffected; it's only the positioning framing that
changes.

### Framing C — hybrid translator

Translator handles both `gen_ai.*` (for compliant surfaces) and
`openclaw.*` (for openclaw) via a dispatch on attribute namespace.
Largest scope; most generic; highest risk of over-scope for a first
sub-project.

## Recommendation for the next session

Bring this observation doc to a short brainstorming session before
SP-1 is spec'd. Pick between A, B, and C. My intuition is **B** —
narrower scope, faster to first real ingest, no dependency on
locating a `gen_ai.*`-compliant first consumer this box. The
ecosystem-compat positioning (A's motivation) is a later concern
that a second translator dialect can address once openclaw ingest
is proved.

## Box-state changes during SP-0 (needs revert decision)

- `~/.openclaw/openclaw.json` — added top-level `diagnostics` block;
  backup at `~/.openclaw/openclaw.json.bak` (created by openclaw's
  own config-overwrite backup path).
- `openclaw plugins enable diagnostics-otel` — modified
  `plugins.entries.diagnostics-otel` from `false` to `true`.
- `~/.config/systemd/user/openclaw-gateway.service.d/otel-capture.conf`
  — new drop-in setting `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318`
  and `OTEL_SERVICE_NAME=openclaw-gateway` in the gateway's env.

Without the receiver running on `:4318`, the gateway's OTLP exporter
will log repeated connection failures every 2 s. Revert options:

1. **Keep current state.** Leave plugin enabled and drop-in in place;
   start the receiver again when SP-1 needs captures.
2. **Disable export only.** Set
   `diagnostics.otel.enabled = false` in `openclaw.json`; plugin
   stays enabled, no network traffic.
3. **Full revert.** Restore `openclaw.json.bak`, remove the drop-in,
   `openclaw plugins disable diagnostics-otel`, reload systemd,
   restart gateway.

Applied at commit-of-this-doc time: **option 2 plus minor cleanup**.

- `diagnostics.otel.enabled` flipped to `false` in `openclaw.json`
  (stops network traffic; plugin stays enabled so SP-1 can flip it
  back without re-editing).
- `agents.defaults.model.primary` restored to
  `ollama/qwen2.5-coder:7b` (its pre-SP-0 value). The `qwen2.5:0.5b`
  alias entry is removed from `agents.defaults.models`; the ollama
  tag is left pulled so SP-1 can reuse it without another download.
- `~/.config/systemd/user/openclaw-gateway.service.d/otel-capture.conf`
  removed; `systemctl --user daemon-reload` + gateway restart
  applied.

To re-enable for SP-1, flip `diagnostics.otel.enabled = true`,
`agents.defaults.model.primary` to whichever local model is pulled,
restart the gateway, and re-run the receiver.
