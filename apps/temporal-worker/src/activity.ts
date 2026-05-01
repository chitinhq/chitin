import { spawn } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { ExecutionRequest, DriverId } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

// Bytes of stdout/stderr returned to Temporal in ActivityResult. Buffers
// during the run are bounded at 2x this value (see runAgentTurn), so
// chatty drivers can't OOM the 24/7 worker.
const TAIL_BYTES = 2000;

interface DriverInvocation {
  command: string;
  args: string[];
  env?: Record<string, string>;
}

// Per-driver openclaw agent mapping (slice 3). Each local-* driver routes to
// a distinct openclaw agent so reasoning and mechanical models can be
// configured independently — the spec calls for qwen3-coder for mechanical
// (local-qwen), glm-5.1:cloud for reasoning (local-glm), deepseek for code
// (local-deepseek). Override per driver via env var, e.g.
// CHITIN_AGENT_LOCAL_QWEN=my-agent. Falls back to `main` if neither env var
// nor the default mapping resolves the driver — `main` always exists.
const DRIVER_AGENT_MAP: Record<string, string> = {
  'local-qwen': 'qwen-agent',
  'local-glm': 'glm-agent',
  'local-deepseek': 'deepseek-agent',
};

function resolveAgent(driver: DriverId): string {
  const envVar = `CHITIN_AGENT_${driver.toUpperCase().replace(/-/g, '_')}`;
  const override = process.env[envVar];
  if (override && override.trim()) return override.trim();
  return DRIVER_AGENT_MAP[driver] ?? 'main';
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
      // Dispatch through openclaw + chitin-governance plugin. The plugin
      // is loaded at openclaw startup (~/.openclaw/openclaw.json plugins.allow
      // includes "chitin-governance"); every tool call the local agent
      // dispatches passes through before_tool_call → chitin gate. The per-
      // driver agent mapping (slice 3) routes to distinct openclaw agents so
      // each local tier runs its own model.
      return {
        command: 'openclaw',
        args: [
          'agent',
          '--local',
          '--agent', resolveAgent(driver),
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

      // Bounded ring buffers — only the tail is reported, so growing strings
      // unboundedly would just burn memory in a 24/7 worker that hits chatty
      // drivers. Cap at 2x the reported tail to absorb boundary chunks.
      const tail = (cur: string, chunk: string, cap: number) =>
        (cur + chunk).slice(-cap);
      const TAIL_CAP = TAIL_BYTES * 2;
      let stdout = '';
      let stderr = '';
      child.stdout.on('data', (b) => (stdout = tail(stdout, b.toString(), TAIL_CAP)));
      child.stderr.on('data', (b) => (stderr = tail(stderr, b.toString(), TAIL_CAP)));

      const killTimer = setTimeout(() => child.kill('SIGKILL'), req.bounds.wall_timeout_s * 1000);
      child.on('close', (code) => {
        clearTimeout(killTimer);
        resolvePromise({
          exit_code: code ?? -1,
          stdout_tail: stdout.slice(-TAIL_BYTES),
          stderr_tail: stderr.slice(-TAIL_BYTES),
          duration_ms: Date.now() - start,
        });
      });
      child.on('error', reject);
    });
  } finally {
    rmSync(taskRoot, { recursive: true, force: true });
  }
}

export const __test__ = { planInvocation, resolvePolicySrc, resolveAgent, DRIVER_AGENT_MAP };
