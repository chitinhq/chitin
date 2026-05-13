import { spawn } from 'node:child_process';

/**
 * @typedef {object} GateInput
 * @property {string} agent
 * @property {string} tool
 * @property {Record<string, unknown>} params
 * @property {string} [cwd]
 *
 * @typedef {object} GateDecision
 * @property {boolean} allow
 * @property {string} [reason]
 * @property {string} [ruleId]
 * @property {Record<string, unknown>} [params]
 *
 * @typedef {object} BridgeOptions
 * @property {string} kernelPath
 * @property {number} timeoutMs
 * @property {boolean} denyOnError
 */

/**
 * Invoke `chitin-kernel gate evaluate` with flag-based input.
 * The kernel writes a JSON decision to stdout: {allowed, reason, rule_id, ...}.
 * Exit 0 = allowed; non-zero = denied (or kernel error).
 *
 * @param {GateInput} input
 * @param {BridgeOptions} opts
 * @returns {Promise<GateDecision>}
 */
export async function evaluateGate(input, opts) {
  const args = [
    'gate',
    'evaluate',
    '-agent',
    input.agent,
    '-tool',
    input.tool,
    '-args-json',
    JSON.stringify(input.params ?? {}),
    '-cwd',
    input.cwd ?? process.cwd(),
  ];

  let stdout = '';
  let stderr = '';
  let timedOut = false;

  try {
    await new Promise((resolve, reject) => {
      const child = spawn(opts.kernelPath, args, {
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      const killTimer = setTimeout(() => {
        timedOut = true;
        child.kill('SIGKILL');
      }, opts.timeoutMs);
      child.stdout.on('data', (b) => (stdout += b.toString()));
      child.stderr.on('data', (b) => (stderr += b.toString()));
      child.on('close', () => {
        clearTimeout(killTimer);
        resolve(undefined);
      });
      child.on('error', reject);
    });

    if (timedOut) {
      return failClosed(opts, `chitin-kernel timed out after ${opts.timeoutMs}ms`);
    }

    const decision = parseDecision(stdout);
    if (!decision) {
      return failClosed(opts, `chitin-kernel returned unparseable stdout: ${stdout.slice(0, 200)}`);
    }
    return decision;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes('ENOENT')) {
      return failClosed(opts, `chitin-kernel binary not found at "${opts.kernelPath}"`);
    }
    return failClosed(opts, `chitin-kernel invocation failed: ${msg}${stderr ? ` | stderr: ${stderr.slice(0, 200)}` : ''}`);
  }
}

/**
 * @param {string} stdout
 * @returns {GateDecision | null}
 */
function parseDecision(stdout) {
  const trimmed = stdout.trim();
  if (!trimmed) return null;
  try {
    const j = JSON.parse(trimmed);
    if (typeof j.allowed !== 'boolean') return null;
    return {
      allow: j.allowed,
      reason: typeof j.reason === 'string' ? j.reason : undefined,
      ruleId: typeof j.rule_id === 'string' ? j.rule_id : undefined,
      params: typeof j.params === 'object' && j.params !== null ? j.params : undefined,
    };
  } catch {
    return null;
  }
}

/**
 * @param {BridgeOptions} opts
 * @param {string} reason
 * @returns {GateDecision}
 */
function failClosed(opts, reason) {
  if (opts.denyOnError) {
    return { allow: false, reason: `chitin bridge: ${reason}`, ruleId: 'bridge_error' };
  }
  return { allow: true };
}

/**
 * Invoke `chitin-kernel router evaluate --hook-stdin` (the router-pipeline
 * path: kernel verdict → heuristics → advisor → composed response).
 *
 * Why a second function alongside `evaluateGate`:
 *
 * - `evaluateGate` calls `chitin-kernel gate evaluate` with FLAGS, gets a
 *   {allowed, reason, rule_id} JSON back. Deterministic-only — no
 *   heuristics, no advisor.
 * - `evaluateRouter` calls `chitin-kernel router evaluate` with a Claude
 *   Code-style HookInput on STDIN, gets the Claude Code hook protocol
 *   back: exit 0 + empty stdout = allow; exit non-0 + first JSON line
 *   {"decision":"block","reason":"..."} = deny.
 *
 * Rationale (2026-05-05): the openclaw plugin originally called
 * `evaluateGate`, which meant T0/T1/T3 agents (every openclaw-driven
 * tier) bypassed heuristics + advisor. T4 (claude-code-headless) was the
 * only tier that hit the router pipeline — exactly inverted from where
 * the advisor is most valuable: Claude itself is the agent at T4, so a
 * smaller advisor checking a smarter agent has marginal worth; cheap
 * local glm-flash at T0 benefits MUCH more from a Claude-class second
 * opinion. This function flips the wiring: openclaw → router → advisor.
 *
 * @param {GateInput & { sessionId?: string }} input
 * @param {BridgeOptions} opts
 * @returns {Promise<GateDecision>}
 */
export async function evaluateRouter(input, opts) {
  return evaluateHookInvocation(input, opts, ['router', 'evaluate', '--hook-stdin', '--agent', input.agent], 'router');
}

/**
 * Invoke `chitin-kernel gate evaluate --hook-stdin` for openclaw exec-shaped
 * calls. The kernel has no dedicated openclaw driver selector here, so exec
 * tools are adapted to the Claude-compatible Bash hook shape while keeping
 * the openclaw agent id in --agent for chain attribution.
 *
 * @param {GateInput & { sessionId?: string }} input
 * @param {BridgeOptions} opts
 * @returns {Promise<GateDecision>}
 */
export async function evaluateHookGate(input, opts) {
  return evaluateHookInvocation(input, opts, ['gate', 'evaluate', '--hook-stdin', '--agent', input.agent], 'gate');
}

/**
 * @param {GateInput & { sessionId?: string }} input
 * @param {BridgeOptions} opts
 * @param {string[]} args
 * @param {string} label
 * @returns {Promise<GateDecision>}
 */
async function evaluateHookInvocation(input, opts, args, label) {
  const hookInput = {
    hook_event_name: 'PreToolUse',
    tool_name: hookToolName(input),
    tool_input: hookToolInput(input),
    cwd: input.cwd ?? process.cwd(),
    session_id: input.sessionId ?? `openclaw-${input.agent}`,
  };

  let stdout = '';
  let stderr = '';
  let timedOut = false;
  let exitCode = -1;

  try {
    await new Promise((resolve, reject) => {
      const child = spawn(opts.kernelPath, args, {
        stdio: ['pipe', 'pipe', 'pipe'],
      });
      const killTimer = setTimeout(() => {
        timedOut = true;
        child.kill('SIGKILL');
      }, opts.timeoutMs);
      child.stdout.on('data', (b) => (stdout += b.toString()));
      child.stderr.on('data', (b) => (stderr += b.toString()));
      child.on('close', (code) => {
        clearTimeout(killTimer);
        exitCode = code ?? -1;
        resolve(undefined);
      });
      child.on('error', reject);
      child.stdin.write(JSON.stringify(hookInput));
      child.stdin.end();
    });

    if (timedOut) {
      return failClosed(opts, `chitin-kernel ${label} timed out after ${opts.timeoutMs}ms`);
    }

    return parseRouterDecision(exitCode, stdout, opts);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes('ENOENT')) {
      return failClosed(opts, `chitin-kernel binary not found at "${opts.kernelPath}"`);
    }
    return failClosed(
      opts,
      `chitin-kernel ${label} invocation failed: ${msg}${stderr ? ` | stderr: ${stderr.slice(0, 200)}` : ''}`,
    );
  }
}

