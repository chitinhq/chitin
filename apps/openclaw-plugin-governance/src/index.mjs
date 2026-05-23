import { spawn } from 'node:child_process';
import { evaluateHookGate, evaluateRouter, isExecShapedTool } from './chitin-bridge.mjs';

const PLUGIN_ID = 'chitin-governance';

// Spec 091 (FR-008/FR-009): per-session state for the stop-hook reentrancy
// guard and the forced-continuation counter.
//   stopHookActive[sid]    — true ⇒ session is in lockdown; subsequent calls
//                            short-circuit to {block,stop} without invoking
//                            the kernel. Sticky within a process UNTIL the
//                            v1.1 unlock check clears it (see AFR-003 below).
//   forcedContinuations[sid] — count of block-decisions for this session that
//                              did NOT carry continue:false. When it reaches
//                              FORCED_CONTINUATION_CAP, set stopHookActive
//                              and emit `stop_signal_ignored` to the chain.
//   stopHookActiveEpoch[sid] — (v1.1 AFR-002) the kernel's lock_epoch at the
//                              moment stopHookActive was set. Used by the
//                              two-condition clear path in AFR-003. null when
//                              the status query at lock-set time failed; in
//                              that case any future `locked:false` response
//                              triggers a clear.
// All three are module-scoped Maps; lifetime = openclaw process. The kernel-
// side lockdown counter handles cross-restart durability (existing mechanism).
// sessionId construction matches the existing pattern at the api.on('before_tool_call')
// callback below: `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}`
const stopHookActive = new Map();
const stopHookActiveEpoch = new Map();
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
      //
      // Spec 091 v1.1 AFR-003: BEFORE returning the sticky block, consult
      // the kernel's session status. The two-condition clear path:
      //   (a) kernel reports locked:false → operator unlocked → clear
      //   (b) kernel reports lock_epoch > cached → new lock generation
      //       has happened (e.g., operator re-locked) and may have been
      //       unlocked since; the cached epoch is stale, clear and let
      //       the next call re-evaluate naturally.
      // If neither condition is met → sticky block stands.
      // If the status query fails → fail-closed (sticky block stands;
      // AFR-005). Absence of a positive unlock signal means "stay stopped".
      if (stopHookActive.get(sessionId)) {
        const agentForStatus = ctx.agentId ?? 'openclaw-plugin';
        let cleared = false;
        let kernelLockEpoch = null;
        try {
          const status = await querySessionStatus(agentForStatus, cfg.kernelPath);
          if (status !== null) {
            const cachedEpoch = stopHookActiveEpoch.get(sessionId);
            kernelLockEpoch = status.lock_epoch;
            const epochAdvanced =
              typeof cachedEpoch === 'number' && typeof status.lock_epoch === 'number'
                ? status.lock_epoch > cachedEpoch
                : false;
            // null cachedEpoch (status query failed at lock-set) means
            // ANY locked:false response clears the flag — there's no
            // meaningful epoch to compare. AFR-002 fallback path.
            if (status.locked === false || epochAdvanced) {
              cleared = true;
            }
          }
        } catch (err) {
          // AFR-005: fail-closed. Status query failure means we cannot
          // verify an unlock; keep the sticky block. Log so the operator
          // can see the failed query in stderr.
          log.warn(
            `chitin: session status query failed for ${agentForStatus}: ${err instanceof Error ? err.message : String(err)} — keeping sticky stop`,
          );
        }

        if (cleared) {
          stopHookActive.delete(sessionId);
          stopHookActiveEpoch.delete(sessionId);
          forcedContinuations.delete(sessionId);
          log.warn(
            `chitin: cleared sticky stop for sessionId=${sessionId} (kernel reports unlocked or epoch advanced to ${kernelLockEpoch ?? '?'})`,
          );
          // AFR-004: emit stop_hook_cleared chain event so the chain
          // distinguishes "kernel cleared the lock" (kernel side, via
          // spec 096's session_unlocked event) from "plugin actually
          // noticed and resumed" (this event). Fire-and-forget.
          emitStopHookCleared({
            sessionId,
            agentId: ctx.agentId,
            kernelLockEpoch,
            kernelPath: cfg.kernelPath,
            log,
          }).catch((err) => {
            log.error(`chitin: emitStopHookCleared failed: ${err instanceof Error ? err.message : String(err)}`);
          });
          // Fall through to the normal evaluateRouter / evaluateHookGate
          // path so the current tool call is gated against the (now-
          // relaxed) policy. The flag-clear MUST NOT cause us to allow
          // unconditionally; the operator unlock is a permission to
          // re-evaluate, not a blanket allow.
        } else {
          return {
            block: true,
            blockReason: 'chitin: stop signal previously emitted; agent loop must terminate',
            stop: true,
          };
        }
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
        // Spec 091 v1.1 AFR-002: capture lock_epoch at lock-set time
        // so AFR-003's two-condition clear can detect transitions later.
        // null on query failure — AFR-003 treats null as "any locked:false
        // clears" (fallback path; without an epoch reference any unlock
        // is by definition a transition).
        await captureLockEpoch(sessionId, ctx.agentId ?? 'openclaw-plugin', cfg.kernelPath, log);
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
        // Spec 091 v1.1 AFR-002: capture lock_epoch here too — this is the
        // other path that sets stopHookActive (the forced-continuation cap).
        // Same fallback semantics as the continue:false path above.
        await captureLockEpoch(sessionId, ctx.agentId ?? 'openclaw-plugin', cfg.kernelPath, log);
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
 * Spec 091 v1.1 AFR-002: query `chitin-kernel session status -agent <id>` and
 * return the parsed JSON, or null if the query failed (binary missing, exit
 * non-zero, malformed output, agent unknown). Used by AFR-003's two-condition
 * clear and by captureLockEpoch at lock-set time.
 *
 * The kernel's session subcommand (spec 096) emits JSON on stdout like:
 *   {"agent":"clawta","locked":false,"locked_ts":"...","unlock_ts":"...",
 *    "lock_epoch":6,"total":12,"level":"normal"}
 * On unknown agent it exits non-zero with an error envelope on stderr; we
 * return null in that case (no row = no transition signal to read).
 *
 * @param {string} agentId
 * @param {string} kernelPath
 * @returns {Promise<{locked: boolean, lock_epoch: number|null} | null>}
 */
export async function querySessionStatus(agentId, kernelPath) {
  return new Promise((resolve) => {
    const child = spawn(kernelPath, ['session', 'status', '-agent', agentId], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d) => {
      stdout += d.toString();
    });
    child.stderr.on('data', (d) => {
      stderr += d.toString();
    });
    child.on('error', () => resolve(null));
    child.on('close', (code) => {
      if (code !== 0) {
        // Unknown agent or kernel error — caller treats null as "no
        // unlock signal available; keep sticky block" (AFR-005 fail-closed).
        resolve(null);
        return;
      }
      try {
        const obj = JSON.parse(stdout);
        if (typeof obj.locked !== 'boolean') {
          resolve(null);
          return;
        }
        resolve({
          locked: obj.locked,
          lock_epoch: typeof obj.lock_epoch === 'number' ? obj.lock_epoch : null,
        });
      } catch {
        resolve(null);
      }
    });
  });
}

