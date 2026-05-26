# Spec 120 Runbook: claudecode-glm Driver

`claudecode-glm` runs the Claude Code CLI through Ollama's built-in Claude
integration:

```sh
ollama launch claude --model glm-5.1 -- <claude-code args>
```

## Prerequisites

- Ollama v0.21 or newer. Older versions do not provide the `launch` subcommand.
- Pull the default model once:

```sh
ollama pull glm-5.1
```

- Claude Code CLI installed and visible on `PATH` as `claude`.
- Ollama daemon reachable at `http://localhost:11434`.

Optional overrides:

- `CHITIN_CLAUDECODE_GLM_MODEL`: model name, default `glm-5.1`.
- `CHITIN_CLAUDECODE_GLM_CONTEXT`: context tokens, default `32768`.
- `CHITIN_OLLAMA_BIN`: ollama executable path, default `ollama`.

## Smoke Test

```sh
ollama launch claude --model glm-5.1 -- -p "say hi"
```

## Verify Registration

Start the worker host and inspect startup logs for the registered driver set:

```sh
journalctl -u chitin-orchestrator -n 200 | grep 'drivers registered'
journalctl -u chitin-orchestrator -n 200 | grep 'claudecode-glm'
```

The driver card should show:

- `driver_id`: `claudecode-glm`
- `agent_runtime`: `claude-code`
- `model`: `glm-5.1` unless overridden
- `tier`: `local`
- `cost_class`: `free`
- capabilities: `code.implement`, `code.spec-implement`

## Force Routing

Run a whole-spec dispatch with only the local GLM driver registered:

```sh
CHITIN_DRIVER_ALLOW=claudecode-glm chitin-orchestrator schedule --whole-spec 120-claudecode-glm-driver
```

Compare against the cloud fallback:

```sh
CHITIN_DRIVER_ALLOW=codex chitin-orchestrator schedule --whole-spec 120-claudecode-glm-driver
```

On a successful single-node whole-spec dispatch, the `scheduler_started`
chain event includes `driver_id`, so local-vs-cloud dispatch share can be
queried from the chain.

## Troubleshooting

- `ollama binary "ollama" not found`: install Ollama or set `CHITIN_OLLAMA_BIN`.
- `ollama daemon not reachable at http://localhost:11434`: start the Ollama service.
- `model glm-5.1 not present in ollama`: run `ollama pull glm-5.1`.
- `Claude Code runtime "claude" not found`: install Claude Code CLI and ensure it is on `PATH`.
- `ollama v0.21+ required for launch subcommand`: upgrade Ollama; the driver does not use litellm or a separate proxy.

## Empirical Measurement

Spec 120's integration path is in place. On this implementation host,
Ollama v0.21.0 and Claude Code 2.1.144 were present, but the local
`glm-5.1` model was not pulled. `ollama list` showed `glm-5.1:cloud`,
and the required smoke command failed with:

```text
Error: model "glm-5.1" not found; run 'ollama pull glm-5.1' first, or use --yes to auto-pull
```

Do not treat `glm-5.1:cloud` as the zero-cost local target for this driver.
Pull `glm-5.1` locally before running the spec 117 empirical check:

```sh
ollama pull glm-5.1
CHITIN_DRIVER_ALLOW=claudecode-glm chitin-orchestrator schedule --whole-spec 117-file-overlap-edge-creates
```

Evaluate the resulting PR for task coverage, test quality, and review
findings. Until that PR passes normal review, keep codex as the default
whole-spec route and use `claudecode-glm` explicitly for zero-cost trials or
cloud-quota fallback.
