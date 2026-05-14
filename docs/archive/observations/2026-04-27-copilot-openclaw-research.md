# Copilot CLI ↔ openclaw Research Spike — 2026-04-27

Three parallel research agents over ~10 minutes. Hypothesis from the user: "GitHub may be going the opposite of Anthropic on this; openclaw may already have an integration." Both halves of the hypothesis confirmed, plus a more important finding the question didn't anticipate.

## TL;DR

1. **GitHub IS going the opposite way.** On 2026-04-02 (25 days ago) GitHub released `@github/copilot-sdk` under **MIT** as a "Copilot is the engine you embed in your own orchestrator" play. First-class BYOK supports Anthropic models. Anthropic's posture remains the inverse — Claude Code as a closed sovereign platform with subagents-inside. This is a real strategic fork, not stylistic difference.

2. **openclaw already integrates with Copilot CLI** — has since 2026-03-09. Two ways, both shipped and `enabledByDefault: true`:
   - `dist/extensions/github-copilot/` — uses **Copilot subscription as an LLM provider** (device-code OAuth to `api.individual.githubcopilot.com`).
   - `dist/extensions/acpx/` — drives **Copilot CLI as a subordinate agent** via `copilot --acp --stdio` (Agent Client Protocol over JSON-RPC).
   - We do not need to build "openclaw orchestrating Copilot" — that ship has shipped.

3. **The integration vector chitin actually wants is unfilled.** Copilot CLI's native plugin/extension API (`@github/copilot-sdk` `joinSession({tools, hooks})` with `onPreToolUse`, `onPostToolUse`, `onSessionStart`, …) is the inverse direction: chitin runs as a forked child *inside* a Copilot CLI session and sees every tool call before execution. Nobody has shipped this. PR #4469 in openclaw attempted the related "openclaw consumes the SDK" migration and was rejected on 2026-02-01 for feature-freeze reasons.

## What's already there (so we stop re-inventing it)

### Copilot CLI's native extensibility surface (1.0.35, installed)

Three coexisting tiers, all officially documented:

| Tier | Discovery | Mechanism | Use case |
|---|---|---|---|
| **Extensions** | `.github/extensions/<name>/extension.mjs` (project) or `~/.copilot/extensions/<name>/` (user); `--plugin-dir <dir>` ad-hoc | Forked child Node process, JSON-RPC over stdio, calls `joinSession({tools, hooks})` from `@github/copilot-sdk/extension` | Custom tools + lifecycle hooks (pre/post tool, session start/end, error) — chitin's natural fit |
| **Plugins** | `copilot plugin install/marketplace/...` from `copilot-plugins` and `awesome-copilot` registries | Directory with `plugin.json` mixing agents/skills/hooks/MCP servers | Marketplace tier above raw extensions |
| **Custom agents** | YAML files in `definitions/*.agent.yaml` | `--agent <name>` and `session.agent.*` RPC | Built-ins: `code-review`, `explore`, `research`, `rubber-duck`, `task`, `configure-copilot` |

Plus three orthogonal modes for *being driven from outside*:
- `--server --stdio` — headless JSON-RPC server on stdio (this is exactly what the Go SDK wraps)
- `--acp --stdio` — Agent Client Protocol mode, used by openclaw's `acpx`
- `--server` over TCP for hybrid TUI+RPC

The full RPC contract ships in the package: `schemas/api.schema.json` (annotated for "SDK codegen tools") and `schemas/session-events.schema.json` for the event stream. Methods include `session.fleet.start`, `session.tools.handlePendingToolCall`, `session.permissions.handlePendingPermissionRequest`, full `sessionFs.*`, `tools.list`, `models.list`, etc.

**Implication.** The Go SDK we built v1 around is a typed wrapper over this same JSON-RPC; nothing prevents speaking it directly from any language.

### MCP support

Copilot CLI is a first-class MCP **client** — local stdio, remote HTTP, remote SSE, plus in-process `registerExternalClient(...)`. Built-in `github-mcp-server` ships pre-wired. Config sources stack: `~/.copilot/mcp-config.json`, workspace `.mcp.json`, plugin-bundled servers, `--additional-mcp-config @file`. RPC methods: `mcp.config.{add,update,remove,enable,disable,list}`, `session.mcp.{enable,disable,list,oauth.login,reload}`. CLI flags: `--add-github-mcp-tool`, `--add-github-mcp-toolset`, `--enable-all-github-mcp-tools`, `--disable-builtin-mcps`.

It does **not** expose itself as an MCP server. Consumer only. (This is symmetric with Claude Code — both are MCP clients, neither is server.)

### openclaw's existing Copilot integrations

**`dist/extensions/github-copilot/`** — uses Copilot subscription as a model provider. Auth is GitHub device-code OAuth (`CLIENT_ID = "Iv1.b507a08c87ecfe98"`, scope `read:user`) → token exchange at `https://api.github.com/copilot_internal/v2/token`. Base URL `https://api.individual.githubcopilot.com`. Headers impersonate the VS Code Copilot Chat extension (`Editor-Version: vscode/1.96.2`, `User-Agent: GitHubCopilotChat/0.26.7`). Default model list includes `claude-sonnet-4.6`, `claude-sonnet-4.5`, `gpt-4o`, `gpt-4.1`, `o1`, `o3-mini`. `enabledByDefault: true`.

