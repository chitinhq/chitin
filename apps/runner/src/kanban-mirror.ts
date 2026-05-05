// kanban-mirror — sync docs/swarm-backlog.md → hermes kanban.
//
// Mirrors EVERY backlog entry (not just status:ready) onto the chitin
// kanban board, mapping chitin status → kanban column so the dashboard
// reflects the full SDLC slice the dispatcher works through:
//
//   chitin status     →  kanban column
//   ─────────────       ────────────
//   in_design         →  triage      (needs design / spec before claimable)
//   ready             →  ready       (claimable; dispatcher's pool)
//   (running, set by bridge — not by mirror)
//   partial           →  done        (work shipped; followups noted in entry)
//   completed         →  archived
//   shipped           →  archived
//   decomposed        →  archived
//   blocked           →  blocked
//
// Idempotent via --idempotency-key swarm-backlog:<entry-id>:
//   - First run: creates each card in the column matching its chitin status.
//   - Subsequent runs: hermes returns the existing card; mirror then syncs
//     the column to the current chitin status (e.g., entry flipped from
//     ready → partial picks up the column flip on the next mirror tick).
//
// SAFETY: cards currently in the `running` column are NEVER touched by
// the mirror — that state belongs to the dispatcher's bridge. A
// concurrent mirror tick during dispatch leaves the running card alone.

import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { parseBacklog } from './grooming/parse-backlog.ts';

const HERMES = process.env.HERMES_BIN ?? `${process.env.HOME}/.local/bin/hermes`;
const BOARD = process.env.HERMES_KANBAN_BOARD ?? 'chitin';
const BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');

// When set, every mirrored card's --assignee is forced to this value
// instead of entry.role. Used to route the whole backlog through the
// `chitin-runner` hermes profile (which proxies to
// chitin-execute-request) rather than naming each role as its own
// profile. Unset = legacy behavior (assignee = entry.role).
const ASSIGNEE_OVERRIDE = process.env.HERMES_KANBAN_ASSIGNEE_OVERRIDE;

interface MirrorResult {
  entry_id: string;
  status: string;
  card_id?: string;
  action: 'created' | 'exists' | 'failed' | 'skipped_running';
  state_synced?: 'triage' | 'ready' | 'blocked' | 'done' | 'archived';
  error?: string;
}

// chitin status → kanban target column.
// `running` is intentionally absent — the dispatcher's bridge owns
// that state and the mirror must not collide with in-flight work.
type KanbanColumn = 'triage' | 'ready' | 'blocked' | 'done' | 'archived';

function chitinStatusToColumn(status: string): KanbanColumn {
  switch (status) {
    case 'in_design': return 'triage';
    case 'ready': return 'ready';
    case 'blocked': return 'blocked';
    case 'partial': return 'done';
    case 'completed':
    case 'shipped':
    case 'decomposed':
      return 'archived';
    default:
      // Unknown status (e.g., the literal "open | claimed | shipped"
      // in the backlog) — surface as ready and let the operator
      // re-classify in the source.
      return 'ready';
  }
}

interface KanbanCard {
  id: string;
  title: string;
  status: string;
}

function findCardByTitle(title: string): KanbanCard | null {
  // hermes kanban list --json with no status filter returns all
  // non-archived cards; archived ones need --archived flag. We need
  // both to detect "card already in archived" so we don't try to
  // re-archive it (no-op, but generates noise).
  const argSets = [
    ['list', '--json'],
    ['list', '--archived', '--json'],
  ];
  for (const args of argSets) {
    try {
      const out = hermesKanban(args);
      const tasks = JSON.parse(out) as KanbanCard[];
      const found = tasks.find((t) => t.title === title);
      if (found) return found;
    } catch {
      // ignore — try the other set
    }
  }
  return null;
}

