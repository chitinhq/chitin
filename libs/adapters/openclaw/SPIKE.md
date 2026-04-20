# OpenClaw Adapter — Spike Notes (Phase 1.5)

> **Superseded in progress by [README.md](./README.md)** as of 2026-04-20
> (Phase F Task F1 — install verified; Task F2 answers the 4 questions
> below by observation, at which point this file is removed). This file
> is retained only so the 4 open questions remain link-resolvable until
> the README's Adapter strategy / Observable streams / Session semantics
> / Tool-call surface sections move past "TBD".

**Status:** Installed and smoke-verified (2026-04-20); SPIKE questions
still open — see README.md.

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
