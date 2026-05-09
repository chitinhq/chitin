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

Production support here means the driver has a pre-side-effect
integration path, a kernel normalizer or kernel-backed bridge, and tests
or fixture coverage in this repository. Guidance-only surfaces are not
governance boundaries.

| Driver | Integration point | Real-time gating | Normalizer / bridge | Fixture location | Production support | Current gaps |
|---|---|---:|---|---|---|---|
| Claude Code | Native `PreToolUse` hook installed by `chitin-kernel install --surface claude-code --global` or `scripts/install-claude-code-hook.sh` | Yes | `go/execution-kernel/internal/driver/claudecode` | `go/execution-kernel/internal/driver/claudecode/*_test.go` | Supported | Future Anthropic tools intentionally hit `unknown` until mapped from live captures. |
| Codex CLI | Native `PreToolUse` hook installed by `scripts/install-codex-hook.sh` into `~/.codex/config.toml` | Yes | `go/execution-kernel/internal/driver/codex` | `go/execution-kernel/internal/driver/codex/normalize_test.go` | Supported | Narrow native enum; any new Codex tool must be added from live hook captures before policy loosening. |
| Gemini CLI | Native `BeforeTool` hook installed by `scripts/install-gemini-hook.sh` into `~/.gemini/settings.json` | Yes | `go/execution-kernel/internal/driver/gemini` | `go/execution-kernel/internal/driver/gemini/normalize_test.go` | Supported | Last tool-registry check in comments was Gemini CLI `0.40.1`; reverify on upgrade. |
| Copilot CLI | In-kernel SDK wrapper through `chitin-kernel drive copilot` | Yes | `go/execution-kernel/internal/driver/copilot` | `go/execution-kernel/internal/driver/copilot/*_test.go` | Supported for wrapped CLI execution | Closed-vendor wrapper only. This does not cover VS Code Copilot agent-mode tool execution. |
| OpenClaw | `before_tool_call` plugin via `apps/openclaw-plugin-governance` and `libs/adapters/openclaw` | Yes, for OpenClaw-dispatched tool calls | Plugin bridge into `chitin-kernel gate evaluate` | `apps/openclaw-plugin-governance/test/*.test.ts` | Supported for OpenClaw runtime traffic | Does not gate standalone Claude/Codex/Gemini/Copilot processes; use their native driver integrations. Kernel has no dedicated `internal/driver/openclaw` normalizer. |
| Hermes | `pre_tool_call` hook installed by `scripts/install-hermes-hook.sh` or plugin files in `docs/governance-setup-extras/` | Yes | `go/execution-kernel/internal/driver/hermes` | `go/execution-kernel/internal/driver/hermes/normalize_test.go` | Supported | `image_generate`, `text_to_speech`, `vision_analyze`, `cronjob`, and `clarify` are intentionally unmapped. Decide canonical types before allowing them. |
| VS Code Copilot | Repository instructions through `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md`, and `AGENTS.md` | No | None | None | Guidance only | No pre-tool hook surface. Treat this as instructions, not governance. Use `chitin-kernel drive copilot` for enforceable terminal-side Copilot execution. |

## Near-term work

1. ~~Mine `default-deny` / `unknown` rows from `~/.chitin/gov-decisions-*.jsonl`~~ **Done.** Cross-driver conformance test (`go/execution-kernel/internal/driver/cross_driver_conformance_test.go`) and openclaw-specific conformance test (`go/execution-kernel/internal/gov/normalize_openclaw_conformance_test.go`) now catch ActUnknown regressions at test time. Two bugs found and fixed: claudecode was missing `NotebookRead` and `TodoRead`; gov.Normalize was missing 11 openclaw tool names.
2. ~~Add a fixture and normalizer test for each mapped tool before changing `chitin.yaml`.~~ **Done.** See the conformance tests above.
3. For Hermes modality tools, decide whether the canonical vocabulary needs new action types (`media.generate`, `speech.generate`, `vision.analyze`, `schedule.job`) or whether they should stay fail-closed as substrate features.
4. For VS Code Copilot, keep instructions current and explicit that IDE guidance is not governance. The enforceable Copilot path remains `chitin-kernel drive copilot`.

## External status notes

- VS Code and GitHub Copilot support repository-wide instructions at
  `.github/copilot-instructions.md`, path-specific files in
  `.github/instructions/*.instructions.md`, and `AGENTS.md` for agent
  context.
- VS Code currently exposes `github.copilot.chat.codeGeneration.useInstructionFiles`
  and `chat.useAgentsMdFile` settings for these instruction surfaces.
- None of those instruction files are a security boundary. They improve
  behavior but do not replace chitin's gate.