**`dist/extensions/copilot-proxy/`** — local shim against a running VS Code with the Copilot Proxy extension at `http://localhost:3000/v1`. Different model list (`gpt-5.2`, `claude-opus-4.6`, `gemini-3-pro`). Slated for deletion in PR #4469 but the PR was rejected.

**`dist/extensions/acpx/`** — the agent-driver. `register.runtime-CHt9wXwu.js:555-562, 1502, 1675-1683` ships a built-in agent registry with `copilot: "copilot --acp --stdio"`, alongside `claude-code`, `gemini`, `codex`, `qwen`, `iflow`, `qoder`, `cursor`. The `acpx/skills/acp-router/SKILL.md` documents `"copilot" → agentId: "copilot"` as a built-in alias. Merged into openclaw 2026-03-09 via `acpx#72` (closing `acpx#60` "native GitHub Copilot CLI agent support").

**The unfilled gap.** No published openclaw extension or third-party bridge registers chitin/openclaw tools+hooks **into a Copilot CLI session** via `@github/copilot-sdk/extension` (`joinSession`). PR #4469 attempted "openclaw consumes copilot-sdk" — rejected. The forward direction "chitin extends Copilot CLI" is an empty quadrant.

## Strategic posture: GitHub vs Anthropic on agent platforms

| Axis | GitHub Copilot | Anthropic Claude Code |
|---|---|---|
| Runtime distribution | `github/copilot-cli` proprietary; `github/copilot-sdk` **MIT** in 5 first-party langs (Node/Python/Go/.NET/Java) + community Rust/Clojure/C++ | Closed, obfuscated npm package; `@anthropic-ai/claude-code` Commercial ToS |
| Multi-agent story | "Embed Copilot in your own orchestrator" — built-in multi-agent + A2A; example pipelines mix Azure OpenAI agents + Copilot agents | Subagents = Claude-Code-spawning-Claude-Code; Skills are filesystem artifacts inside the session; orchestration center stays *inside* Claude Code |
| MCP | First-class client, builtin GitHub MCP server, `/mcp add`, `/plugin install owner/repo` | First-class client, full local/HTTP/in-process support |
| 3rd-party orchestrator endorsement | Copilot SDK *is* the embeddable piece; BYOK includes **Anthropic** models | None first-party. Cursor/Continue/Aider integrations are community-built; Anthropic recently blocked 3rd-party use of Claude Code subscriptions (HN 46549823) |
| License for embedding | MIT | Commercial ToS |

**Inference flagged as inference, not citation.** No public Anthropic manifesto reads "we will not be embeddable." The posture is read from product shape (closed binary + subagents-inside + the third-party-subscription-block) not from a declared strategy. Worth noting as observed-behavior in the talk.

## Recommendation

We have three integration vectors. They are not competitors — they answer different questions.