function syncCardToColumn(card: KanbanCard, target: KanbanColumn, entryId: string): MirrorResult['action'] {
  // SAFETY: never touch a card the dispatcher's bridge has claimed.
  if (card.status === 'running') {
    return 'skipped_running';
  }
  if (card.status === target) {
    return 'exists';  // already in the right column
  }
  try {
    switch (target) {
      case 'triage':
        // No direct "move to triage" command; create with --triage.
        // For an existing card not in triage, we'd need to recreate.
        // For now, skip and surface in the report.
        return 'exists';
      case 'ready':
        // If currently blocked, unblock; otherwise nothing to do.
        if (card.status === 'blocked') {
          hermesKanban(['unblock', card.id]);
        }
        // Other transitions to ready (done → ready, archived → ready)
        // require explicit re-create or operator action. Skip silently.
        return 'exists';
      case 'blocked':
        hermesKanban(['block', card.id, `chitin status=blocked for ${entryId}`]);
        return 'exists';
      case 'done':
        hermesKanban(['complete', card.id, '--result', `chitin status=partial (work shipped; see backlog for any followups)`]);
        return 'exists';
      case 'archived':
        hermesKanban(['archive', card.id]);
        return 'exists';
    }
  } catch {
    // Hermes rejected the transition (already in target, idempotency conflict, etc.).
    // Treat as exists — the card is in some column, just maybe not the one we wanted.
    return 'exists';
  }
}

function hermesKanban(args: string[]): string {
  try {
    return execFileSync(HERMES, ['kanban', '--board', BOARD, ...args], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    }).trim();
  } catch (err) {
    // execFileSync's thrown Error.message is just "Command failed: ..." —
    // the actual stderr (where hermes argparse / runtime errors land) is
    // on err.stderr. Surface both so misclassification doesn't happen.
    const stderr = (err as { stderr?: Buffer }).stderr?.toString() ?? '';
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`${msg}\n--stderr--\n${stderr.trim()}`);
  }
}

// Chitin tier (T0..T4) → hermes priority int. T0 is smallest LOC / fastest;
// hermes treats higher priority as more urgent, so map T0→1 (rare, run
// fast) up to T4→5 (heavy, last-resort). Absent tier → priority 0.
function tierToPriority(tier: string | undefined): string {
  if (!tier) return '0';
  const n = parseInt(tier.replace(/^T/, ''), 10);
  if (Number.isNaN(n)) return '0';
  return String(n + 1);
}

