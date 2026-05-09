# Driver conformance matrix

Status: active operator reference. Keep this file aligned with
`go/execution-kernel/internal/driver/*/normalize.go` and the installer
scripts.

Chitin's moat is one canonical action vocabulary across heterogeneous
agent drivers. A driver is conformant when every tool-call surface lands
in one of these outcomes:

- A canonical `gov.ActionType` with a meaningful target.
- `unknown`, deliberately fail-closed, with a documented gap.
- A structured cross-driver warning when another driver's tool name leaked
  through the wrong hook.

## Current surfaces

| Driver | Integration | Normalizer | Coverage | Current gaps |
|---|---|---|---|---|
| Claude Code | `PreToolUse` hook via `chitin-kernel install --surface claude-code --global` | `internal/driver/claudecode` | Bash, read/write/edit, web, MCP, task/delegate, task-state, worktree, cron/schedule, browse tools, todo | Future Anthropic tools intentionally hit `unknown` until mapped. |
| Codex CLI | `PreToolUse` hook via `scripts/install-codex-hook.sh` | `internal/driver/codex` | Bash, `apply_patch`, `read_file`, MCP, Claude-tool leak fallback | Narrow native enum; any new Codex tool should be added from live hook captures before policy loosening. |
| Gemini CLI | `BeforeTool` hook via `scripts/install-gemini-hook.sh` | `internal/driver/gemini` | Shell, read/list/search, edit/replace/write, web/search, memory/topic, Claude-tool leak fallback | Last tool-registry check in comments was Gemini CLI `0.40.1`; reverify on upgrade. |
| Hermes | `pre_tool_call` shell hook via `scripts/install-hermes-hook.sh` | `internal/driver/hermes` | Terminal/code, file, patch/search, web/browser, delegation, skills, kanban plumbing, process, MCP, Claude-tool leak fallback | `image_generate`, `text_to_speech`, `vision_analyze`, `cronjob`, and `clarify` are intentionally unmapped. Decide canonical types before allowing them. |
| Copilot CLI | In-kernel SDK wrapper via `chitin-kernel drive copilot` | `internal/driver/copilot` | SDK permission kinds: shell, write, read, MCP, URL, memory, custom tool, hook | Closed-vendor wrapper only. This does not cover VS Code Copilot agent-mode tool execution. |
| OpenClaw | `before_tool_call` plugin via `apps/openclaw-plugin-governance` | Plugin bridge into `chitin-kernel gate evaluate` | Tool calls dispatched by OpenClaw's native pi-agent-core runtime | Does not gate standalone Claude/Codex/Gemini/Copilot processes; use their native driver integrations. |
| VS Code Copilot | Repository instructions + `AGENTS.md` context | No execution normalizer | Uses repo guidance to steer agent behavior in the IDE | No pre-tool hook surface. Treat this as guidance only; route terminal-side agent execution through chitin-aware CLIs where enforcement is required. |

## Near-term work

1. Mine `default-deny` / `unknown` rows from `~/.chitin/gov-decisions-*.jsonl`
   by `(agent, tool_name, action_target)` and map the highest-volume real
   tools first.
2. Add a fixture and normalizer test for each mapped tool before changing
   `chitin.yaml`.
3. For Hermes modality tools, decide whether the canonical vocabulary needs
   new action types (`media.generate`, `speech.generate`, `vision.analyze`,
   `schedule.job`) or whether they should stay fail-closed as substrate
   features.
4. For VS Code Copilot, keep instructions current and explicit that IDE
   guidance is not governance. The enforceable Copilot path remains
   `chitin-kernel drive copilot`.

## External status notes

- VS Code and GitHub Copilot support repository-wide instructions at
  `.github/copilot-instructions.md`, path-specific files in
  `.github/instructions/*.instructions.md`, and `AGENTS.md` for agent
  context.
- VS Code currently exposes `github.copilot.chat.codeGeneration.useInstructionFiles`
  and `chat.useAgentsMdFile` settings for these instruction surfaces.
- None of those instruction files are a security boundary. They improve
  behavior but do not replace chitin's gate.
