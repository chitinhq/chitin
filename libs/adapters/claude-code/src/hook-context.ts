import { createHash, randomUUID } from 'node:crypto';
import { hostname, userInfo } from 'node:os';
import type { AdapterContext } from './hook-runner';

const KERNEL_BIN_ENV = 'CHITIN_KERNEL_BINARY';

/**
 * Build an AdapterContext for a single Claude Code hook invocation.
 *
 * The stdin-hook adapter runs as a fresh process per hook event; the
 * `session_id` provided by Claude Code is the stable identifier that links
 * all events in one session. Minting a fresh UUID here would orphan every
 * event into its own chain — that was the PR #19 strike, captured at
 * `souls/strikes/davinci.md`.
 *
 * `runID` and `agentInstanceID` are per-hook because they describe the
 * process-level run, not the session. Chain linkage goes through
 * `sessionID` and event `chain_id`, so these can vary per invocation
 * without breaking chain integrity.
 */
export function buildHookContext(
  sessionID: string,
  chitinDir: string,
): AdapterContext {
  if (!sessionID) {
    throw new Error('buildHookContext: sessionID required');
  }
  const user = userInfo().username;
  const machineID = hostname();
  const machineFingerprint = sha256Hex(
    `${hostname()}|${userInfo().uid}|chitin-machine-fingerprint-v2`,
  );
  const agentFingerprint = sha256Hex(
    JSON.stringify({
      surface: 'claude-code',
      machine: machineID,
      version: 'phase-1.5',
    }),
  );
  return {
    runID: randomUUID(),
    sessionID,
    agentInstanceID: randomUUID(),
    surface: 'claude-code',
    driverIdentity: {
      user,
      machine_id: machineID,
      machine_fingerprint: machineFingerprint,
    },
    agentFingerprint,
    labels: {},
    chitinDir,
    kernelBinary: process.env[KERNEL_BIN_ENV] ?? 'chitin-kernel',
  };
}

function sha256Hex(s: string): string {
  return createHash('sha256').update(s, 'utf8').digest('hex');
}