/**
 * Spec 091 v1.1 AFR-002 helper: snapshot the kernel's lock_epoch into
 * stopHookActiveEpoch at the moment stopHookActive[sessionId] is set, so
 * AFR-003 can later detect "epoch advanced past my cached value" as a
 * transition signal.
 *
 * On query failure caches null. Future AFR-003 invocations treat null
 * as "any locked:false clears" (the fallback path — without a reference
 * epoch, any positive unlock signal is a real transition).
 *
 * @param {string} sessionId
 * @param {string} agentId
 * @param {string} kernelPath
 * @param {{ warn: (msg: string) => void }} log
 */
async function captureLockEpoch(sessionId, agentId, kernelPath, log) {
  try {
    const status = await querySessionStatus(agentId, kernelPath);
    if (status !== null && typeof status.lock_epoch === 'number') {
      stopHookActiveEpoch.set(sessionId, status.lock_epoch);
      return;
    }
    stopHookActiveEpoch.set(sessionId, null);
    log.warn(
      `chitin: captureLockEpoch for ${agentId} returned no epoch — AFR-003 will use locked:false fallback`,
    );
  } catch {
    stopHookActiveEpoch.set(sessionId, null);
  }
}

/**
 * Spec 091 v1.1 AFR-004: emit a `stop_hook_cleared` chain event when the
 * plugin clears its in-memory sticky stop in response to an observed
 * kernel unlock (AFR-003). Distinct from spec 096's `session_unlocked`
 * event:
 *   - session_unlocked says "the kernel cleared the lock state"
 *   - stop_hook_cleared says "the plugin observed the kernel's clear
 *     and resumed normal evaluation"
 * Both events together let the chain reconstruct the full recovery
 * sequence.
 *
 * Fire-and-forget per D8: telemetry failure does not block the deny→
 * allow transition. Caller .catch()es.
 *
 * @param {{
 *   sessionId: string,
 *   agentId: string | undefined,
 *   kernelLockEpoch: number | null,
 *   kernelPath: string,
 *   log: { warn: (msg: string) => void, error: (msg: string) => void },
 * }} args
 */
