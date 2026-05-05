// Thin wrapper around the `hermes kanban` CLI so the dispatcher can
// reflect live state on the operator-facing dashboard. The mirror
// (kanban-mirror.ts) creates cards from swarm-backlog.md ahead of
// time; this module is the read+write surface the dispatcher uses
// during a tick:
//
//   1. findReadyCardByTitle(<entry_id>) — look up the card the
//      mirror created
//   2. claimCard(<task_id>) — flip status ready → running so the
//      dashboard reflects "agent in flight"
//   3. completeCard(<task_id>, {result, metadata}) — flip status
//      running → done with the dispatch outcome
//
// All operations swallow errors and return null/false rather than
// throwing — kanban hiccups must not block dispatch. The chain log is
// the source of truth; kanban is a status surface for the operator.

import { execFileSync } from 'node:child_process';

const HERMES = process.env.HERMES_BIN ?? `${process.env.HOME ?? ''}/.local/bin/hermes`;
const BOARD = process.env.HERMES_KANBAN_BOARD ?? 'chitin';

export interface KanbanCard {
  id: string;
  title: string;
  status: string;
  assignee: string | null;
}

function hermes(args: string[], opts?: { capture?: boolean }): string | null {
  try {
    const stdio: ('ignore' | 'pipe')[] = opts?.capture
      ? ['ignore', 'pipe', 'pipe']
      : ['ignore', 'pipe', 'pipe'];
    const out = execFileSync(HERMES, args, { encoding: 'utf8', stdio });
    return opts?.capture ? out : null;
  } catch {
    // Hermes binary missing, board missing, network hiccup, etc. — all
    // treated as "no kanban available; dispatch continues without it".
    return null;
  }
}

/**
 * Look up the ready card whose title matches the entry id. Returns
 * null when the card doesn't exist (mirror hasn't synced yet) or when
 * the card is in a non-ready status (someone else is already on it).
 */
export function findReadyCardByTitle(title: string): KanbanCard | null {
  const out = hermes(
    ['kanban', '--board', BOARD, 'list', '--status', 'ready', '--json'],
    { capture: true },
  );
  if (!out) return null;
  let tasks: KanbanCard[];
  try {
    tasks = JSON.parse(out) as KanbanCard[];
  } catch {
    return null;
  }
  return tasks.find((t) => t.title === title) ?? null;
}

/**
 * Atomically claim a kanban card. Returns true on success, false on
 * any error (race lost, card archived, hermes missing, etc.).
 */
export function claimCard(taskId: string, ttlSec: number = 3600): boolean {
  const out = hermes(
    ['kanban', '--board', BOARD, 'claim', taskId, '--ttl', String(ttlSec)],
    { capture: true },
  );
  return out !== null;
}

/**
 * Mark a card complete with optional result text + structured metadata
 * (workflow_id, exit_code, duration_ms, pr_url, etc.). Returns true on
 * success, false on any error.
 */
export function completeCard(
  taskId: string,
  opts: { result?: string; summary?: string; metadata?: Record<string, unknown> } = {},
): boolean {
  const args = ['kanban', '--board', BOARD, 'complete', taskId];
  if (opts.result) args.push('--result', opts.result);
  if (opts.summary) args.push('--summary', opts.summary);
  if (opts.metadata) args.push('--metadata', JSON.stringify(opts.metadata));
  const out = hermes(args, { capture: true });
  return out !== null;
}
