# Chitin as an OpenClaw Plugin — Distribution & Integration Design

**Date:** 2026-05-01
**Status:** Investigation — written design. No code yet. Companion to `2026-04-30-local-worker-design-addendum.md` (three-plane reframe). Locks the answer to "should chitin ship as an openclaw plugin?" — yes, with a scope boundary that the plugin pivot **does not** dissolve.
**Active lens:** da Vinci (open-ended cross-surface architecture, no clear single invariant to prove yet — `souls/canonical/davinci.md`).
**Constraints honored:** Anthropic ToS (`project_anthropic_tos_constraints.md`); chitin OSS boundary (`feedback_chitin_oss_boundary.md`); kernel-authority rule (`docs/architecture/layer-contracts.md` v1).
**Supersedes:** the slice 1e plan ("ACP-server mode for the Copilot shim") **only for openclaw's native pi-runtime drivers** (local-coder / local-judge / local-glm). Slice 1e remains valid as the integration shape for acpx-subprocess drivers (Copilot CLI, future closed-vendor agents).

---

## TL;DR

Yes — ship chitin as an openclaw plugin (`openclaw-plugin-chitin-governance`). OpenClaw exposes a first-class `before_tool_call` lifecycle hook with deny semantics, params rewriting, and approval-routing — the exact surface a governance kernel needs. Wiring chitin into that hook turns "install chitin + per-driver shims" into "install one openclaw plugin and every openclaw-orchestrated agent is gated for free."

The pivot is **scoped, not total**. The plugin's `before_tool_call` fires for tool calls dispatched by openclaw's *native pi-runtime harness* — i.e., the local-coder / local-judge / local-glm tier. Tool calls inside an **acpx-spawned subprocess** (claude-code interactive, codex, copilot CLI) do *not* traverse the plugin hook surface — those subprocesses have their own tool runtimes inside the spawned process. So the plugin replaces per-driver shims for the worker swarm's local tier; it does *not* replace the Claude Code PreToolUse hook (PR #66) or the Copilot SDK shim (PR #51).

The three-plane lock from 2026-04-30 holds. The talk arc on 2026-05-07 stays on J3. The plugin lands post-talk as the natural slice 2/3 follow-on.

---

## 1. Hook surface findings (the load-bearing question)

OpenClaw 2026.4.25's plugin SDK exposes a typed lifecycle event system via `api.on(hookName, handler, opts?)`. The full event set (from `dist/plugin-sdk/src/plugins/hook-types.d.ts`, `PluginHookName` union):

```
before_model_resolve   before_prompt_build   before_agent_start
before_agent_reply     model_call_started    model_call_ended
llm_input              llm_output            before_agent_finalize
agent_end              before_compaction     after_compaction
before_reset           inbound_claim         message_received
message_sending        message_sent          before_tool_call ⭐
after_tool_call ⭐     tool_result_persist ⭐ before_message_write
session_start          session_end           subagent_spawning
subagent_delivery_target subagent_spawned    subagent_ended
gateway_start          gateway_stop          before_dispatch
reply_dispatch         before_install ⭐
```

The four ⭐ entries are load-bearing for chitin governance:

### 1.1 `before_tool_call` — the pre-tool-execution gate

```ts
before_tool_call: (
  event: { toolName, params, runId?, toolCallId? },
  ctx:   { agentId?, sessionKey?, sessionId?, runId?, trace?, toolName, toolCallId? }
) => Promise<PluginHookBeforeToolCallResult | void> | PluginHookBeforeToolCallResult | void;

PluginHookBeforeToolCallResult = {
  params?:         Record<string, unknown>;   // can rewrite params
  block?:          boolean;                    // can DENY the tool call
  blockReason?:    string;
  requireApproval?: {                         // can route to user-approval flow
    title, description, severity, timeoutMs,
    timeoutBehavior: 'allow' | 'deny',
    pluginId, onResolution: (PluginApprovalResolution) => void,
  };
};
```

