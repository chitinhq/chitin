# OTEL/GenAI Translation

> 19 nodes · cohesion 0.14

## Key Concepts

- **SP-0 openclaw OTEL capture & schema inventory** (10 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **Hermes post_api_request capture** (5 connections) — `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- **No gen_ai.* semconv emission** (4 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **SP-1 Dogfood Gate (deferred)** (4 connections) — `docs/observations/2026-04-20-sp1-dogfood-gate.md`
- **SP-1 Dogfood Gate retry (deferred again)** (4 connections) — `docs/observations/2026-04-28-sp1-dogfood-gate-retry.md`
- **@openclaw/diagnostics-otel plugin** (2 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **Framing A: pivot to gen_ai.*-compliant first consumer** (2 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **Framing B: retain openclaw, widen translator scope** (2 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **openclaw.* attribute namespace (75 occurrences)** (2 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **qwen2.5-coder:7b model-not-pulled blocker** (2 connections) — `docs/observations/2026-04-20-sp1-dogfood-gate.md`
- **diagnostic events never fire blocker** (2 connections) — `docs/observations/2026-04-28-sp1-dogfood-gate-retry.md`
- **systemd unit /home/jared user blocker** (2 connections) — `docs/observations/2026-04-28-sp1-dogfood-gate-retry.md`
- **api_call_count is per-turn not per-session** (1 connections) — `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- **chitin-sink hermes plugin** (1 connections) — `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- **hermes-dialect-adapter-v1 plan** (1 connections) — `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- **Token-key deviation (input_tokens/output_tokens)** (1 connections) — `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- **Framing C: hybrid translator (gen_ai.* + openclaw.*)** (1 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **OTEL GenAI ingest workstream meta-spec** (1 connections) — `docs/observations/2026-04-20-openclaw-otel-capture.md`
- **synthesized-model-usage.pb fixture** (1 connections) — `docs/observations/2026-04-20-sp1-dogfood-gate.md`

## Relationships

- No strong cross-community connections detected

## Source Files

- `docs/observations/2026-04-20-openclaw-otel-capture.md`
- `docs/observations/2026-04-20-sp1-dogfood-gate.md`
- `docs/observations/2026-04-21-hermes-post-api-request-capture.md`
- `docs/observations/2026-04-28-sp1-dogfood-gate-retry.md`

## Audit Trail

- EXTRACTED: 42 (88%)
- INFERRED: 6 (12%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*