import { evaluateHookGate, evaluateRouter, isExecShapedTool } from './chitin-bridge.mjs';

const PLUGIN_ID = 'chitin-governance';

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
      const evaluate = isExecShapedTool(event.toolName) ? evaluateHookGate : evaluateRouter;
      const decision = await evaluate(
        {
          agent: ctx.agentId ?? 'openclaw-plugin',
          tool: event.toolName,
          params: event.params ?? {},
          cwd: process.cwd(),
          // Stable session id per (agent, cwd) so the floundering
          // heuristic can read prior chain events for this session.
          sessionId: `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}`,
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

export default plugin;
