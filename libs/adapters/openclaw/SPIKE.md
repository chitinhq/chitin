# OpenClaw Adapter — Spike Notes (Phase 1.5)

**Status:** Placeholder — OpenClaw's launch model not yet verified.

**Open questions:**
1. Does OpenClaw expose a hook/plugin API, or do we wrap its CLI at the process level?
2. What logs/streams does it produce during a session (stdout, structured log file, API events)?
3. How does it identify a "session" and its boundaries?
4. Does it support tool calls, and if so, where is the decision/execution boundary observable?

**Phase 1.5 minimum (current):** `chitin run openclaw [args]` emits a `session_start` on launch
and a `session_end` on exit. No inner events. This proves the envelope carries surface-neutrally
for a third surface even if the content is sparse.

**Follow-up when OpenClaw is in use:** extend this adapter to capture `user_prompt` and
`assistant_turn` from whatever mechanism OpenClaw exposes. Tool-call chain parity is Phase 2.
