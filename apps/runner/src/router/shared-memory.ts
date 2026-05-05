// Per-workflow shared-memory scratchpad — JSON file at
// `~/.chitin/shared-memory/<workflow-id>.json`. The simplest possible
// shape that lets the router write nudges into the workflow's
// context for the agent's next turn to read.
//
// MVP shape — operator-grade but not production:
//   - Single file per workflow_id (no concurrent writers)
//   - Append-only via timestamped entries
//   - Agent reads via `chitin shared-memory get <workflow-id>` (CLI)
//   - Router writes via this module's writeNudge()
//
// Future (deferred): cross-workflow vector retrieval (Hindsight
// pattern), schema-validated entries, concurrent-safe locking.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { homedir } from 'node:os';

const SHARED_MEMORY_ROOT = join(homedir(), '.chitin', 'shared-memory');

export interface SharedMemoryEntry {
  /** ISO-8601 timestamp of when this entry was written. */
  ts: string;
  /** Source of the entry — usually 'router-nudge' or 'router-takeover-decision' or 'agent-self-report'. */
  source: string;
  /** Free-form payload — typically `{ nudge: string, verdict: string, advisor_run_id: string }`. */
  payload: Record<string, unknown>;
}

export interface SharedMemoryFile {
  workflow_id: string;
  entries: SharedMemoryEntry[];
}

function pathFor(workflowId: string): string {
  // Sanitize workflow_id to prevent path traversal.
  if (workflowId.includes('/') || workflowId.includes('..')) {
    throw new Error(`shared-memory: invalid workflow_id ${JSON.stringify(workflowId)}`);
  }
  return join(SHARED_MEMORY_ROOT, `${workflowId}.json`);
}

/**
 * Read the current shared-memory file for a workflow. Returns an
 * empty file shape if none exists.
 */
export function readSharedMemory(workflowId: string): SharedMemoryFile {
  const path = pathFor(workflowId);
  if (!existsSync(path)) {
    return { workflow_id: workflowId, entries: [] };
  }
  return JSON.parse(readFileSync(path, 'utf8')) as SharedMemoryFile;
}

/**
 * Append an entry to the workflow's shared memory. Creates the
 * file (and parent dir) if needed.
 */
export function appendSharedMemory(
  workflowId: string,
  entry: Omit<SharedMemoryEntry, 'ts'>,
): void {
  const path = pathFor(workflowId);
  mkdirSync(dirname(path), { recursive: true });
  const file = readSharedMemory(workflowId);
  file.entries.push({ ts: new Date().toISOString(), ...entry });
  writeFileSync(path, JSON.stringify(file, null, 2));
}

/**
 * Convenience: write a router-nudge entry with the standard payload
 * shape. Called by the dispatcher after an advisor call returns.
 */
export function writeNudge(
  workflowId: string,
  payload: { nudge: string; verdict: 'continue' | 'takeover'; advisor_run_id?: string; reason?: string },
): void {
  appendSharedMemory(workflowId, { source: 'router-nudge', payload });
}

/**
 * Render shared memory as a markdown block for inclusion in the
 * next dispatch's prompt. Returns empty string if no entries.
 */
export function renderForPrompt(workflowId: string): string {
  const file = readSharedMemory(workflowId);
  if (file.entries.length === 0) return '';
  let out = '\n\n---\n\nROUTER SHARED MEMORY (prior nudges + decisions for this entry):\n\n';
  for (const entry of file.entries) {
    out += `[${entry.ts}] ${entry.source}\n`;
    out += '```json\n' + JSON.stringify(entry.payload, null, 2) + '\n```\n\n';
  }
  out += '---\n';
  return out;
}