/**
 * @param {GateInput} input
 * @returns {string}
 */
function hookToolName(input) {
  return isExecShapedTool(input.tool) ? 'Bash' : input.tool;
}

/**
 * @param {GateInput} input
 * @returns {Record<string, unknown>}
 */
function hookToolInput(input) {
  if (!isExecShapedTool(input.tool)) return input.params ?? {};
  const params = input.params ?? {};
  const command = commandFromParams(params);
  if (command === undefined) return params;
  return { command };
}

/**
 * @param {string} tool
 * @returns {boolean}
 */
export function isExecShapedTool(tool) {
  return /^(?:exec|process|terminal|bash|shell|shell\.exec)$/i.test(String(tool ?? '').trim());
}

/**
 * @param {Record<string, unknown>} params
 * @returns {string | undefined}
 */
function commandFromParams(params) {
  for (const key of ['command', 'cmd', 'input']) {
    const value = params[key];
    if (typeof value === 'string') return value;
  }
  return undefined;
}

/**
 * Parse the Claude Code hook protocol output from `router evaluate
 * --hook-stdin`. Contract:
 *   exit 0, empty/non-block stdout            → allow
 *   exit non-0, first JSON line {decision,..} → deny
 *
 * @param {number} exitCode
 * @param {string} stdout
 * @param {BridgeOptions} opts
 * @returns {GateDecision}
 */
function parseRouterDecision(exitCode, stdout, opts) {
  if (exitCode === 0) {
    return { allow: true };
  }
  const firstLine = stdout.split('\n').find((l) => l.trim().startsWith('{')) ?? '';
  if (!firstLine) {
    return failClosed(opts, `chitin-kernel router exited ${exitCode} with no parseable verdict`);
  }
  try {
    const j = JSON.parse(firstLine);
    if (j.decision === 'block') {
      return {
        allow: false,
        reason: typeof j.reason === 'string' ? j.reason : 'denied by chitin router',
        ruleId: 'router_block',
      };
    }
    return failClosed(
      opts,
      `chitin-kernel router returned non-block deny verdict: ${JSON.stringify(j).slice(0, 200)}`,
    );
  } catch {
    return failClosed(opts, `chitin-kernel router returned unparseable verdict: ${firstLine.slice(0, 200)}`);
  }
}
