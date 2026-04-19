# Canonical Event Model

The contract. Owned by `libs/contracts`. Every adapter conforms to this
schema regardless of surface.

```json
{
  "run_id": "uuid",
  "session_id": "uuid",
  "surface": "claude-code | openclaw | ollama-local | copilot | github-actions | ...",
  "driver": "string",
  "agent_id": "string",
  "tool_name": "string",
  "raw_input": {},
  "canonical_form": {},
  "action_type": "read | write | exec | git | net | dangerous",
  "result": "success | error | denied",
  "duration_ms": 123,
  "error": null,
  "ts": "2026-04-19T12:00:00Z",
  "metadata": {}
}
```

## Field ownership

- **`raw_input`** — preserved verbatim from the agent's tool call.
- **`canonical_form`** — produced by the Go kernel's canon package (shell
  command → canonical `Command`/`Pipeline` form).
- **`action_type`** — produced by the Go kernel's normalize package
  (6 classes: read/write/exec/git/net/dangerous).
- **`result`** — Phase 1 is monitor-only, so this is always `success`
  (no denials). Phase 2+ may produce `denied`.
- **Schema ownership:** zod schema in `libs/contracts`. Go types are
  generated from the zod schema via an Nx target (`nx run contracts:generate-go-types`).

## Key properties

- **Surface-neutral.** `surface` + `driver` fields are the only things
  that differ between Claude Code, OpenClaw, Ollama-local, Copilot, etc.
- **Raw + canonical travel together.** Forensic fidelity is preserved.
- **Local-only.** Phase 1 never sends events over the network.
