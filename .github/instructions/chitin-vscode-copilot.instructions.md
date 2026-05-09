---
applyTo: ".vscode/**,.github/copilot-instructions.md,.github/instructions/**,AGENTS.md,docs/driver-conformance.md,docs/governance-setup.md"
---

VS Code Copilot instructions are guidance, not enforcement. Do not describe
them as a chitin gate or policy boundary.

For enforceable Copilot execution, use the kernel wrapper
`chitin-kernel drive copilot`. For IDE use, keep Copilot pointed at
`AGENTS.md`, `.github/copilot-instructions.md`, and these path-specific
instruction files so it follows the same product boundary as other agents.

When documenting IDE setup, state that Copilot's VS Code agent mode does not
currently expose the same pre-tool hook surface as Claude Code, Codex CLI,
Gemini CLI, Hermes, or OpenClaw.
