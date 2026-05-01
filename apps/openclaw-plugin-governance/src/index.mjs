import { evaluateGate } from './chitin-bridge.mjs';

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
      // Default 'observe' matches openclaw.plugin.json (single source of truth).
      // Slice 2 ships observe-default because chitin's normalizer doesn't
      // recognize openclaw chat-domain tools yet — enforce would deadlock
      // small agents on every unknown tool. Slice 3 extends the normalizer
      // and flips the default.
      mode: { type: 'string', enum: ['enforce', 'observe'], default: 'observe' },
      workerMode: { type: 'boolean', default: false },
      denyOnError: { type: 'boolean', default: true },
      timeoutMs: { type: 'number', minimum: 100, default: 5000 },
    },
  }),

  register(api) {
    const cfg = resolveConfig(api.pluginConfig);
    const log = api.logger;

    log.info(
      `chitin-governance registering: kernelPath=${cfg.kernelPath} mode=${cfg.mode} workerMode=${cfg.workerMode}`,
    );

    // ── pre-tool gate (pi runtime) ───────────────────────────────────────
    api.on('before_tool_call', async (event, ctx) => {
      const decision = await evaluateGate(
        {
          agent: ctx.agentId ?? 'openclaw-plugin',
          tool: event.toolName,
          params: event.params ?? {},
          cwd: process.cwd(),
        },
        cfg,
      );

      if (decision.allow) {
        return decision.params ? { params: decision.params } : undefined;
      }

      if (cfg.mode === 'observe') {
        log.warn(
          `[observe] would-deny ${event.toolName}: ${decision.reason ?? 'no reason'} (rule=${decision.ruleId ?? 'unknown'})`,
        );
        return undefined;
      }

      log.info(
        `chitin denied tool=${event.toolName} rule=${decision.ruleId ?? 'unknown'} reason=${decision.reason ?? '(none)'}`,
      );
      return {
        block: true,
        blockReason: decision.reason ?? 'denied by chitin policy',
      };
    });

    // ── subagent gate: enforces ToS + worker driver allowlist ────────────
    api.on('subagent_spawning', async (event, _ctx) => {
      if (cfg.workerMode && event.agentId === 'claude-code') {
        log.info(
          `chitin denied subagent spawn agent=claude-code (Anthropic ToS — claude-code is interactive-only, not a worker driver)`,
        );
        return {
          status: 'error',
          error:
            'claude-code is not allowed as a worker subagent (Anthropic ToS — see chitin/memory/project_anthropic_tos_constraints.md)',
        };
      }
      return { status: 'ok' };
    });

    // ── plugin/skill install audit ──────────────────────────────────────
    api.on('before_install', async (event, _ctx) => {
      if (!cfg.workerMode) return undefined;
      const kind = event.request?.kind;
      if (kind === 'plugin-git') {
        log.info(`chitin denied install kind=plugin-git in worker mode`);
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
 * @param {Record<string, unknown> | undefined} raw
 */
function resolveConfig(raw) {
  const r = raw ?? {};
  return {
    kernelPath: typeof r.kernelPath === 'string' && r.kernelPath ? r.kernelPath : 'chitin-kernel',
    mode: r.mode === 'enforce' ? 'enforce' : 'observe',
    workerMode: r.workerMode === true,
    denyOnError: r.denyOnError !== false,
    timeoutMs: typeof r.timeoutMs === 'number' && r.timeoutMs >= 100 ? r.timeoutMs : 5000,
  };
}

export default plugin;
