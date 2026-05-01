import { spawn } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { ExecutionRequest, DriverId } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

interface DriverInvocation {
  command: string;
  args: string[];
  env?: Record<string, string>;
}

function planInvocation(req: ExecutionRequest): DriverInvocation {
  const driver: DriverId = req.allowed_drivers[0];
  switch (driver) {
    case 'copilot':
      return {
        command: 'chitin-kernel',
        args: ['drive', 'copilot', req.prompt],
      };
    case 'local-qwen':
    case 'local-glm':
    case 'local-deepseek':
      // Slice 2: dispatch through openclaw + chitin-governance plugin.
      // The plugin is loaded at openclaw startup (~/.openclaw/openclaw.json
      // plugins.allow includes "chitin-governance"); every tool call the
      // local agent dispatches passes through before_tool_call → chitin gate.
      // Per-driver model selection is owned by the openclaw agent config
      // (`agents.defaults.model.primary` or per-agent override). Slice 3
      // adds a `--agent <id>` mapping per driver tier (local-qwen →
      // qwen-agent, etc.) — for slice 2 the default `main` agent ships.
      return {
        command: 'openclaw',
        args: [
          'agent',
          '--local',
          '--agent', 'main',
          '--json',
          '--timeout', String(req.bounds.wall_timeout_s),
          '--message', req.prompt,
        ],
      };
    default: {
      const exhaustive: never = driver;
      throw new Error(`unknown driver: ${exhaustive as string}`);
    }
  }
}

// Policy file lookup order:
//   1. CHITIN_POLICY_FILE env var (absolute path) — explicit override.
//   2. <cwd>/chitin.yaml — repo-relative default. The worker is meant to be
//      launched from the repo root; this matches developer/CI ergonomics.
// If neither resolves to an existing file, the worker proceeds without
// seeding a policy file. The kernel's gate evaluate path will then fall
// back to its own default-deny semantics.
function resolvePolicySrc(): string {
  const explicit = process.env.CHITIN_POLICY_FILE;
  if (explicit) return resolve(explicit);
  return resolve(process.cwd(), 'chitin.yaml');
}

export async function runAgentTurn(req: ExecutionRequest): Promise<ActivityResult> {
  const taskRoot = mkdtempSync(join(tmpdir(), `chitin-worker-${req.workflow_id}-`));
  const policySrc = resolvePolicySrc();
  if (existsSync(policySrc)) {
    copyFileSync(policySrc, join(taskRoot, 'chitin.yaml'));
  }

  const plan = planInvocation(req);

  const start = Date.now();
  try {
    return await new Promise<ActivityResult>((resolvePromise, reject) => {
      const child = spawn(plan.command, plan.args, {
        cwd: taskRoot,
        env: {
          ...process.env,
          ...(plan.env ?? {}),
          CHITIN_WORKFLOW_ID: req.workflow_id,
          CHITIN_RUN_ID: req.run_id,
        },
        stdio: ['ignore', 'pipe', 'pipe'],
      });

      let stdout = '';
      let stderr = '';
      child.stdout.on('data', (b) => (stdout += b.toString()));
      child.stderr.on('data', (b) => (stderr += b.toString()));

      const killTimer = setTimeout(() => child.kill('SIGKILL'), req.bounds.wall_timeout_s * 1000);
      child.on('close', (code) => {
        clearTimeout(killTimer);
        resolvePromise({
          exit_code: code ?? -1,
          stdout_tail: stdout.slice(-2000),
          stderr_tail: stderr.slice(-2000),
          duration_ms: Date.now() - start,
        });
      });
      child.on('error', reject);
    });
  } finally {
    rmSync(taskRoot, { recursive: true, force: true });
  }
}

export const __test__ = { planInvocation, resolvePolicySrc };
