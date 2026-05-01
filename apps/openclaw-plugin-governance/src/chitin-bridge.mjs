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
