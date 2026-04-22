# Hermes post_api_request Capture — 2026-04-21

Captured during the hermes dialect adapter v1 implementation, Phase B of
`docs/superpowers/plans/2026-04-21-hermes-dialect-adapter-v1.md`.

## Source

- Hermes version: Hermes Agent v0.10.0 (2026.4.16)
- Model (primary): `glm-5.1:cloud` via `ollama-launch` provider (see `~/.hermes/config.yaml`)
- Model (delegation): `qwen3-coder:30b` via `ollama-launch` (this session exercised delegation)
- Gateway: CLI (`hermes chat`), not Telegram — the probe spec assumed Telegram, but the plugin hooks fire identically in CLI
- Plugin: `~/.hermes/plugins/chitin-sink/` (Phase A of this plan)
- Trigger: Phase A's `hermes chat` session running tasks 1–5 of this plan (real work, not synthetic round-trip)

## Event-type counts

```
     12 on_session_end
      3 on_session_start
     22 post_api_request
     14 post_tool_call
     30 pre_tool_call
```

## One sample `post_api_request` line (pretty-printed)

```json
{
  "event_type": "post_api_request",
  "ts": "2026-04-21T23:45:37.556922+00:00",
  "kwargs": {
    "task_id": "20260421_194524_bf44f6",
    "session_id": "20260421_194524_bf44f6",
    "platform": "cli",
    "model": "glm-5.1:cloud",
    "provider": "custom",
    "base_url": "http://127.0.0.1:11434/v1",
    "api_mode": "chat_completions",
    "api_call_count": 1,
    "api_duration": 7.367602109909058,
    "finish_reason": "stop",
    "message_count": 2,
    "response_model": "glm-5.1",
    "usage": {
      "input_tokens": 12539,
      "output_tokens": 40,
      "cache_read_tokens": 0,
      "cache_write_tokens": 0,
      "reasoning_tokens": 0,
      "request_count": 1,
      "prompt_tokens": 12539,
      "total_tokens": 12579
    },
    "assistant_content_chars": 36,
    "assistant_tool_call_count": 0
  }
}
```

## Notes — shape deviations from spec

The spec (design doc § post_api_request mapping) assumed an OpenAI-compat
usage shape. Real hermes v0.10.0 emits a richer, native-hermes shape:

1. **Token keys**: `usage` has `input_tokens`/`output_tokens` as the canonical keys,
   with `prompt_tokens` as an alias for `input_tokens`. `completion_tokens` is
   **not present** — only `output_tokens` carries that value. Translator must try
   `prompt_tokens` first and fall back to `input_tokens`; and must read
   `output_tokens` (there is no `completion_tokens` to try first).

2. **Cache tokens**: spec expected `usage.prompt_tokens_details.cached_tokens`.
   Real hermes emits `usage.cache_read_tokens` at the top level of the usage
   dict, plus `usage.cache_write_tokens` and `usage.reasoning_tokens`. Translator
   should read `cache_read_tokens` directly.

3. **Provider normalization**: `providers.ollama-launch` in `config.yaml` becomes
   `provider: "custom"` in the event because hermes normalizes custom-endpoint
   providers. Translator keeps the emitted value verbatim; downstream can
   heuristically re-derive provider from `base_url` if needed (out of scope for v1).

4. **response_model vs model**: `model` retains the `:cloud` suffix (`glm-5.1:cloud`);
   `response_model` strips it (`glm-5.1`). Translator prefers `response_model` per
   spec — the value actually returned by the LLM server.

5. **Plugin output path**: Phase A plugin writes to `~/chitin-sink/` (via
   `Path.home()`) rather than the spec's `~/.hermes/chitin-sink/`. Capture location
   is a Phase A bug; does not affect chitin-side ingest, since this observation
   doc plus the translator are path-agnostic. Fix belongs on the hermes side.

## Session-correlation observation

`task_id` and `session_id` are identical in the capture
(`20260421_194524_bf44f6`). Hermes appears to use the session timestamp ID as
both task and session identifier for CLI sessions. Translator stays robust by
reading `session_id` as authoritative for trace derivation (per spec).

## api_call_count is per-turn, not per-session

The design spec's `hermesSyntheticSpanID(session_id, api_call_count)` assumed
`api_call_count` is a monotonically-increasing counter across a session. The
real capture disproves this: 8 distinct LLM calls within session
`20260421_194524_bf44f6` (different timestamps, token counts, finish reasons)
**all have `api_call_count: 1`**. The counter resets on turn boundaries
rather than accumulating for the session.

Fix: the translator now derives the span ID from `(session_id, ts)` — the
plugin's `ts` field is microsecond-resolution ISO UTC, unique per
post_api_request line. `api_call_count` is still required for the
missing-fields quarantine check (it signals malformed plugin output), but is
no longer part of the span-ID input.