This is exactly the contract `gov.Gate.Evaluate()` needs to bind to. **Block-with-reason** is native; **params rewriting** lets governance redact secrets in flight; **requireApproval** routes to openclaw's existing channel-based approval gateway (the same machinery that asks "approve this dangerous shell command?" through configured chat channels).

Dispatch site verified: `dist/pi-tools.before-tool-call-CKqfsSYm.js` — fires from openclaw's pi-agent-core harness path, before the tool function runs. Loop detection runs first; then the plugin hook; result respected.

### 1.2 `after_tool_call` & `tool_result_persist` & `registerAgentToolResultMiddleware`

Three post-execution surfaces, each with different scope:

- `after_tool_call` (hook): **pi-runtime** only. Receives `{ toolName, params, result, error, durationMs }`. Fire-and-forget — no return value affects flow.
- `tool_result_persist` (hook): **pi-runtime** only. Can rewrite the persisted message before it lands in the session transcript.
- `registerAgentToolResultMiddleware` (registry method): **`runtimes: 'pi' | 'codex'`**. Can wrap and modify the result for both runtime families. This is the broader surface — covers codex's app-server harness too.

Combined: the chitin event chain captures pre + post for both runtime families, with chain-of-events linkage.

### 1.3 `before_install` — plugin/skill install audit

Chitin's "where do plugins/skills come from?" hook. Returns `{ block?, blockReason?, findings? }`. Lets chitin enforce supply-chain policies (signed plugins, pinned versions, no `git`-kind installs in worker-mode).

### 1.4 `subagent_spawning` — orchestration gate

Returns `{ status: 'ok', threadBindingReady?, deliveryOrigin? } | { status: 'error', error }`. Chitin can deny subagent spawns based on agent-id allowlists — the cleanest enforcement primitive for the Anthropic-ToS constraint. In worker-mode, `event.agentId === 'claude-code'` → return `{ status: 'error', error: 'claude-code disallowed as worker driver per ToS' }`.

### 1.5 What's NOT exposed

- No hook for tool calls **inside an acpx subprocess**. When openclaw spawns claude-code or codex via ACP, those agents' internal tool runtimes live in the child process. The `before_tool_call` hook fires only for tool calls dispatched by openclaw's own pi-runtime. Implication below in §3.
- No generic "intercept arbitrary RPC" hook. Plugin governance is bounded to the lifecycle events openclaw chooses to surface.

### 1.6 Scope summary

| Surface | Who's gated | Surface in plugin |
|---|---|---|
| openclaw native pi-runtime agents (local-coder, local-judge, local-glm via ollama) | ✅ tool calls, results, session, subagent, install | `before_tool_call`, `after_tool_call`, `subagent_*`, `before_install`, `session_*` |
| openclaw codex-runtime agents | ⚠️ results only (no codex-runtime `before_tool_call`) | `registerAgentToolResultMiddleware` (post-hoc), `subagent_*` (lifecycle), `session_*` |
| acpx-subprocess agents (claude-code, codex-via-ACP, copilot CLI v1) | ❌ tool calls invisible to plugin | only `subagent_*` lifecycle events (spawn/end) |

The plugin pivot is a **partial replacement**, not total. This is the central architectural fact the rest of the design lives with.

---

## 2. Proposed plugin shape