function ensureBoard(): void {
  // boards create is idempotent if it errors with "already exists";
  // swallow that. Other errors bubble up.
  try {
    execFileSync(HERMES, ['kanban', 'boards', 'create', BOARD], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    console.log(`[mirror] created board "${BOARD}"`);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (/already exists|exists/i.test(msg)) {
      console.log(`[mirror] board "${BOARD}" already exists — reusing`);
    } else {
      throw err;
    }
  }
}

function ensureKanbanInit(): void {
  // hermes kanban init is documented as idempotent.
  execFileSync(HERMES, ['kanban', 'init'], { stdio: ['ignore', 'pipe', 'pipe'] });
}

function buildBody(entry: ReturnType<typeof parseBacklog>[number]): string {
  const head = '```yaml\n' + entry.rawFrontmatter.trim() + '\n```\n\n';
  // Cap description at 4000 chars to avoid bloating the kanban DB; full
  // entry stays canonical in docs/swarm-backlog.md.
  const desc = entry.description.length > 4000
    ? entry.description.slice(0, 4000) + '\n\n…(truncated; see docs/swarm-backlog.md)'
    : entry.description;
  return head + desc;
}

function mirrorEntry(entry: ReturnType<typeof parseBacklog>[number]): MirrorResult {
  const idempotencyKey = `swarm-backlog:${entry.id}`;
  const targetColumn = chitinStatusToColumn(entry.status as string);

  const args = [
    'create',
    entry.id,
    '--body', buildBody(entry),
    '--idempotency-key', idempotencyKey,
    '--created-by', 'kanban-mirror',
    '--json',
  ];
  // For in_design entries, create directly in triage so we don't have to
  // "demote" from ready afterward (no kanban cmd does that without recreate).
  if (targetColumn === 'triage') args.push('--triage');
  const assignee = ASSIGNEE_OVERRIDE ?? entry.role;
  if (assignee) args.push('--assignee', assignee);
  args.push('--priority', tierToPriority(entry.tier));

  let cardId: string | undefined;
  let createAction: 'created' | 'exists' | 'failed' = 'created';
  try {
    const out = hermesKanban(args);
    const result = JSON.parse(out);
    cardId = result.id as string;
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (/already.*exists|idempotency/i.test(msg)) {
      createAction = 'exists';
    } else {
      return { entry_id: entry.id, status: entry.status, action: 'failed', error: msg };
    }
  }

  // For both fresh creates AND existing cards, sync the column to the
  // current chitin status. hermes returns the card-id even on
  // idempotency match (when the create returns success), so for the
  // create-success path we have cardId. For the exists-via-throw path
  // we need to look up the card.
  const card = cardId
    ? { id: cardId, title: entry.id, status: createAction === 'created' ? (targetColumn === 'triage' ? 'triage' : 'ready') : 'unknown' }
    : findCardByTitle(entry.id);

  if (!card) {
    return { entry_id: entry.id, status: entry.status, action: createAction, error: 'card lookup failed post-create' };
  }

  // For freshly-created cards, the create call already placed them in
  // their initial column (ready by default, triage with --triage).
  // For triage/ready cases there's nothing more to do.
  // For blocked/done/archived, we need a follow-up command.
  let stateSync: MirrorResult['state_synced'];
  let action: MirrorResult['action'] = createAction;
  if (targetColumn !== 'ready' && targetColumn !== 'triage') {
    // The freshly-created card is in 'ready'; we need to push it to the target.
    // For existing cards, syncCardToColumn checks status before acting.
    const cardWithRealStatus = createAction === 'exists' && card.status === 'unknown'
      ? findCardByTitle(entry.id) ?? card
      : card;
    const syncResult = syncCardToColumn(cardWithRealStatus, targetColumn, entry.id);
    if (syncResult === 'skipped_running') {
      action = 'skipped_running';
    } else {
      stateSync = targetColumn;
    }
  } else if (createAction === 'exists' && targetColumn === 'ready') {
    // Existing card; verify it's in ready (or unblock if blocked).
    const realCard = card.status === 'unknown' ? findCardByTitle(entry.id) ?? card : card;
    syncCardToColumn(realCard, 'ready', entry.id);
    stateSync = 'ready';
  } else if (targetColumn === 'triage') {
    stateSync = 'triage';
  } else {
    stateSync = 'ready';
  }

  return { entry_id: entry.id, status: entry.status, card_id: card.id, action, state_synced: stateSync };
}

function main(): void {
  console.log(`[mirror] hermes bin: ${HERMES}`);
  console.log(`[mirror] board: ${BOARD}`);
  console.log(`[mirror] reading: ${BACKLOG_PATH}`);

  ensureKanbanInit();
  ensureBoard();

  const all = parseBacklog(BACKLOG_PATH);

  // Distribution by chitin status, for the operator-facing log.
  const byStatus: Record<string, number> = {};
  for (const e of all) byStatus[e.status as string] = (byStatus[e.status as string] ?? 0) + 1;
  console.log(`[mirror] backlog has ${all.length} entries:`);
  for (const [status, count] of Object.entries(byStatus).sort()) {
    console.log(`  ${status.padEnd(15)} ${count} → kanban ${chitinStatusToColumn(status)}`);
  }

  const results: MirrorResult[] = [];
  for (const entry of all) {
    const r = mirrorEntry(entry);
    results.push(r);
  }

  const counts = {
    created: results.filter((r) => r.action === 'created').length,
    exists: results.filter((r) => r.action === 'exists').length,
    skipped_running: results.filter((r) => r.action === 'skipped_running').length,
    failed: results.filter((r) => r.action === 'failed').length,
  };
  console.log(`\n[mirror] done — created=${counts.created} exists=${counts.exists} skipped_running=${counts.skipped_running} failed=${counts.failed}`);
  if (counts.failed > 0) {
    console.log('[mirror] failed entries:');
    for (const r of results.filter((x) => x.action === 'failed')) {
      console.log(`  ${r.entry_id}: ${r.error}`);
    }
    process.exit(1);
  }
}

main();
