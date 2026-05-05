// kanban-pr-mirror — sync chitinhq/chitin open PRs → hermes kanban as
// child cards of their corresponding backlog-entry cards. Together with
// kanban-mirror.ts (backlog → kanban), this gives the operator a single
// dashboard view of the full SDLC slice: idea (triage) → ready →
// running → PR-open (child cards) → merged (parent flips to done).
//
// Card title format: `pr-<N>: <pr-title>` so the operator can scan
// quickly and grep across cards.
//
// Parent linking: the swarm PR title format is `swarm: <entry-id>`,
// so we strip the prefix to recover the parent entry id and look up
// its card. Non-swarm PRs (operator-authored, comment-responder
// follow-ups, etc.) get no parent — they show up as orphan cards.
//
// Status mapping:
//   PR draft                               → triage
//   PR open + any check FAILURE            → blocked
//   PR open + checks ok + mergeable=yes    → ready  (landable per /land contract)
//   PR open + checks ok + mergeable=other  → ready  (probably mergeable; gate is /land)
//   PR merged                              → done   (then auto-archive on next pass)
//   PR closed without merge                → archived
//
// Idempotency: --idempotency-key chitin-pr:<N>:<head_sha> so a fresh
// commit on the PR (head_sha changes) creates a new card iteration
// rather than silently silencing a stale dedup.

import { execFileSync } from 'node:child_process';

const HERMES = process.env.HERMES_BIN ?? `${process.env.HOME}/.local/bin/hermes`;
const BOARD = process.env.HERMES_KANBAN_BOARD ?? 'chitin';
const REPO = process.env.CHITIN_PR_MIRROR_REPO ?? 'chitinhq/chitin';

interface PrInfo {
  number: number;
  title: string;
  url: string;
  state: 'OPEN' | 'CLOSED' | 'MERGED';
  isDraft: boolean;
  mergeable: 'MERGEABLE' | 'CONFLICTING' | 'UNKNOWN';
  headRefOid: string;
  statusCheckRollup: Array<{ conclusion?: string }>;
}

type KanbanColumn = 'triage' | 'ready' | 'blocked' | 'done' | 'archived';

interface MirrorResult {
  pr: number;
  card_id?: string;
  parent_id?: string;
  target_column: KanbanColumn;
  action: 'created' | 'exists' | 'failed';
  error?: string;
}

function gh(args: string[]): string {
  return execFileSync('gh', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
}

function hermesKanban(args: string[]): string {
  try {
    return execFileSync(HERMES, ['kanban', '--board', BOARD, ...args], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    }).trim();
  } catch (err) {
    const stderr = (err as { stderr?: Buffer }).stderr?.toString() ?? '';
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`${msg}\n--stderr--\n${stderr.trim()}`);
  }
}

function fetchOpenPrs(): PrInfo[] {
  const out = gh([
    'pr', 'list', '-R', REPO, '--state', 'open',
    '--json', 'number,title,url,state,isDraft,mergeable,headRefOid,statusCheckRollup',
    '-L', '100',
  ]);
  return JSON.parse(out) as PrInfo[];
}

function classifyPr(pr: PrInfo): KanbanColumn {
  if (pr.state === 'MERGED') return 'done';
  if (pr.state === 'CLOSED') return 'archived';
  if (pr.isDraft) return 'triage';
  const failed = (pr.statusCheckRollup ?? []).some((c) => c.conclusion === 'FAILURE');
  if (failed) return 'blocked';
  return 'ready';
}

function entryIdFromSwarmPrTitle(title: string): string | null {
  // Swarm dispatcher PR titles: `swarm: <entry-id>` (programmer path)
  // and `auto: <description>` (auto-flipper). Only the swarm: prefix
  // gives us the entry id back.
  const m = title.match(/^swarm:\s*(.+)$/);
  return m ? m[1].trim() : null;
}

function findBacklogCardByTitle(title: string): { id: string } | null {
  for (const args of [['list', '--json'], ['list', '--archived', '--json']]) {
    try {
      const tasks = JSON.parse(hermesKanban(args)) as Array<{ id: string; title: string }>;
      const found = tasks.find((t) => t.title === title);
      if (found) return found;
    } catch {
      // ignore
    }
  }
  return null;
}

