import { spawn } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
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
    case 'claude-code':
      throw new Error(
        `driver=claude-code is not a valid worker driver (Anthropic ToS — see ` +
          `memory/project_anthropic_tos_constraints.md). Use copilot or local-* for orchestrated agent work.`,
      );
    default: {
      const exhaustive: never = driver;
      throw new Error(`unknown driver: ${exhaustive as string}`);
    }
  }
}

export async function runAgentTurn(req: ExecutionRequest): Promise<ActivityResult> {
  const taskRoot = mkdtempSync(join(tmpdir(), `chitin-worker-${req.workflow_id}-`));
  const policySrc = process.env.CHITIN_POLICY_FILE ?? '/home/red/workspace/chitin-temporal-worker/chitin.yaml';
  if (existsSync(policySrc)) {
    copyFileSync(policySrc, join(taskRoot, 'chitin.yaml'));
  }

  const plan = planInvocation(req);

  const start = Date.now();
  const result = await new Promise<ActivityResult>((resolve, reject) => {
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
      resolve({
        exit_code: code ?? -1,
        stdout_tail: stdout.slice(-2000),
        stderr_tail: stderr.slice(-2000),
        duration_ms: Date.now() - start,
      });
    });
    child.on('error', reject);
  });

  return result;
}
