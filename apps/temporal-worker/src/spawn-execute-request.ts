// Shared helper for the per-PR dispatch sites (peer-reviewer +
// comment-responder × 2) post-Temporal cut-over. Replaces
// client.workflow.start(executeRequestWorkflow, {args:[req]}) with a
// detached spawn of the chitin-execute-request CLI, with log-file-mtime
// dedup so concurrent ingester ticks don't re-spawn an in-flight agent
// (the role Temporal's USE_EXISTING workflowIdConflictPolicy played
// pre-cut-over).
//
// Same pattern as review-graph-dispatch.ts's defaultSpawnLobster +
// log-file dedup oracle, but for the generic executeRequestWorkflow
// path (no .lobster file involved — the single agent turn is the
// whole "workflow").

import { spawn as nodeSpawn } from 'node:child_process';
import {
  mkdirSync,
  openSync,
  writeFileSync,
  appendFileSync,
  readdirSync,
  statSync,
} from 'node:fs';
import { resolve } from 'node:path';
import type { ExecutionRequest } from '@chitin/contracts';

const EXECUTE_REQUEST_BIN =
  process.env.CHITIN_EXECUTE_REQUEST_BIN ??
  resolve(process.cwd(), 'apps/temporal-worker/bin/chitin-execute-request');

const REQUESTS_DIR = resolve(
  process.env.HOME ?? '/tmp',
  '.cache/chitin/execute-request-args',
);
const LOGS_DIR = resolve(
  process.env.HOME ?? '/tmp',
  '.cache/chitin/execute-request-logs',
);

/**
 * Dedup window for the log-file-mtime oracle. Default 1h matches the
 * longest realistic agent turn (cap is 31min via the workflow's
 * startToCloseTimeout, but stuck workers + retries can push close to
 * 1h). Anything older = stale, allow re-spawn.
 */
const RUNNING_WINDOW_MS = parseInt(
  process.env.CHITIN_EXECUTE_REQUEST_RUNNING_WINDOW_MS ?? '3600000', 10,
);

function logsDir(): string {
  return process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR ?? LOGS_DIR;
}

function logPathFor(workflow_id: string): string {
  return resolve(logsDir(), `${workflow_id}.log`);
}

function requestPathFor(workflow_id: string): string {
  return resolve(
    process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR ?? REQUESTS_DIR,
    `${workflow_id}.json`,
  );
}

/**
 * True iff the log file for this workflow_id exists and was touched
 * within the dedup window. Caller of spawnExecuteRequest uses this to
 * skip the spawn when an in-flight agent already exists.
 */
export function isExecuteRequestRunning(
  workflow_id: string,
  now: number = Date.now(),
): boolean {
  try {
    const st = statSync(logPathFor(workflow_id));
    return now - st.mtimeMs < RUNNING_WINDOW_MS;
  } catch {
    return false;
  }
}

/**
 * List in-flight execute-request workflow ids by scanning the log dir
 * for files with recent mtime. Replaces the pre-cut-over Temporal
 * visibility query (`workflow.list({ query: ... })`).
 */
export function listRunningExecuteRequestsFromDisk(
  now: number = Date.now(),
): Set<string> {
  const ids = new Set<string>();
  let entries: string[];
  try {
    entries = readdirSync(logsDir());
  } catch {
    return ids;
  }
  for (const name of entries) {
    if (!name.endsWith('.log')) continue;
    const fullPath = resolve(logsDir(), name);
    try {
      const st = statSync(fullPath);
      if (now - st.mtimeMs < RUNNING_WINDOW_MS) {
        ids.add(name.slice(0, -'.log'.length));
      }
    } catch {
      // file disappeared between readdir and stat — skip
    }
  }
  return ids;
}

export interface SpawnExecuteRequestInput {
  request: ExecutionRequest;
  /** Set false to bypass the dedup oracle (e.g., scripts that
   *  intentionally want to re-spawn). Default true. */
  dedup?: boolean;
  /** Injectable for tests. Default spawns chitin-execute-request. */
  spawnFn?: (args: SpawnFnInput) => Promise<void>;
}

export interface SpawnFnInput {
  /** Resolved workflow_id (same as request.workflow_id; surfaced for
   *  the spawnFn signature so injected mocks don't have to re-derive). */
  workflow_id: string;
  /** Path the spawn should pass as `--request-file <path>`. */
  requestPath: string;
  /** Path the spawn should redirect stdout+stderr to (open append). */
  logPath: string;
  /** Resolved bin path the spawn should exec. */
  bin: string;
}

export interface SpawnExecuteRequestResult {
  enqueued: boolean;
  workflow_id: string;
  /** True when enqueued=false because the dedup oracle saw a recent
   *  in-flight log. Caller logs as info, not warn. */
  skipped_already_running?: boolean;
  /** Spawn-failure detail when enqueued=false and not a dedup skip. */
  error?: string;
}

async function defaultSpawnFn(input: SpawnFnInput): Promise<void> {
  const out = openSync(input.logPath, 'a');
  const child = nodeSpawn(
    input.bin,
    ['--request-file', input.requestPath],
    { detached: true, stdio: ['ignore', out, out] },
  );
  child.on('error', (err) => {
    try {
      appendFileSync(input.logPath, `\n[spawn-error] ${err.message}\n`);
    } catch {
      // best-effort
    }
  });
  if (!child.pid) {
    throw new Error(`chitin-execute-request spawn returned no pid (binary not found at ${input.bin}?)`);
  }
  child.unref();
}

/**
 * Spawn chitin-execute-request as a detached child process to run an
 * ExecutionRequest. Returns a result envelope describing what happened
 * (enqueued / skipped / failed). Writes the request JSON to disk so
 * the child can read it via --request-file (avoids stdin pipe
 * complexity in detached mode).
 */
export async function spawnExecuteRequest(
  input: SpawnExecuteRequestInput,
): Promise<SpawnExecuteRequestResult> {
  const { request } = input;
  const dedup = input.dedup !== false;
  const spawnFn = input.spawnFn ?? defaultSpawnFn;

  if (dedup && isExecuteRequestRunning(request.workflow_id)) {
    return {
      enqueued: false,
      workflow_id: request.workflow_id,
      skipped_already_running: true,
    };
  }

  mkdirSync(logsDir(), { recursive: true });
  mkdirSync(
    process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR ?? REQUESTS_DIR,
    { recursive: true },
  );

  const requestPath = requestPathFor(request.workflow_id);
  const logPath = logPathFor(request.workflow_id);
  writeFileSync(requestPath, JSON.stringify(request, null, 2));

  try {
    await spawnFn({
      workflow_id: request.workflow_id,
      requestPath,
      logPath,
      bin: EXECUTE_REQUEST_BIN,
    });
  } catch (err) {
    return {
      enqueued: false,
      workflow_id: request.workflow_id,
      error: err instanceof Error ? err.message : String(err),
    };
  }

  return { enqueued: true, workflow_id: request.workflow_id };
}
