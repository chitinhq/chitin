import { spawn } from 'node:child_process';

export interface RunHookOptions {
  kernelBin: string;
  workspace: string;
  runId: string;
  payload: Record<string, unknown>;
  agentId?: string;
}

/**
 * Invoke the Go execution kernel, forwarding the Claude Code hook payload
 * via stdin and workspace context via env. Monitor-only: always resolves.
 * Returns the kernel's exit code (should be 0 in Phase 1).
 */
export async function runHook(opts: RunHookOptions): Promise<number> {
  return await new Promise<number>((resolve, reject) => {
    const child = spawn(opts.kernelBin, [], {
      env: {
        ...process.env,
        CHITIN_WORKSPACE: opts.workspace,
        CHITIN_RUN_ID: opts.runId,
        ...(opts.agentId ? { CHITIN_AGENT_ID: opts.agentId } : {}),
      },
      stdio: ['pipe', 'inherit', 'inherit'],
    });

    child.on('error', reject);
    child.on('close', (code) => resolve(code ?? 0));

    child.stdin.write(JSON.stringify(opts.payload));
    child.stdin.end();
  });
}
