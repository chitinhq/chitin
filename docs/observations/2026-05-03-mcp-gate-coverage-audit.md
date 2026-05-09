# MCP gate coverage audit (2026-05-03)

> **Cull note (2026-05-08):** §7 references `libs/governance/` (TS classifier), which was deleted in the 2026-05-08 cull. The Go kernel is now the canonical policy evaluator. §6 references `apps/runner/`, also deleted.

## Question

Which MCP tool calls reach `gov.Policy.Evaluate`? Which slip through?

## Method

Surveyed every code path that receives an MCP tool call and traced whether it normalizes to a `gov.Action` and calls the kernel gate.

## Coverage map

### Gated paths (✓)

1. **Copilot SDK driver** (`go/execution-kernel/internal/driver/copilot/normalize.go:84-98`)
   - Receives `PermissionRequest.Kind=Mcp`, normalizes to `gov.ActMCPCall` with `target="serverName/toolName"`
   - Calls `gov.Policy.Evaluate` via `chitin-kernel drive copilot`
   - Tests: `internal/driver/copilot/normalize_test.go:103-128`

2. **CLI direct gate** (`cmd/chitin-kernel/main.go:801-906`)
   - Entry: `chitin-kernel gate evaluate --tool <name> --args-json <json>`
   - Calls `gov.Normalize(tool, args)` → `gov.Policy.Evaluate(action, agent, envelope)`

3. **Claude Code hook stdin (post-fix)** (`cmd/chitin-kernel/main.go:816`)
   - Entry: `chitin-kernel gate evaluate --hook-stdin --agent=claude-code`
   - Routes through `internal/driver/claudecode/normalize.go::Normalize`
   - **Pre-fix gap:** MCP tool names (`mcp__server__tool`) fell through to `ActUnknown` → `default-deny-unknown`
   - **Fix:** added `parseMCPToolName` + a case ahead of the `ActUnknown` fallback that routes to `gov.ActMCPCall` with `target="server/tool"`. See [PR #257](https://github.com/chitinhq/chitin/pull/257).

4. **OpenClaw plugin** (`apps/openclaw-plugin-governance/src/chitin-bridge.mjs:31-83`)
   - Spawns `chitin-kernel gate evaluate --tool <name> --args-json <params>`

### Ungated paths (advisory only)

5. **Router advisory layer** (`go/execution-kernel/cmd/chitin-kernel/router_hook.go:67-130`)
   - Calls kernel verdict (gated ✓), then runs heuristics + optional advisor subprocess
   - **The advisor itself is not gated** — its recommendations don't touch the kernel gate
   - Acceptable: kernel deny is final; advisor can only soften, not break-through

6. **Temporal-worker hook wrapper** (`apps/runner/src/router/hook-wrapper.ts:65-90`)
   - Same shape as #5; same constraint.

7. **TS governance libs** (`libs/governance/src/classifier.ts`)
   - In-process classifier; lines 36-49 cover `claude_code_pretooluse`+Bash and `openclaw_before_tool_call`+shell.exec
   - **Gap:** `ingress='mcp'` returns `action_class='unclassified'` (line ~145 fixture)
   - This is the advisory path — not security-critical because the kernel gate is the authoritative line

## Gaps remediated by this PR

- **Gap A (closed):** Claude Code MCP tools now normalize to `gov.ActMCPCall` instead of `ActUnknown`. Operators can now write `action: mcp.call, target: "github/*"` rules and have them match real Claude Code MCP traffic.

## Gaps deferred

- **Gap B (deferred):** TS classifier MCP ingress rule. Advisory-only; not security-critical. Filed as backlog entry `tooling/mcp-ts-classifier-ingress`.
- **Gap C (deferred):** Re-gate advisor recommendations. The kernel deny is final, so this is policy-architectural rather than security. Filed as `governance/regate-advisor-recommendations`.

## Verification

- `internal/driver/claudecode/normalize_test.go` — `TestNormalize_MCPToolRoutesToMCPCall` covers 3 wire-format variants (server+tool, tool with embedded underscores, server-only)
- `TestNormalize_NonMCPNotMisclassified` — guards the `mcpDebug`-style false-positive case
- Full module: 797 pass across 23 packages

## Why this matters

Pre-fix, an operator running enforce mode with the default rule set would see EVERY MCP call from Claude Code denied. The fix unblocks the policy author to write meaningful MCP rules (`allow target=github/*`, `deny target=filesystem/write_*`) instead of having to add a blanket `allow action=unknown` exception that disables every other fail-closed protection.