| Vector | What it gives chitin | Build cost | Status |
|---|---|---|---|
| **Provider/inference** (configure Copilot as one of openclaw's LLMs) | Nothing chitin-specific. openclaw users gain a model option | 0 — already shipped (`github-copilot` extension) | **Already done by openclaw** |
| **ACP driver** (openclaw spawns Copilot CLI as a subordinate via `--acp --stdio`) | openclaw observes Copilot's ACP events; chitin sees nothing unless it sits in front of the spawn | 0 — already shipped (`acpx copilot`) | **Already done by openclaw** |
| **Copilot CLI extension** (`extension.mjs` calling `joinSession({tools, hooks})`, registering chitin's `onPreToolUse`/`onPostToolUse` to gate every tool call inside a Copilot session) | Chitin governance hooks ride **inside** every Copilot CLI session — same place the user runs Copilot, no orchestrator wrapper required | Real work — but on a published MIT SDK | **Empty quadrant. This is the chitin-shaped hole.** |

**Recommendation: target the SDK-extension vector for v2 of the Copilot driver.** It is strictly better than the v1 Go-SDK-as-orchestrator path on three dimensions:

1. **No orchestrator wrapper.** Today users run `chitin-kernel drive copilot "..."`; the Go SDK spawns Copilot CLI as a child. With the extension model they run `copilot "..."` directly and chitin rides as a `~/.copilot/extensions/chitin/extension.mjs`. Lower friction, no fork in the user's invocation flow.
2. **Native lifecycle.** `onPreToolUse` is exactly the gate hook chitin needs — no permission-handler-vs-Ice-vocab mismatch, no LockdownCh workaround, no PrintEvent shim. The SDK was built for this.
3. **Stays sympathetic to GitHub's "embed me" posture.** Chitin becomes a citizen of the Copilot extension ecosystem, not a sidecar.

**Recommendation: do NOT build the provider/ACP-driver paths.** They exist; rebuilding is opportunity cost.

## Implications for the 2026-05-07 talk

The strategic finding is talk-grade material, not just an integration footnote. Two beats become available:

1. **Open-vendor governance (Copilot).** Chitin governs an *intentionally embeddable* engine. Demo: `extension.mjs` gating `onPreToolUse`. Sympathetic vendor — chitin is filling a gap the vendor designed for.
2. **Closed-vendor governance (Claude Code, future).** Chitin governs an *intentionally sovereign* platform. Demo would be the heavier lift (the wrapper-driver story) — adversarial vendor, governance is hostile-environment work.

The April 2026 news (Copilot SDK MIT public preview, BYOK-Anthropic, Anthropic blocking 3rd-party Claude Code use) is recent enough to be a live narrative beat, not a stale one. **The talk is the right venue to draw the contrast.**

For 2026-05-07 specifically: v1 (`feat/copilot-cli-governance-v1`) is the Go-SDK-as-orchestrator path. It works, it demos, it's frozen. Do not retarget for the extension model before the talk. **But:** add a slide acknowledging the extension-model exists and saying "v2 will move governance inside the Copilot session via this path" — it positions chitin as evolving with the vendor, and gives a clean roadmap close.

## Implications for chitin architecture (post-talk)

- **v2 Copilot driver**: TypeScript `extension.mjs` invoking `chitin-kernel gate evaluate` over a stable boundary (subprocess + JSON, or in-process via FFI/WASM). Decision is to keep the gov.Gate Go-side as canonical; extension is a thin shim that calls into it.
- **Retire v1 driver**: once v2 lands, `chitin-kernel drive copilot` becomes a deprecated convenience wrapper. The Go SDK, the wire-kind hack, the LockdownCh — all become legacy supporting the wrapper-mode demos.
- **Two-driver architecture as a permanent design principle.** Open vendors → in-process extension. Closed vendors → wrapping orchestrator. Chitin's governance API stays the same; the integration shim per vendor is what varies. This generalizes cleanly to future agent platforms.
- **openclaw integration stops being a chitin problem.** openclaw users who want governance install chitin's Copilot CLI extension just like everyone else; openclaw's `acpx` driving Copilot CLI inherits chitin's gating for free, since the gating happens inside Copilot's own session.

## Open questions for follow-up

1. **Verify PR/issue refs.** Agent 2 cited `openclaw/openclaw#4469` (rejected 2026-02-01) and `openclaw/acpx#60`/`#72` (merged 2026-03-09). These came from web search; before we cite in the talk, fetch them directly and confirm the dates and rationale.
2. **Confirm `@github/copilot-sdk` extension stability.** The SDK is in public preview as of 2026-04-02; is the `joinSession({tools, hooks})` surface stable enough to build against, or will it churn before our talk?
3. **License compatibility.** Copilot SDK is MIT. Chitin is OSS — confirm chitin's license (review `LICENSE` in the chitin root) is compatible with shipping a `~/.copilot/extensions/chitin/` package that depends on `@github/copilot-sdk`.
4. **MCP-server inversion?** Could chitin instead expose itself as an MCP server that Copilot CLI consumes for "policy checks," using the existing MCP client surface? Lower-trust integration vector — MCP servers don't get pre-tool-use hooks, they only provide tools. Probably not the right shape, but worth a one-page "why not MCP" companion before committing to the extension path.

## Sources

- Copilot CLI extensibility — local: `/home/red/.cache/copilot/pkg/linux-x64/1.0.35/copilot-sdk/docs/{extensions,agent-author}.md`, `extension.d.ts`, `preloads/extension_bootstrap.mjs`, `app.js` flag table, `schemas/{api,session-events}.schema.json`
- openclaw integrations — local: `/home/red/.nvm/versions/node/v22.22.1/lib/node_modules/openclaw/dist/extensions/{github-copilot,copilot-proxy,acpx}/`, schema `dist/zod-schema.core-CYrn8zgQ.js:8-18`
- [GitHub Copilot CLI GA changelog (2026-02-25)](https://github.blog/changelog/2026-02-25-github-copilot-cli-is-now-generally-available/)
- [Copilot SDK public preview changelog (2026-04-02)](https://github.blog/changelog/2026-04-02-copilot-sdk-in-public-preview/)
- [Copilot SDK getting started](https://docs.github.com/en/copilot/how-tos/copilot-sdk/sdk-getting-started)
- [Creating a plugin for Copilot CLI](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/plugins-creating)
- [Adding MCP servers for Copilot CLI](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers)
- [github/copilot-sdk repo (MIT)](https://github.com/github/copilot-sdk)
- [github/copilot-cli repo (proprietary)](https://github.com/github/copilot-cli)
- [InfoQ on Copilot SDK](https://www.infoq.com/news/2026/02/github-copilot-sdk/)
- [openclaw github-copilot provider docs](https://docs.openclaw.ai/providers/github-copilot)
- [Claude Code subagents](https://code.claude.com/docs/en/sub-agents) and [Skills](https://platform.claude.com/docs/en/agent-sdk/skills)
- [HN: Anthropic blocks 3rd-party Claude Code use](https://news.ycombinator.com/item?id=46549823)
- [anthropics/claude-code LICENSE](https://github.com/anthropics/claude-code/blob/main/LICENSE.md)
