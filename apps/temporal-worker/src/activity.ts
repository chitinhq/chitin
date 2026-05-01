import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest, DriverId } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

const MODEL_FOR_DRIVER: Record<DriverId, string> = {
  'claude-code': process.env.CHITIN_WORKER_MODEL ?? 'qwen3-coder:30b',
  'copilot': 'qwen3-coder:30b',
  'local-qwen': 'qwen3-coder:30b',
  'local-glm': 'glm-5.1:cloud',
  'local-deepseek': 'qwen3-coder:30b',
};

const GATE_HOOK_SETTINGS = {
  hooks: {
    PreToolUse: [
      {
        _tag: 'chitin-governance-worker',
        matcher: 'Bash|Edit|Write|NotebookEdit|Read|WebFetch|WebSearch|Task|Glob|Grep|LS|TodoWrite',
        hooks: [
          {
            type: 'command',
            command: 'chitin-kernel gate evaluate --hook-stdin --agent=claude-code',
          },
        ],
      },
    ],
  },
};

export async function runAgentTurn(req: ExecutionRequest): Promise<ActivityResult> {
  const driver = req.allowed_drivers[0];
  const model = MODEL_FOR_DRIVER[driver];

  const taskRoot = mkdtempSync(join(tmpdir(), `chitin-worker-${req.workflow_id}-`));
  mkdirSync(join(taskRoot, '.claude'), { recursive: true });
  writeFileSync(join(taskRoot, '.claude', 'settings.json'), JSON.stringify(GATE_HOOK_SETTINGS, null, 2));

  const args = [
    '--print',
    '--dangerously-skip-permissions',
    '--model', model,
    req.prompt,
  ];

  const start = Date.now();
  const result = await new Promise<ActivityResult>((resolve, reject) => {
    const child = spawn('claude', args, {
      cwd: taskRoot,
      env: {
        ...process.env,
        ANTHROPIC_BASE_URL: 'http://127.0.0.1:11434',
        ANTHROPIC_API_KEY: 'ollama',
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