```ts
// openclaw-plugin-chitin-governance/src/index.ts
import { definePluginEntry } from 'openclaw/plugin-sdk/plugin-entry';
import { evaluateGate, emitEvent }
  from './chitin-bridge.js';   // wraps `chitin-kernel gate evaluate` subprocess

export default definePluginEntry({
  id: 'chitin-governance',
  name: 'Chitin Governance',
  description:
    'Execution kernel for AI coding agents. Every tool call gated by chitin policy; ' +
    'every event lands in a hash-linked chain that also emits OTEL spans.',
  configSchema: () => ({
    type: 'object',
    additionalProperties: false,
    properties: {
      kernelPath:    { type: 'string', minLength: 1 },     // chitin-kernel binary
      mode:          { type: 'string', enum: ['enforce','observe'] },
      workerMode:    { type: 'boolean', default: false },  // bootstrap rules ON
      otelEmit:      { type: 'boolean', default: true },
      denyOnError:   { type: 'boolean', default: true },   // fail closed
    },
    required: ['kernelPath'],
  }),
  register(api) {
    // ── pre-tool gate (pi runtime only) ────────────────────────────
    api.on('before_tool_call', async (event, ctx) => {
      const decision = await evaluateGate({
        agent: ctx.agentId ?? 'openclaw',
        tool:  event.toolName,
        params: event.params,
        sessionKey: ctx.sessionKey,
        runId:      event.runId ?? ctx.runId,
        toolCallId: event.toolCallId,
        callerOrigin: 'openclaw-plugin',          // self-identify per #79
      });
      if (decision.allow) {
        // optional: chitin may rewrite params (e.g., redact secrets)
        return decision.params ? { params: decision.params } : undefined;
      }
      return {
        block: true,
        blockReason: decision.reason ?? 'denied by chitin policy',
      };
    });

    // ── post-tool capture (pi + codex runtimes) ────────────────────
    api.registerAgentToolResultMiddleware(async (event, mctx) => {
      await emitEvent({
        kind: 'post_tool_use',
        toolName: event.toolName,
        toolCallId: event.toolCallId,
        runtime:    mctx.runtime,
        sessionKey: mctx.sessionKey,
        runId:      mctx.runId,
        result:     event.result,
        isError:    event.isError,
      });
      // chitin does not mutate the result here — chain captures, doesn't transform
    });

    // ── subagent gate: enforces ToS + driver allowlist ─────────────
    api.on('subagent_spawning', async (event, ctx) => {
      const decision = await evaluateGate({
        kind:    'subagent_spawn',
        agent:   event.agentId,
        mode:    event.mode,
        sessionKey: event.childSessionKey,
        callerOrigin: 'openclaw-plugin',
      });
      if (!decision.allow) {
        return { status: 'error', error: decision.reason };
      }
      return { status: 'ok' };
    });

    // ── install-time security gate ─────────────────────────────────
    api.on('before_install', async (event, ctx) => {
      const decision = await evaluateGate({
        kind: 'plugin_install',
        targetType: event.targetType,
        targetName: event.targetName,
        request: event.request,
        callerOrigin: 'openclaw-plugin',
      });
      if (!decision.allow) {
        return { block: true, blockReason: decision.reason };
      }
    });

    // ── lifecycle telemetry ────────────────────────────────────────
    api.on('session_start', async (event, ctx) => {
      await emitEvent({ kind: 'session_start', sessionId: event.sessionId, sessionKey: event.sessionKey });
    });
    api.on('session_end', async (event, ctx) => {
      await emitEvent({
        kind: 'session_end',
        sessionId: event.sessionId,
        reason:    event.reason,
        durationMs: event.durationMs,
        messageCount: event.messageCount,
      });
    });
    api.on('subagent_ended', async (event, ctx) => {
      await emitEvent({
        kind: 'subagent_ended',
        sessionKey: event.targetSessionKey,
        outcome: event.outcome,
        reason:  event.reason,
      });
    });
  },
});
```

Notes on shape:

- `definePluginEntry` is the canonical entry helper from `openclaw/plugin-sdk/plugin-entry`.
- `configSchema` declares the plugin's own config (under `chitinGovernance.*` in `openclaw.json`); orthogonal to chitin's existing `chitin.yaml`.
- `callerOrigin: 'openclaw-plugin'` self-identifies the gate caller per the convention added in PR #79 (`feat(gov): self-identify gate callers that bypass envelope (caller_origin)`) — preserves the audit-truth invariant when the plugin proxies into the Go kernel.
- Plugin does NOT register tools, providers, or commands — it's pure governance. This keeps the plugin minimal and lets it coexist cleanly with everything else openclaw is doing.
- Plugin does NOT host policy logic. All `evaluateGate` calls subprocess to `chitin-kernel gate evaluate`. Policy lives where it always has — in the Go kernel. See §3.

