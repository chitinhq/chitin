import { spawn } from 'node:child_process';
import { evaluateHookGate, evaluateRouter, isExecShapedTool } from './chitin-bridge.mjs';

const PLUGIN_ID = 'chitin-governance';

// Spec 091 (FR-008/FR-009): per-session state for the stop-hook reentrancy
// guard and the forced-continuation counter.
//   stopHookActive[sid]    — true ⇒ session is in lockdown; subsequent calls
//                            short-circuit to {block,stop} without invoking
//                            the kernel. Sticky once set within a process.
//   forcedContinuations[sid] — count of block-decisions for this session that
//                              did NOT carry continue:false. When it reaches
//                              FORCED_CONTINUATION_CAP, set stopHookActive
//                              and emit `stop_signal_ignored` to the chain.
// Both are module-scoped Maps; lifetime = openclaw process. The kernel-side
// lockdown counter handles cross-restart durability (existing mechanism).
// sessionId construction matches the existing pattern below at line 58:
//   `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}`
const stopHookActive = new Map();
const forcedContinuations = new Map();
const FORCED_CONTINUATION_CAP = 3;

const plugin = {
  id: PLUGIN_ID,
  name: 'Chitin Governance',
  description:
    'Execution kernel for AI coding agents — gate every tool call through chitin policy and record a hash-linked event chain.',
  configSchema: () => ({
    type: 'object',
    additionalProperties: false,
    properties: {
      kernelPath: { type: 'string', minLength: 1, default: 'chitin-kernel' },
      // Slice 3: default flipped from 'observe' to 'enforce'. Safe because
      // chitin's normalizer now covers all 19 tools the openclaw `main`
      // agent exposes (PR #83 + this slice) — every tool call lands a
      // policy-meaningful action_type instead of ActUnknown, which means
      // each call hits a real chitin.yaml rule (default-allow-* for safe
      // ops; specific deny rules for dangerous ones). Operators can opt
      // back to observe via the manifest config — it's flagged as
      // dangerous in openclaw.plugin.json's configContracts.dangerousFlags.
      mode: { type: 'string', enum: ['enforce', 'observe'], default: 'enforce' },
      workerMode: { type: 'boolean', default: false },
      denyOnError: { type: 'boolean', default: true },
      timeoutMs: { type: 'number', minimum: 100, default: 5000 },
    },
  }),

  /**
   * Plugin registration entry point called by the openclaw plugin loader.
   *
   * IMPORTANT: plugin code MUST NOT write to stdout. Openclaw and downstream
   * consumers parse stdout as JSON (hook protocol / stdio transport). Any
   * stdout write corrupts that stream. Use api.logger exclusively — .warn and
   * .error route to stderr in the openclaw runtime; .info may route to stdout
   * depending on loader version, so prefer .warn for all plugin diagnostics.
   */
  register(api) {
    const cfg = resolveConfig(api.pluginConfig);
    const log = api.logger;

    log.warn(
      `chitin-governance registering: kernelPath=${cfg.kernelPath} mode=${cfg.mode} workerMode=${cfg.workerMode}`,
    );

    // ── pre-tool gate (pi runtime) ───────────────────────────────────────
    api.on('before_tool_call', async (event, ctx) => {
      const sessionId = `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}`;

      // Spec 091 FR-008: stop-hook reentrancy guard. If the kernel already
      // emitted continue:false (or the FR-009 cap fired) for this session,
      // short-circuit subsequent calls to a fast {block,stop} response
      // WITHOUT invoking the kernel again. This breaks the lockdown loop:
      // even though openclaw's harness will keep calling before_tool_call
      // (it does not consume the `stop` field — R1 Case B), each retry
      // returns instantly with a non-escalating block, so the kernel's
      // lockdown_loop_detected counter never accumulates more deny events
      // for the same rule.
      if (stopHookActive.get(sessionId)) {
        return {
          block: true,
          blockReason: 'chitin: stop signal previously emitted; agent loop must terminate',
          stop: true,
        };
      }

      const evaluate = isExecShapedTool(event.toolName) ? evaluateHookGate : evaluateRouter;
      const decision = await evaluate(
        {
          agent: ctx.agentId ?? 'openclaw-plugin',
          tool: event.toolName,
          params: event.params ?? {},
          cwd: process.cwd(),
          // Stable session id per (agent, cwd) so the floundering
          // heuristic can read prior chain events for this session.
          sessionId,
        },
        cfg,
      );

      if (decision.allow) {
        return Object.keys(decision.params ?? {}).length > 0 ? { params: decision.params } : undefined;
      }

      if (cfg.mode === 'observe') {
        log.warn(
          `[observe] would-deny ${event.toolName}: ${decision.reason ?? 'no reason'} (rule=${decision.ruleId ?? 'unknown'})`,
        );
        return undefined;
      }

      // Spec 091 FR-007: honor continue:false from the kernel. Set the
      // reentrancy guard and emit a high-signal log line that the
      // openclaw-gateway journal captures, so operators (or an outer
      // watcher) can detect the stop attempt even though the openclaw
      // harness loop won't honor the `stop` field directly (R1 Case B).
      if (decision.continue === false) {
        stopHookActive.set(sessionId, true);
        log.error(
          `chitin-stop-signal sessionId=${sessionId} rule=${decision.ruleId ?? 'unknown'} reason=${decision.stopReason ?? decision.reason ?? '(none)'}`,
        );
        return {
          block: true,
          blockReason: decision.stopReason ?? decision.reason ?? 'denied by chitin policy',
          stop: true,
        };
      }

      // Spec 091 FR-009: track forced continuations on regular denies. If we
      // see CAP consecutive denies for a session without a continue:false
      // signal, treat that as evidence the harness is ignoring our stops
      // (or the kernel's lockdown counter hasn't fired yet) and force-stop
      // the session ourselves. Emit a stop_signal_ignored chain event so
      // the orchestrator side can route the failure.
      const count = (forcedContinuations.get(sessionId) ?? 0) + 1;
      forcedContinuations.set(sessionId, count);
      if (count >= FORCED_CONTINUATION_CAP) {
        stopHookActive.set(sessionId, true);
        log.error(
          `chitin: ${count} forced continuations for sessionId=${sessionId} — marking session failed (stop_signal_ignored)`,
        );
        // Fire-and-forget — telemetry emission failure must not break the deny path.
        emitStopSignalIgnored({
          sessionId,
          agentId: ctx.agentId,
          count,
          cap: FORCED_CONTINUATION_CAP,
          lastRuleId: decision.ruleId,
          lastReason: decision.reason,
          kernelPath: cfg.kernelPath,
          log,
        }).catch((err) => {
          log.error(`chitin: emitStopSignalIgnored failed: ${err instanceof Error ? err.message : String(err)}`);
        });
        return {
          block: true,
          blockReason: 'chitin: forced-continuation cap exceeded — session terminated',
          stop: true,
        };
      }

      log.warn(
        `chitin denied tool=${event.toolName} rule=${decision.ruleId ?? 'unknown'} reason=${decision.reason ?? '(none)'}`,
      );
      return {
        block: true,
        blockReason: decision.reason ?? 'denied by chitin policy',
      };
    });

    // ── subagent gate: ToS-driven Claude-Code denylist ───────────────────
    // Anthropic ToS forbids spawning Claude Code as a subagent under any
    // orchestrator. The constraint is a hard rule, not a workerMode toggle —
    // workerMode is a separate concept (worker bootstrap rules) and was
    // previously gating this check, allowing default-config openclaw to spawn
    // claude-code freely. Match is case-insensitive against the family name
    // so 'Claude-Code', 'claude_code', 'claude-code-2', '@anthropic/claude-code'
    // are all caught — the check is on the category, not one literal id.
    api.on('subagent_spawning', async (event, _ctx) => {
      if (isClaudeCodeAgent(event.agentId)) {
        log.warn(
          `chitin denied subagent spawn agent=${event.agentId} (Anthropic ToS — Claude Code is interactive-only, not a worker subagent)`,
        );
        return {
          status: 'error',
          error:
            'Claude Code is not allowed as a subagent (Anthropic ToS — see chitin/memory/project_anthropic_tos_constraints.md)',
        };
      }
      return { status: 'ok' };
    });

    // ── plugin/skill install audit ──────────────────────────────────────
    api.on('before_install', async (event, _ctx) => {
      if (!cfg.workerMode) return undefined;
      const kind = event.request?.kind;
      if (kind === 'plugin-git') {
        log.warn(`chitin denied install kind=plugin-git in worker mode`);
        return {
          block: true,
          blockReason:
            'git-kind plugin installs disallowed in worker mode (signed/pinned only)',
        };
      }
      return undefined;
    });

    // Post-tool capture (v2 post_tool_use chain emit) is slice 3 work — the
    // current `chitin-kernel emit` path takes a JSON event file, not a flag-
    // based call, so wiring it from here means writing+reading a temp file
    // per tool call. Deferred until the kernel exposes a streaming emit
    // subcommand. The before_tool_call gate already lands a gov-decisions
    // row per call, which is the audit-grade record for slice 2.
  },
};

