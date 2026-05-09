# Claude Code Adapter

The first and most mature chitin driver adapter. Captures all Claude Code tool-call events and forwards them to the chitin kernel for governance evaluation and chain recording.

## How it works

This adapter registers as a PreToolUse hook in Claude Code's settings. When Claude Code is about to execute a tool call, it invokes the hook, which:

1. **Receives** a `HookInput` (event name, tool name, tool input, session ID)
2. **Normalizes** the event type via `hookToEventType()` (maps `PreToolUse` → `intended`, `PostToolUse` → `executed`/`failed`, etc.)
3. **Dispatches** to `chitin-kernel gate evaluate` for policy evaluation
4. **Records** the decision in the tamper-evident event chain

The adapter spawns `chitin-kernel` as a subprocess per hook event (matching Claude Code's per-hook-process model).

## Hook event mapping

| Claude Code event | chitin event_type |
|---|---|
| `SessionStart` | `session_start` |
| `UserPromptSubmit` | `user_prompt` |
| `PreToolUse` | `intended` |
| `PostToolUse` (success) | `executed` |
| `PostToolUse` (error) | `failed` |
| `PreCompact` | `compaction` |
| `SubagentStop` | `session_end` |
| `SessionEnd` | `session_end` |

## Key types

- **`HookInput`** — Claude Code's hook payload (event name, tool metadata, session ID, subagent info)
- **`AdapterContext`** — Per-invocation context (run ID, session ID, surface identity, kernel binary path)
- **`HookResult`** — The outcome (event type, emitted seq, hash, skip flag)

## Install path

The adapter is wired into Claude Code via PreToolUse hook configuration. The kernel binary path is resolved from `CHITIN_KERNEL_BINARY` env var or defaults to `chitin-kernel`.

## Relationship to other drivers

| Driver | Surface | Integration |
|---|---|---|
| `claude-code` (this) | Claude Code hooks | `PreToolUse` / `PostToolUse` hook dispatch |
| `codex` | Codex CLI hooks | `--agent=codex` flag on kernel |
| `gemini` | Gemini CLI hooks | `--agent=gemini` flag on kernel |
| `copilot` | VS Code Copilot SDK | `chitin-kernel drive copilot` in-process |
| `openclaw` | OpenClaw `before_tool_call` plugin | `apps/openclaw-plugin-governance/` |
| `hermes` | Hermes `pre_tool_call` hook | `scripts/install-hermes-hook.sh` |