---

## 3. Boundary decision: Go kernel authority, TS plugin as thin adapter

The boundary call: where does `gov.Gate.Evaluate()` live post-pivot — in the Go kernel as a subprocess called by the plugin, or hosted in TypeScript inside the plugin itself?

**Decision: Go kernel stays canonical. The TS plugin is a thin adapter that subprocesses to `chitin-kernel gate evaluate --hook-stdin --agent=openclaw-plugin`.**

Reasons:

1. **Layer Contracts kernel-authority rule.** "Chitin remains the only policy authority on tool calls." Splitting policy across Go and TS would mean two rule engines, two source-of-truth files, two drift surfaces. The plugin would silently diverge from the standalone CLI and Claude Code hook the moment a rule changed.
2. **Architectural hard rule** (`project_architectural_rules.md`): Go kernel owns all side effects; TS is read-only. The plugin's *side effect* is "call the kernel"; the kernel writes the chain row, mutates envelope state, emits OTEL. TS does not.
3. **PR #51 precedent.** The Copilot SDK shim is exactly this pattern: a thin TS adapter that delegates every gating decision to `chitin-kernel`. Same pattern, different host.
4. **Performance is not a constraint.** The gate subprocess is microseconds (it's a single Go binary that reads stdin and writes JSON to stdout). Tool dispatch latency is dominated by the model call, not the gate.
5. **Source-side minimization** (`feedback_chitin_minimal_source_side.md`). The plugin is "source-side code that lives in someone else's runtime." It should be as thin as possible. All normalization, policy evaluation, event-chain writing, OTEL emit, decisions-stream writing — those live in the canonical Go kernel where they already work.

What the TS plugin contains beyond `api.on` wiring:

- `chitin-bridge.js` — subprocess invocation, stdin/stdout protocol, error handling, fail-closed-on-kernel-unreachable behavior (controlled by `denyOnError` config).
- A small in-memory cache for the chitin-kernel binary path resolution (skip cold-start lookup on every call).
- Nothing else. No yaml parsing, no event chain, no rules engine, no decisions stream.

Roughly ~300 LOC of TS for the entire plugin. The `before_tool_call` handler is ~20 lines. Most of the bulk is config schema, error handling, and the bridge.

---

## 4. Three-plane interaction — what survives, what shifts

The 2026-04-30 three-plane lock (`project_three_plane_architecture.md`) is plane-orthogonal to where chitin's hook attaches. The pivot does not redraw plane boundaries; it relocates the integration seam.

| Plane | Pre-pivot wire | Post-pivot wire | Status |
|---|---|---|---|
| **Control (Temporal)** | Schedules workflows. Activity invokes `chitin-kernel task validate` (pre-activity) → `acpx <agent>` (dispatch). | Unchanged. | Locked. |
| **Execution (OpenClaw)** | Spawns agent via acpx; pi-runtime path has no chitin gate (corner-cut documented in slice 1d). | pi-runtime path now invokes `before_tool_call` → chitin-governance plugin → kernel gate. acpx-subprocess path unchanged (still relies on per-agent shim). | **Now plane-correct for pi-runtime.** acpx-subprocess gap stays. |
| **Enforcement (Chitin)** | Go kernel called by per-driver shim (claude-code hook, copilot SDK joinSession, F4 OTEL emitter). | Same Go kernel; now ALSO called from openclaw plugin for openclaw-native runtime. Two new caller_origins: `openclaw-plugin` (was just `openclaw` before). | Audit truth preserved; one more caller surface. |

Layer Contracts compliance check (per `docs/architecture/layer-contracts.md` v1):

- *Kernel authority:* policy lives in Go kernel; plugin is a caller. ✓
- *Driver constraint:* `allowed_drivers` still policy output; plugin can enforce it via `subagent_spawning` block on disallowed `agentId`. ✓
- *Routing scope (capacity-only):* Temporal still does capacity; the plugin doesn't make routing decisions. ✓
- *Aggregation role:* event chain still canonical; OTEL still projection. The plugin emits chain rows via the kernel (one source of truth), not directly. ✓

The four-rule check passes. The pivot is plane-correct.

---

## 5. Migration path — what stays, what gets ported, what can retire

### Stays as-is

- **PR #51 — Copilot SDK shim** (`chitin-kernel drive copilot --acp --stdio`). Copilot CLI v1 runs as an acpx subprocess; its internal tool calls are not visible to the openclaw plugin. The SDK shim is the right surface — it's the *open-vendor in-process* path from `project_two_driver_pattern.md`. Pivot does not touch this.
- **PR #66 — Claude Code PreToolUse hook**. Claude Code is *interactive only* per Anthropic ToS; never a worker driver. Its PreToolUse hook lives in the Claude Code config (`.claude/settings.json`) and runs entirely inside Claude Code's process. The openclaw plugin pivot is irrelevant to this surface. Hook stays.
- **F4 OTEL emit MVP**. Stays as the canonical chain-to-OTEL bridge. The plugin emits chain rows via the kernel (which already runs F4); the plugin does not also OTEL-emit directly. One emit site keeps trace_id/span_id mapping deterministic.
- **Standalone `chitin-kernel` CLI**. Independent install path for users who run chitin without openclaw. Two distribution channels — openclaw-plugin and standalone — coexist; plugin is *additive*.
- **`project_two_driver_pattern.md`** as a memory. Still accurate: open-vendor (Copilot v2 SDK), closed-vendor (Copilot v1 wrap, Claude Code) shims still apply for those vendors. Plugin pivot is a *third* shape — "openclaw-native runtime gating" — not a replacement.

### Gets superseded (partially)

- **Slice 1e — ACP-server mode for Copilot shim.** This was scoped to put chitin in front of the Copilot shim as an ACP server openclaw could spawn. Under the plugin pivot, that's only needed for *acpx-subprocess* drivers. For openclaw-native pi-runtime drivers, the plugin's `before_tool_call` covers the gating directly without an ACP-server intermediary. Slice 1e shrinks to "remains valid for closed-vendor / acpx-subprocess drivers like Copilot CLI v1; not built for the local-coder / local-judge / local-glm tier" — those are now plugin-gated.
- **Slice 1d's corner-cut justification.** Slice 1d shipped the Copilot tier through the chitin shim with openclaw NOT in the loop. That was honest tier-1 proof and the corner-cut was documented. The plugin pivot is the structural answer to "openclaw should be in the loop" for local-tier drivers (slice 2 work). Don't backfill slice 1d; let the corner-cut stand as documented.

### New work the pivot creates

- **`openclaw-plugin-chitin-governance/` package.** New repo or new directory — open question, see §7.
- **`chitin-bridge.ts`.** The subprocess adapter. ~150 LOC, mostly error handling.
- **Plugin marketplace listing.** Once openclaw publishes a plugin marketplace, chitin-governance gets listed alongside acpx, diagnostics-otel, memory-core. (Distribution-channel work, post-talk.)
- **Acceptance test.** End-to-end: openclaw drives local-coder against ollama, makes a tool call, plugin intercepts, chitin denies, openclaw surfaces the deny back to the agent. Mirrors the slice 1d acceptance test but via the plugin path instead of the shim path.

---

## 6. Talk arc impact — stay on J3, plugin lands post-talk

Current locked talk arc (J3 in `project_framing_v1.md`):

> Copilot-first slide → Demo 1 → kernel reveal → Demos 2-5 → bridging slide (Claude Code + openclaw configs side-by-side, same chitin.yaml) → F4 OTEL trace beat → forward-line close.

The plugin pivot is tempting as a *stronger* kernel reveal: "chitin runs INSIDE openclaw, governing every agent it orchestrates." But:

1. **It's only stronger if it's true across the demo set.** The demos in J3 lean on Claude Code and Copilot CLI — both of which are *outside* the plugin's pre-tool gate. Claiming "chitin runs inside openclaw governing every agent" while showing Claude Code demos that are governed by a *separate* PreToolUse hook is dishonest framing. The reveal would need a footnote, and footnotes kill reveals.
2. **Seven days is not enough to build it well.** Plugin scaffolding, bridge subprocess, end-to-end acceptance, openclaw plugin marketplace coordination, package publishing — even if all greenfield, this is post-talk slice-2 work. Cutting corners to land it pre-talk re-creates the slice-1d corner-cut problem at higher stakes.
3. **The current J3 reveal is already strong.** "Same chitin.yaml gates Claude Code, Copilot, and openclaw via three different shim shapes" *is* the closed-loop story. The plugin pivot makes that story cleaner for one of the three drivers (openclaw-native pi-runtime), but the story works without it.

**Recommendation: do not modify the talk arc.** Land the plugin in slice-2 / slice-3 (post-2026-05-07). Use it as the "what's next" beat in the forward-line close — *"the next slice ships chitin as an openclaw plugin: any team running openclaw can install one package and get tool-call gating, hash-linked event chain, and OTEL emit across every agent openclaw orchestrates."* That's a stronger forward-line than a footnote-heavy reveal would be — and it doubles as the distribution pitch (§7.4).

What this preserves: hero sentence, driver discipline, demo-earns-reframe principle, the "let the demo earn it" hard-won lesson from the framing-v1 grill-me sequence.

What this defers: the "plugin marketplace as primary distribution" pitch. That's a Q3-Q4 narrative, not a 7-days-out narrative.

---

## 7. Open questions (deferred — non-blocking for the pivot decision)

1. ~~**Repo location.**~~ **Resolved 2026-05-01: monorepo, `apps/openclaw-plugin-governance/`, `npm publish` from the subdirectory.** Contract pinning and single CI gate beat the slightly-cleaner separate-repo publishing path. The plugin and the kernel ship together, version-locked.
2. **Codex-runtime `before_tool_call` parity.** OpenClaw's hook system has `before_tool_call` firing from pi-runtime; codex-runtime gets `registerAgentToolResultMiddleware` post-hoc only. Does that asymmetry matter for the swarm? Local-coder/judge/glm all run on pi-runtime via ollama, so for the worker swarm the answer is "no, pi-runtime is fine." If a future driver lands on codex-runtime (unlikely — Codex is OpenAI's), revisit. **Defer.**
3. **Plugin install authenticity.** OpenClaw's plugin install path (`before_install` hook) is itself the place to enforce signed-plugin or pinned-version policy. Self-referentially: chitin-governance plugin enforces install-time security on chitin-governance plugin updates? Cute but tractable — the *first* install isn't covered (chicken-and-egg), every subsequent install is. **Defer to slice-3 supply-chain hardening.**
4. **`registerCodexAppServerExtensionFactory` (bundled-only seam).** OpenClaw allows codex tool-result middleware via `registerCodexAppServerExtensionFactory`, but only "bundled plugins" can use it (`contracts.embeddedExtensionFactories` allowlist). Is chitin a candidate to land in that allowlist? Reach out to Steinberger post-talk. **Not blocking.**
5. **Plugin reload semantics.** OpenClaw supports plugin hot-reload via `OpenClawPluginReloadRegistration`. Should `chitin-governance` register reload behavior, or fail closed when reloaded mid-session? **Lean fail-closed.** Decide before publishing v0.1.
6. ~~**Two-emit-site OTEL dedup.**~~ **Resolved 2026-05-01: there is no second emit site.** The plugin subprocesses every gate decision to `chitin-kernel gate evaluate`, and the kernel's F4 emit translator (chain → OTEL spans, one-way bridge per `project_otel_emit_direction.md`) does the OTEL work. Plugin contains zero OTEL code. We get the translator we already wrote, for free, with the chain-canonical / OTEL-projection invariant intact. OpenClaw's own `diagnostics-otel` plugin is orthogonal — it emits openclaw-internal spans (model calls, channel routing) under different semantics; the two streams compose, they don't compete. The "wherever is clever" answer turns out to be: kernel, because it's where the translator already lives.