/**
 * Match the Claude Code agent family. Catches 'claude-code', 'Claude-Code',
 * 'claude_code', 'claude-code-2', '@anthropic/claude-code', etc. The category
 * is what's ToS-restricted — the literal id is not a stable identifier.
 *
 * @param {unknown} agentId
 * @returns {boolean}
 */
export function isClaudeCodeAgent(agentId) {
  if (typeof agentId !== 'string') return false;
  return /claude[-_ ]?code/i.test(agentId);
}

/**
 * Apply config defaults and coerce types for the plugin runtime. Exported
 * for direct test of the slice 3 default-enforce flip.
 *
 * @param {Record<string, unknown> | undefined} raw
 */
export function resolveConfig(raw) {
  const r = raw ?? {};
  return {
    kernelPath: typeof r.kernelPath === 'string' && r.kernelPath ? r.kernelPath : 'chitin-kernel',
    // Slice 3: default-enforce. Only explicit 'observe' opts out.
    mode: r.mode === 'observe' ? 'observe' : 'enforce',
    workerMode: r.workerMode === true,
    denyOnError: r.denyOnError !== false,
    // Default 30s: covers the router pipeline, including pure-Go signals and
    // optional plugin subprocess checks. The pre-router gate path was 5s,
    // bumped because evaluateRouter replaced evaluateGate as the default
    // invocation surface.
    timeoutMs: typeof r.timeoutMs === 'number' && r.timeoutMs >= 100 ? r.timeoutMs : 30000,
  };
}

