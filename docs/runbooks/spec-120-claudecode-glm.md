# Spec 120 Runbook: claudecode-glm

`claudecode-glm` runs the Claude Code CLI through Ollama's built-in local
gateway:

```sh
ollama launch claude --model glm-5.1 -- -p "say hi"
```

## Prerequisites

- Ollama v0.21 or newer. Older versions do not provide `ollama launch`.
- The model is pulled once on the worker host:

```sh
ollama pull glm-5.1
```

- The Claude Code CLI is installed and available as `claude` on `$PATH`.
- The Ollama daemon is reachable at `http://localhost:11434`.

Optional overrides:

- `CHITIN_CLAUDECODE_GLM_MODEL`: model name, default `glm-5.1`.
- `CHITIN_CLAUDECODE_GLM_CONTEXT`: advertised context window, default `32768`.
- `CHITIN_OLLAMA_BIN`: alternate `ollama` executable path.

## Smoke Test

```sh
ollama launch claude --model glm-5.1 -- -p "say hi"
```

The command should start Claude Code and return a short answer without using a
cloud Anthropic endpoint. Ollama supplies `ANTHROPIC_BASE_URL` and
`ANTHROPIC_AUTH_TOKEN` to the child process.

## Registry Check

Restart the orchestrator worker and inspect the startup log:

```sh
journalctl -u chitin-orchestrator.service -n 100 | grep 'drivers registered' | grep claudecode-glm
```

The implementation registry line should include `claudecode-glm`.

## Forced Routing

For a local-only whole-spec dispatch:

```sh
CHITIN_DRIVER_ALLOW=claudecode-glm chitin-orchestrator schedule 120 --whole-spec
```

For a codex-only control run:

```sh
CHITIN_DRIVER_ALLOW=codex chitin-orchestrator schedule 120 --whole-spec
```

The `scheduler_started` chain event includes `driver_id` when a single
whole-spec driver is selected. Query the chain by `payload.driver_id` to split
local versus cloud dispatch counts.

## Troubleshooting

- `ollama binary "ollama" not found`: install Ollama or set `CHITIN_OLLAMA_BIN`.
- `ollama daemon not reachable at http://localhost:11434`: start the Ollama
  daemon and retry.
- `model glm-5.1 not present in ollama`: run `ollama pull glm-5.1`.
- `claude CLI binary "claude" not found`: install Claude Code on the worker
  host.
- `ollama v0.21+ required for launch subcommand`: upgrade Ollama.

## Empirical Measurement

This implementation verified the operator-host prerequisites that are safe to
check locally: `ollama` and `claude` are on `$PATH`, Ollama reports v0.21.0,
and `/api/tags` includes `remote_model: "glm-5.1"`.

The full SC-004 measurement intentionally remains an operator-host dispatch
because it creates a separate spec-117 PR. Keep codex as the fallback default
until that run is evaluated:

```sh
CHITIN_DRIVER_ALLOW=claudecode-glm chitin-orchestrator schedule 117 --whole-spec
```

Evaluate the resulting PR for task coverage, test pass rate, and review
defects before preferring `claudecode-glm` over codex globally.