---

## 8. Acceptance criteria — replacement for slice 1e (post-talk slice-2)

The plugin pivot replaces slice 1e for openclaw-native drivers. Done shipping when:

- [ ] `apps/openclaw-plugin-governance/` exists (or separate repo per Q1) with `package.json`, `openclaw.plugin.json`, `index.ts`, `chitin-bridge.ts`.
- [ ] Plugin loads under `openclaw plugin install <local-path>` and registers without errors.
- [ ] `before_tool_call` hook handler successfully subprocesses to `chitin-kernel gate evaluate --hook-stdin --agent=openclaw-plugin`; allow / deny / params-rewrite cases all work end-to-end.
- [ ] `subagent_spawning` denies an attempt to spawn `agentId: 'claude-code'` when `workerMode: true` (ToS enforcement primitive).
- [ ] `before_install` denies a `git`-kind install spec when `workerMode: true`.
- [ ] `registerAgentToolResultMiddleware` writes a post_tool_use chain row tagged with `runtime: 'pi'` or `runtime: 'codex'`.
- [ ] End-to-end: openclaw spawns local-coder (`ollama qwen3-coder:30b`) → agent makes a Bash tool call → plugin intercepts → chitin denies (rule: `worker:no-recursive-delete` for `rm -rf`) → openclaw surfaces deny back to agent → chain row written with `caller_origin: 'openclaw-plugin'`, `decision: 'deny'`.
- [ ] Standalone `chitin-kernel` CLI still works for non-openclaw users (no regressions).
- [ ] Layer Contracts compliance audit passes (kernel authority, driver constraint, routing scope, aggregation role).
- [ ] PR #51 (Copilot shim) and PR #66 (Claude Code hook) confirmed unaffected — their integration tests still green.

