# Ollama Local Adapter

Thin adapter for parsing Ollama HTTP API responses into the chitin canonical event format.

## Scope

This is **not** an independent driver adapter like `claude-code` or `openclaw`. It's a utility library that:

- Parses Ollama `/api/generate` JSON responses into `ParsedOllamaResponse`
- Extracts token usage (`prompt_eval_count` + `eval_count`) into the chitin usage schema
- Is consumed by drivers that need to normalize Ollama output for the event chain

The openclaw plugin adapter (`apps/openclaw-plugin-governance/`) handles the actual `before_tool_call` hook surface for openclaw-driven agents (including local Ollama models like qwen3-coder and glm-4.7-flash).

## Exports

- `parseOllamaJSONResponse(body: string): ParsedOllamaResponse` — Parse a non-streaming Ollama response JSON
- `OllamaUsage`, `ParsedOllamaResponse` — Typed interfaces for the parsed result

## Relationship to other adapters

| Adapter | Role |
|---------|------|
| `libs/adapters/claude-code` | PreToolUse hook driver — captures all Claude Code tool events |
| `libs/adapters/openclaw` | Investigation adapter — documents the openclaw hook/plugin surface |
| `libs/adapters/ollama-local` | **This library** — utility for parsing Ollama responses |
| `apps/openclaw-plugin-governance` | The actual openclaw `before_tool_call` plugin that gates tool calls through `chitin-kernel` |