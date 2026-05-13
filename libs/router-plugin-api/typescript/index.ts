// chitin-router-plugin-api (TypeScript) — opt-in side-effect gate
// for plugin authors.
//
// When a chitin router plugin needs to perform an action with
// side effects (file write, shell exec, network call), it should
// route that action through this library FIRST. The library
// shells to `chitin-kernel gate evaluate --hook-stdin` and
// returns the deterministic verdict.
//
// Usage:
//
//   import { gateAction, GateBlocked } from '@chitin/router-plugin-api';
//
//   try {
//     await gateAction({
//       toolName: 'Write',
//       toolInput: { file_path: '/tmp/output.json' },
//       agent: 'my-plugin',
//       sessionId: 'router-plugin-foo',
//     });
//   } catch (e) {
//     if (e instanceof GateBlocked) {
//       console.error(`Action blocked by chitin: ${e.reason}`);
//       return;
//     }
//     throw e;
//   }
//
//   // If we get here, the kernel allowed the action
//   await fs.writeFile('/tmp/output.json', ...);
//
// Design notes (mirrors python/chitin_governance.py):
//   - This is OPT-IN. Plugins that don't import this aren't gated.
//     Operator's responsibility to install only trusted plugins
//     (see plugins_trust in chitin.yaml).
//   - Subprocess overhead (~10ms per gate call). Plugin authors
//     should batch related actions when possible.
//   - Returns the kernel's exact decision; allows the plugin to
//     react (retry, escalate, fall back).

import { spawn } from 'node:child_process';

export class GateBlocked extends Error {
  reason: string;
  ruleId?: string;
  constructor(reason: string, ruleId?: string) {
    super(reason);
    this.name = 'GateBlocked';
    this.reason = reason;
    this.ruleId = ruleId;
  }
}

export interface GateDecision {
  allowed: boolean;
  reason?: string;
  ruleId?: string;
  raw?: Record<string, unknown>;
}

export interface GateActionInput {
  toolName: string;
  toolInput: Record<string, unknown>;
  agent?: string;
  sessionId?: string;
  cwd?: string;
  raiseOnDeny?: boolean;
  kernelBinary?: string;
  timeoutMs?: number;
  /**
   * When true, pass --require-policy to the kernel so that running
   * outside any chitin.yaml-resolved scope is treated as deny
   * (strict mode). Default false preserves the kernel's documented
   * fail-open-on-no-policy behavior, matching the rest of the
   * chitin hook surface.
   */
  requirePolicy?: boolean;
}

/**
 * Run a hypothetical action through the chitin kernel gate.
 * Returns a GateDecision; throws GateBlocked when raiseOnDeny=true
 * (default) and the kernel denies. Falls open (allow) if the
 * kernel binary is missing.
 */
export async function gateAction(input: GateActionInput): Promise<GateDecision> {
  const {
    toolName,
    toolInput,
    agent = 'plugin',
    sessionId,
    cwd,
    raiseOnDeny = true,
    kernelBinary = 'chitin-kernel',
    timeoutMs = 5000,
    requirePolicy = false,
  } = input;

  const payload: Record<string, unknown> = {
    hook_event_name: 'PreToolUse',
    tool_name: toolName,
    tool_input: toolInput,
  };
  if (sessionId) payload.session_id = sessionId;
  if (cwd) payload.cwd = cwd;

  return new Promise<GateDecision>((resolve, reject) => {
    let resolved = false;
    const finish = (val: GateDecision): void => {
      if (resolved) return;
      resolved = true;
      if (!val.allowed && raiseOnDeny) {
        reject(new GateBlocked(val.reason ?? 'kernel-denied', val.ruleId));
      } else {
        resolve(val);
      }
    };

    const args = ['gate', 'evaluate', '--hook-stdin', `--agent=${agent}`];
    if (requirePolicy) args.push('--require-policy');
    const child = spawn(kernelBinary, args, { stdio: ['pipe', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d: Buffer) => (stdout += d.toString('utf8')));
    child.stderr.on('data', (d: Buffer) => (stderr += d.toString('utf8')));

    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      finish({ allowed: true, reason: 'kernel-timeout-fail-open' });
    }, timeoutMs);

    child.on('error', (err: Error) => {
      clearTimeout(timer);
      // Binary missing — fail open with stderr warning
      console.error(
        JSON.stringify({
          level: 'warn',
          component: 'router-plugin-api',
          msg: 'kernel-binary-missing-failing-open',
          binary: kernelBinary,
          err: err.message,
        }),
      );
      finish({ allowed: true, reason: 'kernel-missing-fail-open' });
    });

    child.on('close', (code: number | null) => {
      clearTimeout(timer);
      if (code === 0 && !stdout.trim()) {
        finish({ allowed: true });
        return;
      }
      try {
        const parsed = JSON.parse(stdout) as Record<string, unknown>;
        let allowed = true;
        let reason: string | undefined;
        let ruleId: string | undefined;
        const dec = parsed.decision;
        if (typeof dec === 'string') {
          allowed = dec === 'allow' || dec === 'continue';
          reason = (parsed.reason as string) ?? (parsed.message as string);
        } else if (dec && typeof dec === 'object') {
          const dObj = dec as Record<string, unknown>;
          allowed = dObj.Allowed === true;
          reason = dObj.Reason as string;
          ruleId = dObj.RuleID as string;
        } else if (parsed.decision === 'block') {
          allowed = false;
          reason = (parsed.reason as string) ?? (parsed.message as string);
        }
        finish({ allowed, reason, ruleId, raw: parsed });
      } catch {
        finish({ allowed: true, reason: 'kernel-non-json-fail-open' });
      }
    });

    child.stdin.on('error', () => {
      // EPIPE on a dead pipe is expected when spawn fails with ENOENT;
      // child.on('error') already records the spawn failure and fails open.
    });
    child.stdin.write(JSON.stringify(payload));
    child.stdin.end();
  });
}

// ─── Convenience wrappers ─────────────────────────────────────────

export function gateFileWrite(filePath: string, opts: Partial<GateActionInput> = {}) {
  return gateAction({ ...opts, toolName: 'Write', toolInput: { file_path: filePath } });
}

export function gateShellExec(command: string, opts: Partial<GateActionInput> = {}) {
  return gateAction({ ...opts, toolName: 'Bash', toolInput: { command } });
}

export function gateHttpRequest(url: string, opts: Partial<GateActionInput> = {}) {
  return gateAction({ ...opts, toolName: 'WebFetch', toolInput: { url } });
}