---

## 9. What this design does commit to (and what it doesn't)

**Commits to:**

- **Plugin marketplace as a primary distribution channel.** Not a side door; not a Q3-only narrative. The plugin is built so that *any* team running openclaw can `openclaw plugin install chitin-governance` (or pull from the marketplace once it's live) and get tool-call gating, hash-linked event chain, and OTEL emit without ever knowing about chitin's standalone CLI. README and onboarding written for that audience: someone who came from the openclaw side and has never read a chitin doc. The OSS framing makes this credible — there's nothing Readybench-internal to gate.
- **Marketplace-grade discoverability.** Plugin name, description, configSchema, uiHints, configContracts.dangerousFlags all written to read well in the openclaw plugin browser. Chitin's category noun ("execution kernel for AI coding agents") leads the description.
- **Adoption asymmetry deliberately exploited.** Standalone install requires a chitin user; plugin install requires only an openclaw user — a much larger addressable population. The plugin pulls people across the boundary in the right direction (toward gating + audit), without forcing them to commit to chitin's CLI before seeing value.

**Does not commit to:**

- A timeline. Slice 2 follows the talk; pacing TBD by the user.
- Replacing the standalone CLI. Plugin is *additive*; standalone CLI remains canonical for non-openclaw users and for CI / scripting contexts where openclaw isn't in play.
- Coupling chitin's release cadence to openclaw's. Plugin is versioned independently; consumes openclaw's plugin SDK as a peer dependency, not a hard dependency.
- A talk-arc rewrite. J3 stays.

---

**Carry-forward to next session:** plugin shape locked. Boundary locked (Go kernel canonical, TS plugin thin). Migration path locked (PR #51 / PR #66 / F4 stay; slice 1e shrinks; plugin is new work). Talk arc locked (no change). Repo location resolved: monorepo at `apps/openclaw-plugin-governance/`. OTEL question resolved: kernel emits via the existing F4 translator; plugin contains zero OTEL code. Distribution intent locked: marketplace-first onboarding for non-chitin openclaw users. Remaining open questions (codex parity, install authenticity, codex-app-server seam, reload semantics) are non-blocking — defer to slice-2 kickoff. Next concrete step is a slice-2 plan that turns §8's acceptance criteria into a build-and-merge sequence — not in scope for this design.