export async function emitStopHookCleared(args) {
  // Write the event JSON to a temp file. The kernel's emit subcommand
  // takes `-event-file <path>` not `-event-json -` (verified against
  // the live binary 2026-05-23; see spec 097's emit.go for the same
  // correction in the orchestrator-side emitter).
  const event = {
    schema_version: '2',
    event_type: 'stop_hook_cleared',
    run_id: `session-${args.agentId ?? 'openclaw-plugin'}`,
    session_id: args.sessionId,
    surface: 'openclaw-plugin-governance',
    agent_instance_id: args.agentId ?? 'openclaw-plugin',
    chain_type: 'plugin-runtime',
    payload: {
      session_id: args.sessionId,
      agent: args.agentId ?? 'openclaw-plugin',
      kernel_lock_epoch: args.kernelLockEpoch,
      cleared_at: new Date().toISOString(),
    },
    ts: new Date().toISOString(),
  };
  const { writeFile, mkdtemp, rm } = await import('node:fs/promises');
  const { tmpdir } = await import('node:os');
  const { join } = await import('node:path');
  const dir = await mkdtemp(join(tmpdir(), 'chitin-stop-hook-cleared-'));
  const path = join(dir, 'event.json');
  await writeFile(path, JSON.stringify(event));
  try {
    await new Promise((resolve, reject) => {
      const child = spawn(args.kernelPath, ['emit', '-event-file', path], {
        stdio: ['ignore', 'pipe', 'pipe'],
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
    });
  } finally {
    rm(dir, { recursive: true, force: true }).catch(() => {});
  }
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
  stopHookActiveEpoch.clear();
  forcedContinuations.clear();
}
export function __test_isStopHookActive(sessionId) {
  return stopHookActive.get(sessionId) === true;
}
export function __test_getForcedContinuationCount(sessionId) {
  return forcedContinuations.get(sessionId) ?? 0;
}
// Spec 091 v1.1 test surface: read the cached lock_epoch so AFR-002 +
// AFR-003 tests can assert capture-and-compare behavior.
export function __test_getStopHookActiveEpoch(sessionId) {
  return stopHookActiveEpoch.get(sessionId);
}
export const __test_FORCED_CONTINUATION_CAP = FORCED_CONTINUATION_CAP;

export default plugin;
