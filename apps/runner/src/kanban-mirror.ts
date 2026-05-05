// kanban-mirror — one-shot mirror of docs/swarm-backlog.md → hermes kanban.
//
// Step 2 of the Temporal-removal migration: get the backlog visible in
// hermes kanban side-by-side with the existing dispatcher (which still
// reads docs/swarm-backlog.md directly). No cut-over yet — the kanban
// is a parallel surface so the operator can evaluate the dashboard UX
// before any spawn-site rewrites land.
//
// Idempotent: each entry's id becomes its --idempotency-key, so re-runs
// are no-ops on entries that already have a card. New `status: ready`
// entries land as new cards; flipped entries are NOT updated yet (a
// later pass can sync state — out of scope for this prototype).

import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { parseBacklog } from './grooming/parse-backlog.ts';

const HERMES = process.env.HERMES_BIN ?? `${process.env.HOME}/.local/bin/hermes`;
const BOARD = process.env.HERMES_KANBAN_BOARD ?? 'chitin';
const BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');

interface MirrorResult {
  entry_id: string;
  card_id?: string;
  action: 'created' | 'exists' | 'failed';
  error?: string;
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
  const args = [
    'create',
    entry.id,
    '--body', buildBody(entry),
    '--idempotency-key', idempotencyKey,
    '--created-by', 'kanban-mirror',
    '--json',
  ];
  if (entry.role) args.push('--assignee', entry.role);
  args.push('--priority', tierToPriority(entry.tier));

  try {
    const out = hermesKanban(args);
    const result = JSON.parse(out);
    // hermes kanban create --json returns {"id": "...", "title": "...", ...}
    return { entry_id: entry.id, card_id: result.id, action: 'created' };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    // Idempotency-key match returns success (per hermes's documented
    // behavior: "its id is returned instead of creating a duplicate").
    // Treat that as 'exists'.
    if (/already.*exists|idempotency/i.test(msg)) {
      return { entry_id: entry.id, action: 'exists' };
    }
    return { entry_id: entry.id, action: 'failed', error: msg };
  }
}

function main(): void {
  console.log(`[mirror] hermes bin: ${HERMES}`);
  console.log(`[mirror] board: ${BOARD}`);
  console.log(`[mirror] reading: ${BACKLOG_PATH}`);

  ensureKanbanInit();
  ensureBoard();

  const all = parseBacklog(BACKLOG_PATH);
  // Mirror only `ready` entries — those are the dispatcher's working set.
  // partial / in_design / blocked land via separate passes if/when needed.
  const ready = all.filter((e) => e.status === 'ready');
  console.log(`[mirror] backlog has ${all.length} entries; ${ready.length} are ready`);

  const results: MirrorResult[] = [];
  for (const entry of ready) {
    process.stdout.write(`  ${entry.id} … `);
    const r = mirrorEntry(entry);
    results.push(r);
    process.stdout.write(`${r.action}${r.card_id ? ` (${r.card_id})` : ''}${r.error ? ` — ${r.error.slice(0, 80)}` : ''}\n`);
  }

  const created = results.filter((r) => r.action === 'created').length;
  const exists  = results.filter((r) => r.action === 'exists').length;
  const failed  = results.filter((r) => r.action === 'failed').length;
  console.log(`\n[mirror] done — created=${created} exists=${exists} failed=${failed}`);
  if (failed > 0) {
    console.log('[mirror] failed entries:');
    for (const r of results.filter((x) => x.action === 'failed')) {
      console.log(`  ${r.entry_id}: ${r.error}`);
    }
    process.exit(1);
  }
}

main();
