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
      mode: { type: 'string', enum: ['enforce', 'observe'], default: 'enforce' },
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

    // ── post-tool capture (pi + codex runtimes) ─────────────────────────
    api.registerAgentToolResultMiddleware(
      async (event, mctx) => {
        try {
          await evaluateGate(
            {
              agent: mctx.agentId ?? 'openclaw-plugin',
              tool: `_observe.post_tool_use.${event.toolName}`,
              params: {
                tool_call_id: event.toolCallId,
                runtime: mctx.runtime,
                is_error: event.isError ?? false,
              },
              cwd: process.cwd(),
            },
            { ...cfg, denyOnError: false },
          );
        } catch (err) {
          log.warn(
            `chitin post-tool emit failed: ${err instanceof Error ? err.message : String(err)}`,
          );
        }
      },
      { runtimes: ['pi', 'codex'] },
    );
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
