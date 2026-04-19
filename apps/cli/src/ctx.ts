import { createHash, randomUUID } from 'node:crypto';
import { hostname, userInfo } from 'node:os';

export interface AdapterContextInput {
  surface: string;
  chitinDir: string;
  user?: string;
  machineID?: string;
  labelsCli?: Record<string, string>;
  labelsProject?: Record<string, string>;
}

export interface AdapterContext {
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

export function buildAdapterContext(input: AdapterContextInput): AdapterContext {
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