function buildBody(pr: PrInfo, parentEntryId: string | null): string {
  const lines = [
    `**PR:** ${pr.url}`,
    `**Head:** \`${pr.headRefOid.slice(0, 7)}\``,
    `**Mergeable:** ${pr.mergeable}`,
    `**Draft:** ${pr.isDraft ? 'yes' : 'no'}`,
  ];
  if (parentEntryId) {
    lines.push(`**Parent backlog entry:** \`${parentEntryId}\``);
  }
  const checks = pr.statusCheckRollup ?? [];
  if (checks.length) {
    const ok = checks.filter((c) => c.conclusion === 'SUCCESS').length;
    const fail = checks.filter((c) => c.conclusion === 'FAILURE').length;
    lines.push(`**Checks:** ${ok} ok, ${fail} fail`);
  }
  return lines.join('\n');
}

function syncCardColumn(cardId: string, current: string, target: KanbanColumn, pr: PrInfo): void {
  if (current === 'running') return;  // dispatcher owns this
  if (current === target) return;
  try {
    switch (target) {
      case 'blocked':
        hermesKanban(['block', cardId, `PR has failing checks (head ${pr.headRefOid.slice(0, 7)})`]);
        break;
      case 'ready':
        if (current === 'blocked') hermesKanban(['unblock', cardId]);
        break;
      case 'done':
        hermesKanban(['complete', cardId, '--result', 'merged']);
        break;
      case 'archived':
        hermesKanban(['archive', cardId]);
        break;
      // triage from non-triage requires recreate; skip silently.
    }
  } catch {
    // hermes rejected; surface as a no-op
  }
}

function mirrorPr(pr: PrInfo): MirrorResult {
  const entryId = entryIdFromSwarmPrTitle(pr.title);
  const parentCard = entryId ? findBacklogCardByTitle(entryId) : null;
  const target = classifyPr(pr);
  const idempotencyKey = `chitin-pr:${pr.number}:${pr.headRefOid}`;
  const cardTitle = `pr-${pr.number}: ${pr.title}`;

  const args = [
    'create',
    cardTitle,
    '--body', buildBody(pr, entryId),
    '--idempotency-key', idempotencyKey,
    '--created-by', 'kanban-pr-mirror',
    '--json',
  ];
  if (target === 'triage') args.push('--triage');
  if (parentCard) args.push('--parent', parentCard.id);

  let cardId: string;
  let createAction: 'created' | 'exists' = 'created';
  try {
    const result = JSON.parse(hermesKanban(args)) as { id: string; status: string };
    cardId = result.id;
    // Idempotency match returns existing card; we can't tell from here whether
    // it was a fresh create or a return-existing. Treat both as 'created' for
    // reporting; the column-sync below handles the divergence.
    if (result.status === target || (target === 'ready' && result.status === 'ready')) {
      // No sync needed.
    } else {
      syncCardColumn(cardId, result.status, target, pr);
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { pr: pr.number, target_column: target, action: 'failed', error: msg };
  }

  return {
    pr: pr.number,
    card_id: cardId,
    parent_id: parentCard?.id,
    target_column: target,
    action: createAction,
  };
}

function main(): void {
  console.log(`[pr-mirror] hermes bin: ${HERMES}`);
  console.log(`[pr-mirror] board: ${BOARD}`);
  console.log(`[pr-mirror] repo: ${REPO}`);

  const prs = fetchOpenPrs();
  console.log(`[pr-mirror] found ${prs.length} open PR(s)`);

  const results: MirrorResult[] = [];
  for (const pr of prs) {
    const r = mirrorPr(pr);
    results.push(r);
    process.stdout.write(`  pr-${pr.number} → ${r.target_column.padEnd(8)}`);
    if (r.parent_id) process.stdout.write(` (child of ${r.parent_id})`);
    if (r.error) process.stdout.write(` — ${r.error.slice(0, 80)}`);
    process.stdout.write('\n');
  }

  const counts = {
    created: results.filter((r) => r.action === 'created').length,
    failed: results.filter((r) => r.action === 'failed').length,
  };
  console.log(`\n[pr-mirror] done — created/synced=${counts.created} failed=${counts.failed}`);
  if (counts.failed > 0) process.exit(1);
}

main();
