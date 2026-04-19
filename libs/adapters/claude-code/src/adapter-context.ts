/**
 * Builds an AdapterContext for the claude-code adapter.
 *
 * Inlined from apps/cli/src/ctx.ts to avoid a cross-boundary import from
 * a lib into an app. The logic must stay in sync manually if ctx.ts changes.
 *
 * resolveChitinDir is also inlined here to avoid an @chitin/contracts
 * dependency that the adapter package does not declare.
 */
import { createHash, randomUUID } from 'node:crypto';
import { existsSync, statSync, mkdirSync } from 'node:fs';
import { join, dirname, resolve as resolvePath } from 'node:path';
import { hostname, homedir, userInfo } from 'node:os';

/**
 * Walk up from cwd looking for an existing .chitin/ dir. Falls back to
 * $HOME/.chitin/ (created on demand).
 */
export function resolveChitinDir(cwd: string, workspaceBoundary: string): string {
  const absCwd = resolvePath(cwd);
  const absBoundary = workspaceBoundary ? resolvePath(workspaceBoundary) : '';

  let dir = absCwd;
  while (true) {
    const candidate = join(dir, '.chitin');
    if (existsSync(candidate) && statSync(candidate).isDirectory()) {
      return candidate;
    }
    if (absBoundary && dir === absBoundary) break;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  const orphan = join(homedir(), '.chitin');
  if (!existsSync(orphan)) {
    mkdirSync(orphan, { recursive: true });
  }
  return orphan;
}

export interface AdapterContextInput {
  surface: string;
  chitinDir: string;
  user?: string;
  machineID?: string;
  labelsCli?: Record<string, string>;
  labelsProject?: Record<string, string>;
}

export interface AdapterContextBuilt {
  runID: string;
  sessionID: string;
  agentInstanceID: string;
  surface: string;
  driverIdentity: { user: string; machine_id: string; machine_fingerprint: string };
  agentFingerprint: string;
  labels: Record<string, string>;
  chitinDir: string;
  kernelBinary: string;
}

const KERNEL_BIN_ENV = 'CHITIN_KERNEL_BINARY';

export function buildAdapterContext(input: AdapterContextInput): AdapterContextBuilt {
  const user = input.user ?? userInfo().username;
  const machineID = input.machineID ?? hostname();
  const machineFingerprint = sha256Hex(
    `${hostname()}|${userInfo().uid}|chitin-machine-fingerprint-v2`,
  );
  const agentFingerprint = sha256Hex(
    JSON.stringify({
      surface: input.surface,
      machine: machineID,
      version: 'phase-1.5',
    }),
  );
  return {
    runID: randomUUID(),
    sessionID: randomUUID(),
    agentInstanceID: randomUUID(),
    surface: input.surface,
    driverIdentity: { user, machine_id: machineID, machine_fingerprint: machineFingerprint },
    agentFingerprint,
    labels: { ...(input.labelsProject ?? {}), ...(input.labelsCli ?? {}) },
    chitinDir: input.chitinDir,
    kernelBinary: process.env[KERNEL_BIN_ENV] ?? 'chitin-kernel',
  };
}

function sha256Hex(s: string): string {
  return createHash('sha256').update(s, 'utf8').digest('hex');
}