/**
 * Spec 091 FR-009: emit a `stop_signal_ignored` chain event when the plugin's
 * forced-continuation counter hits the cap. Routes through `chitin-kernel emit`
 * so the kernel remains the only chain-writer (constitution §1).
 *
 * Fire-and-forget: telemetry failure must not corrupt the deny path. Caller
 * .catch()es and logs.
 *
 * Event schema: see specs/091-fix-clawta-lockdown-loop/contracts/stop-signal-ignored-event.md
 *
 * @param {{
 *   sessionId: string,
 *   agentId: string | undefined,
 *   count: number,
 *   cap: number,
 *   lastRuleId: string | undefined,
 *   lastReason: string | undefined,
 *   kernelPath: string,
 *   log: { warn: (msg: string) => void, error: (msg: string) => void },
 * }} args
 */
export async function emitStopSignalIgnored(args) {
  const event = {
    event_type: 'stop_signal_ignored',
    agent_instance_id: args.agentId ?? 'openclaw-plugin',
    session_id: args.sessionId,
    payload: {
      continuation_count: args.count,
      cap: args.cap,
      last_rule_id: args.lastRuleId ?? null,
      last_reason: args.lastReason ?? null,
    },
    ts: new Date().toISOString(),
  };
  return new Promise((resolve, reject) => {
    const child = spawn(args.kernelPath, ['emit', '-event-json', '-'], {
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let stderr = '';
    child.stderr.on('data', (d) => {
      stderr += d.toString();
    });
    child.on('error', (err) => reject(err));
    child.on('close', (code) => {
      if (code === 0) resolve(undefined);
      else reject(new Error(`chitin-kernel emit exited ${code}${stderr ? `: ${stderr.slice(0, 200)}` : ''}`));
    });
    child.stdin.end(JSON.stringify(event));
  });
}

/**
 * Spec 091: test-only helpers to inspect / reset module state. The plugin's
 * stop-hook map and counter map are intentionally process-scoped (per the
 * data-model.md); these helpers exist to let vitest assert on state cleanly
 * without exporting the raw Maps (which would invite production misuse).
 *
 * @internal — test surface only.
 */
export function __test_resetState() {
  stopHookActive.clear();
  forcedContinuations.clear();
}
export function __test_isStopHookActive(sessionId) {
  return stopHookActive.get(sessionId) === true;
}
export function __test_getForcedContinuationCount(sessionId) {
  return forcedContinuations.get(sessionId) ?? 0;
}
export const __test_FORCED_CONTINUATION_CAP = FORCED_CONTINUATION_CAP;

export default plugin;
