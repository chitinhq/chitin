#!/usr/bin/env node
// Chitin Console — local read-only API server.
//
// Aggregates data from the operator's local state stores for the
// Angular console frontend. Everything here is read-only and localhost
// bound by default.
//
// Sources:
//   ~/.chitin/kanban/<board>/kanban.db          — tickets, runs, events (preferred)
//   ~/.hermes/kanban/boards/<board>/kanban.db   — legacy fallback during cutover
//   ~/.openclaw/data/clawta.db                  — swarm ELO + dispatch scores
//   ~/.openclaw/data/clawta_decisions.db        — per-ticket dispatch decisions
//   ~/.argus/index.db                           — argus observatory index
//   ~/.chitin/gov-decisions-<date>.jsonl        — chain ledger / replay source
//   <repo>/chitin.yaml                          — policy (read-only)

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { execFileSync, spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import Database from 'better-sqlite3';

const PORT = Number(process.env.CHITIN_CONSOLE_PORT || 7878);
const HOST = process.env.CHITIN_CONSOLE_HOST || '127.0.0.1';
const HOME = os.homedir();

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const REPO_ROOT = process.env.CHITIN_REPO_ROOT
  || path.resolve(__dirname, '..', '..', '..');

// Prefer the chitin-owned kanban DB (Phase 1: chitin-kernel kanban
// migrate <board> populates ~/.chitin/kanban/<board>/kanban.db).
// Fall back to the legacy hermes layout when the chitin copy is
// absent so the console still works on operator boxes that haven't
// run the migration yet.
const CHITIN_KANBAN_ROOT = process.env.CHITIN_KANBAN_ROOT || path.join(HOME, '.chitin', 'kanban');
const HERMES_KANBAN_ROOT = process.env.HERMES_KANBAN_ROOT || path.join(HOME, '.hermes', 'kanban');
// KANBAN_ROOT kept as an alias for the legacy hermes root so the
// existing `current` board sentinel resolves the same way.
const KANBAN_ROOT = HERMES_KANBAN_ROOT;
const CURRENT_BOARD = (() => {
  try { return fs.readFileSync(path.join(KANBAN_ROOT, 'current'), 'utf8').trim(); }
  catch { return 'chitin'; }
})();
// Chitin layout: <root>/<board>/kanban.db (no `boards/` segment).
// Hermes layout: <root>/boards/<board>/kanban.db.
const CHITIN_BOARD_DB = path.join(CHITIN_KANBAN_ROOT, CURRENT_BOARD, 'kanban.db');
const HERMES_BOARD_DB = path.join(HERMES_KANBAN_ROOT, 'boards', CURRENT_BOARD, 'kanban.db');
const BOARD_DB = fs.existsSync(CHITIN_BOARD_DB) ? CHITIN_BOARD_DB : HERMES_BOARD_DB;
const BOARD_DB_SOURCE = BOARD_DB === CHITIN_BOARD_DB ? 'chitin' : 'hermes';
const BUS_DB = process.env.AGENT_BUS_DB || path.join(HOME, '.chitin', 'agent-bus', 'bus.db');
const ARGUS_DB = process.env.ARGUS_INDEX_DB || path.join(HOME, '.argus', 'index.db');
const CLAWTA_DECISIONS_DB = process.env.CLAWTA_DECISIONS_DB
  || path.join(HOME, '.openclaw', 'data', 'clawta_decisions.db');
const CLAWTA_DB = process.env.CLAWTA_DB || path.join(HOME, '.openclaw', 'data', 'clawta.db');
const CHAIN_LEDGER_DIR = path.join(HOME, '.chitin');
const ANALYZER_DB = process.env.CHITIN_ANALYZER_DB || path.join(CHAIN_LEDGER_DIR, 'analyzer.db');
const POLICY_FILE = process.env.CHITIN_POLICY_FILE || path.join(REPO_ROOT, 'chitin.yaml');

// Optional static bundle. When CHITIN_CONSOLE_STATIC_ROOT points at a
// built chitin-console (e.g. dist/apps/chitin-console/browser), the
// server doubles as the SPA host so a single port covers both the
// frontend and /api. Used for Tailscale exposure (one port to share)
// and for any production-style deployment. Leave unset for the dev
// loop where `nx serve` runs the frontend on its own port.
const STATIC_ROOT = process.env.CHITIN_CONSOLE_STATIC_ROOT
  || (fs.existsSync(path.join(REPO_ROOT, 'dist/apps/chitin-console/browser'))
        ? path.join(REPO_ROOT, 'dist/apps/chitin-console/browser')
        : null);
const STATIC_MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js':   'application/javascript; charset=utf-8',
  '.mjs':  'application/javascript; charset=utf-8',
  '.css':  'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg':  'image/svg+xml',
  '.png':  'image/png',
  '.jpg':  'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.ico':  'image/x-icon',
  '.woff':  'font/woff',
  '.woff2': 'font/woff2',
  '.map':  'application/json; charset=utf-8',
};

let kanbanDB = null;
let busDB = null;
let argusDB = null;
let clawtaDecisionsDB = null;
let clawtaDB = null;
let analyzerDB = null;

const ANALYZER_SCHEMA = `
CREATE TABLE IF NOT EXISTS analyzer_suggestions (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  target TEXT NOT NULL,
  diff TEXT NOT NULL,
  rationale TEXT NOT NULL,
  applied INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_analyzer_suggestions_created_at
  ON analyzer_suggestions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_analyzer_suggestions_type_target
  ON analyzer_suggestions(type, target);
`;

function openIfExists(p, readonly = true) {
  try {
    if (!fs.existsSync(p)) return null;
    return new Database(p, { readonly, fileMustExist: true });
  } catch (e) {
    console.warn(`[console-api] could not open ${p}: ${e.message}`);
    return null;
  }
}

function openAnalyzerDB() {
  try {
    fs.mkdirSync(path.dirname(ANALYZER_DB), { recursive: true });
    const db = new Database(ANALYZER_DB);
    db.exec(ANALYZER_SCHEMA);
    return db;
  } catch (e) {
    console.warn(`[console-api] could not open ${ANALYZER_DB}: ${e.message}`);
    return null;
  }
}

function reopen() {
  kanbanDB?.close(); busDB?.close(); argusDB?.close(); clawtaDecisionsDB?.close(); clawtaDB?.close(); analyzerDB?.close();
  kanbanDB           = openIfExists(BOARD_DB);
  busDB              = openIfExists(BUS_DB);
  argusDB            = openIfExists(ARGUS_DB);
  clawtaDecisionsDB  = openIfExists(CLAWTA_DECISIONS_DB);
  clawtaDB           = openIfExists(CLAWTA_DB);
  analyzerDB         = openAnalyzerDB();
}
reopen();

function json(res, status, body) {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
    'Cache-Control': 'no-store',
    'Access-Control-Allow-Origin': '*',
  });
  res.end(payload);
}
const notFound  = (res) => json(res, 404, { error: 'not_found' });
const serverErr = (res, e) => json(res, 500, { error: 'server_error', detail: String(e?.message || e) });
const ATTACHMENT_CACHE_TTL_MS = 60_000;
const attachmentCache = new Map();

// ---------- Helpers ----------
function* readJsonl(file) {
  const raw = fs.readFileSync(file, 'utf8');
  for (const line of raw.split('\n')) {
    if (!line.trim()) continue;
    try { yield JSON.parse(line); } catch { continue; }
  }
}

function listLedgerFiles({ maxDays = 14 } = {}) {
  try {
    const files = fs.readdirSync(CHAIN_LEDGER_DIR)
      .filter(f => /^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$/.test(f))
      .map(f => path.join(CHAIN_LEDGER_DIR, f))
      .map(p => ({ path: p, mtime: fs.statSync(p).mtimeMs }))
      .sort((a, b) => b.mtime - a.mtime)
      .slice(0, maxDays);
    return files.map(f => f.path);
  } catch { return []; }
}

function slugify(value) {
  return String(value || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function previewText(value, max = 140) {
  const text = String(value || '').replace(/\s+/g, ' ').trim();
  if (!text) return '';
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
}

function safeRepoPath(relPath) {
  if (!relPath) return null;
  const resolved = path.resolve(REPO_ROOT, relPath);
  const relative = path.relative(REPO_ROOT, resolved);
  if (relative.startsWith('..') || path.isAbsolute(relative)) return null;
  return resolved;
}

function parseMarkdownPreview(content) {
  const lines = String(content || '').split(/\r?\n/);
  let title = '';
  let i = 0;
  while (i < lines.length && !title) {
    const line = lines[i].trim();
    if (line.startsWith('# ')) title = line.replace(/^# /, '').trim();
    i += 1;
  }
  const bodyLines = [];
  let inFence = false;
  for (; i < lines.length; i += 1) {
    const raw = lines[i];
    const line = raw.trim();
    if (line.startsWith('```')) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    if (!line) {
      if (bodyLines.length) break;
      continue;
    }
    bodyLines.push(line);
  }
  return {
    title: title || '(untitled spec)',
    preview: bodyLines.join(' '),
  };
}

function currentRepoInfo() {
  try {
    const remote = execFileSync('git', ['config', '--get', 'remote.origin.url'], {
      cwd: REPO_ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    const match = remote.match(/github\.com[:/]([^/]+)\/(.+?)(?:\.git)?$/);
    if (!match) return null;
    return { owner: match[1], repo: match[2], remote };
  } catch {
    return null;
  }
}

const repoInfo = currentRepoInfo();

function ghApi(route, extraArgs = []) {
  const args = ['api', route, ...extraArgs];
  const raw = execFileSync('gh', args, {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  return JSON.parse(raw);
}

function withAttachmentCache(key, loader) {
  const now = Date.now();
  const hit = attachmentCache.get(key);
  if (hit && now - hit.at < ATTACHMENT_CACHE_TTL_MS) return hit.value;
  const value = loader();
  attachmentCache.set(key, { at: now, value });
  return value;
}

// ---------- Endpoints ----------
function getStats() {
  if (!kanbanDB) return { board: CURRENT_BOARD, lanes: {} };
  const rows = kanbanDB.prepare("SELECT status, COUNT(*) as n FROM tasks GROUP BY status").all();
  const lanes = Object.fromEntries(rows.map(r => [r.status, r.n]));
  const last7 = kanbanDB.prepare(
    "SELECT COUNT(*) as n FROM tasks WHERE completed_at IS NOT NULL AND completed_at > strftime('%s','now') - 7*86400"
  ).get().n;
  const inFlight = kanbanDB.prepare("SELECT COUNT(*) as n FROM tasks WHERE status='in_progress'").get().n;
  const ages = kanbanDB.prepare(
    "SELECT (strftime('%s','now') - created_at) AS age FROM tasks WHERE status IN ('ready','todo','triage') ORDER BY age"
  ).all().map(r => r.age);
  const median = ages.length ? ages[Math.floor(ages.length / 2)] : 0;
  let successRate = null, runsLast24 = 0, runsCompleted24 = 0;
  try {
    const sr = kanbanDB.prepare(`
      SELECT
        SUM(CASE WHEN outcome='completed' THEN 1 ELSE 0 END) AS ok,
        COUNT(*) AS total
      FROM task_runs
      WHERE ended_at IS NOT NULL AND ended_at > strftime('%s','now') - 7*86400
    `).get();
    if (sr.total) successRate = sr.ok / sr.total;
    const r24 = kanbanDB.prepare(`
      SELECT
        SUM(CASE WHEN outcome='completed' THEN 1 ELSE 0 END) AS ok,
        COUNT(*) AS total
      FROM task_runs WHERE ended_at IS NOT NULL AND ended_at > strftime('%s','now') - 86400
    `).get();
    runsLast24 = r24.total; runsCompleted24 = r24.ok;
  } catch {
    // Stats endpoints stay available even when task_runs is absent.
  }
  return {
    board: CURRENT_BOARD,
    lanes,
    completedLast7Days: last7,
    inFlight,
    medianAgeSecondsActive: median,
    successRate7d: successRate,
    runsLast24,
    runsCompleted24,
    generatedAt: Date.now(),
  };
}

function listTasks(query) {
  if (!kanbanDB) return { board: CURRENT_BOARD, tasks: [] };
  const status   = query.get('status');
  const assignee = query.get('assignee');
  const search   = query.get('q');
  const limit    = Math.min(Number(query.get('limit') || 500), 2000);

  const conds = [];
  const params = {};
  if (status) {
    const list = status.split(',').map(s => s.trim()).filter(Boolean);
    conds.push(`status IN (${list.map((_, i) => `@s${i}`).join(',')})`);
    list.forEach((s, i) => { params[`s${i}`] = s; });
  }
  if (assignee) { conds.push('assignee = @assignee'); params.assignee = assignee; }
  if (search) {
    conds.push('(LOWER(title) LIKE @q OR LOWER(body) LIKE @q OR id LIKE @qid)');
    params.q = `%${search.toLowerCase()}%`;
    params.qid = `%${search}%`;
  }
  const where = conds.length ? `WHERE ${conds.join(' AND ')}` : '';

  const sql = `
    SELECT id, title, status, assignee, priority, created_at, started_at, completed_at,
           workspace_kind, workspace_path, tenant, current_run_id,
           workflow_template_id, current_step_key, consecutive_failures, max_retries,
           last_heartbeat_at, idempotency_key,
           CASE WHEN body IS NULL THEN 0 ELSE 1 END AS has_body
    FROM tasks
    ${where}
    ORDER BY
      CASE status
        WHEN 'in_progress' THEN 0
        WHEN 'triage' THEN 1
        WHEN 'ready' THEN 2
        WHEN 'todo' THEN 3
        WHEN 'done' THEN 4
        ELSE 5
      END,
      priority DESC, created_at DESC
    LIMIT @limit
  `;
  params.limit = limit;
  const rows = kanbanDB.prepare(sql).all(params);
  return { board: CURRENT_BOARD, count: rows.length, tasks: rows };
}

function getTask(id) {
  if (!kanbanDB) return null;
  const task = kanbanDB.prepare('SELECT * FROM tasks WHERE id = ?').get(id);
  if (!task) return null;
  const events = kanbanDB.prepare(
    'SELECT id, run_id, kind, payload, created_at FROM task_events WHERE task_id = ? ORDER BY id DESC LIMIT 300'
  ).all(id);
  const runs = kanbanDB.prepare(
    'SELECT id, profile, step_key, status, started_at, ended_at, outcome, summary, error FROM task_runs WHERE task_id = ? ORDER BY started_at DESC'
  ).all(id);
  let comments = [];
  try { comments = kanbanDB.prepare('SELECT id, author, body, created_at FROM task_comments WHERE task_id = ? ORDER BY id ASC').all(id); } catch { comments = []; }
  let links = [];
  try { links = kanbanDB.prepare('SELECT id, rel, ref, created_at FROM task_links WHERE task_id = ? ORDER BY id ASC').all(id); } catch { links = []; }
  let clawtaDecisions = [];
  if (clawtaDecisionsDB) {
    try {
      clawtaDecisions = clawtaDecisionsDB.prepare(
        'SELECT id, driver, model, selection_mode, reasoning, ts FROM clawta_decisions WHERE ticket_id = ? ORDER BY ts DESC LIMIT 25'
      ).all(id);
    } catch {
      clawtaDecisions = [];
    }
  }
  return { task, runs, events, comments, links, clawtaDecisions };
}

function listThreads(query) {
  if (!busDB) return { threads: [], count: 0 };
  const board = query.get('board');
  const status = query.get('status');
  const audience = query.get('audience');
  const search = (query.get('q') || '').trim().toLowerCase();
  const limit = Math.min(Number(query.get('limit') || 200), 1000);

  const conds = [];
  const params = {};
  if (board) { conds.push('t.board = @board'); params.board = board; }
  if (status) { conds.push('t.status = @status'); params.status = status; }
  if (audience) {
    conds.push("(t.audience IS NULL OR t.audience = '' OR ',' || t.audience || ',' LIKE @audience)");
    params.audience = `%,${audience},%`;
  }
  if (search) {
    conds.push('(LOWER(t.title) LIKE @q OR LOWER(COALESCE(first_msg.body, \'\')) LIKE @q OR t.task_id LIKE @qid)');
    params.q = `%${search}%`;
    params.qid = `%${query.get('q')}%`;
  }
  const where = conds.length ? `WHERE ${conds.join(' AND ')}` : '';
  const rows = busDB.prepare(`
    SELECT
      t.id,
      t.board,
      t.task_id,
      t.title,
      t.author,
      t.audience,
      t.status,
      t.discord_thread_id,
      t.created_at,
      t.updated_at,
      COALESCE(msg_counts.message_count, 0) AS message_count,
      COALESCE(att_counts.attachment_count, 0) AS attachment_count,
      latest_msg.author AS last_message_author,
      latest_msg.body AS last_message_body
    FROM threads t
    LEFT JOIN (
      SELECT thread_id, COUNT(*) AS message_count
      FROM messages
      GROUP BY thread_id
    ) AS msg_counts ON msg_counts.thread_id = t.id
    LEFT JOIN (
      SELECT thread_id, COUNT(*) AS attachment_count
      FROM attachments
      GROUP BY thread_id
    ) AS att_counts ON att_counts.thread_id = t.id
    LEFT JOIN messages AS latest_msg
      ON latest_msg.id = (
        SELECT id FROM messages WHERE thread_id = t.id ORDER BY created_at DESC, id DESC LIMIT 1
      )
    LEFT JOIN messages AS first_msg
      ON first_msg.id = (
        SELECT id FROM messages WHERE thread_id = t.id ORDER BY created_at ASC, id ASC LIMIT 1
      )
    ${where}
    ORDER BY t.updated_at DESC, t.id DESC
    LIMIT @limit
  `).all({ ...params, limit });
  return {
    threads: rows.map((row) => ({
      ...row,
      last_message_preview: previewText(row.last_message_body),
    })),
    count: rows.length,
  };
}

function getThread(id) {
  if (!busDB) return null;
  const thread = busDB.prepare(`
    SELECT
      t.id,
      t.board,
      t.task_id,
      t.title,
      t.author,
      t.audience,
      t.status,
      t.discord_thread_id,
      t.created_at,
      t.updated_at
    FROM threads t
    WHERE t.id = ?
  `).get(id);
  if (!thread) return null;
  const messages = busDB.prepare(`
    SELECT id, thread_id, parent_id, author, audience, body, kind,
           discord_message_id, ack_required, created_at
    FROM messages
    WHERE thread_id = ?
    ORDER BY created_at ASC, id ASC
  `).all(id);
  const attachments = busDB.prepare(`
    SELECT id, thread_id, kind, ref, display, created_at
    FROM attachments
    WHERE thread_id = ?
    ORDER BY created_at ASC, id ASC
  `).all(id);
  return {
    thread: {
      ...thread,
      message_count: messages.length,
      attachment_count: attachments.length,
      last_message_preview: previewText(messages.at(-1)?.body),
      last_message_author: messages.at(-1)?.author || null,
    },
    messages,
    attachments,
  };
}

function listAssignees() {
  if (!kanbanDB) return { assignees: [] };
  const rows = kanbanDB.prepare(`
    SELECT assignee, COUNT(*) AS n
    FROM tasks
    WHERE assignee IS NOT NULL AND assignee != ''
    GROUP BY assignee
    ORDER BY n DESC
  `).all();
  return { assignees: rows };
}

function getTaskAttachment(ref) {
  if (!kanbanDB) {
    return {
      kind: 'task',
      ref,
      status: 'missing',
      label: ref,
      title: ref,
      subtitle: '(missing)',
    };
  }
  const task = kanbanDB.prepare('SELECT id, title, status, assignee FROM tasks WHERE id = ?').get(ref);
  if (!task) {
    return {
      kind: 'task',
      ref,
      status: 'missing',
      label: ref,
      title: ref,
      subtitle: '(missing)',
      href: `/tickets?id=${encodeURIComponent(ref)}`,
    };
  }
  return {
    kind: 'task',
    ref,
    status: 'ok',
    label: task.id,
    title: task.title,
    subtitle: task.assignee ? `${task.status} · ${task.assignee}` : task.status,
    taskStatus: task.status,
    assignee: task.assignee,
    href: `/tickets?id=${encodeURIComponent(task.id)}`,
  };
}

function getSpecAttachment(ref) {
  const absPath = safeRepoPath(ref);
  if (!absPath || !fs.existsSync(absPath)) {
    return {
      kind: 'spec',
      ref,
      status: 'missing',
      label: 'spec',
      title: path.basename(ref),
      subtitle: '(missing)',
    };
  }
  const content = fs.readFileSync(absPath, 'utf8');
  const preview = parseMarkdownPreview(content);
  return {
    kind: 'spec',
    ref,
    status: 'ok',
    label: 'spec',
    title: preview.title,
    subtitle: previewText(preview.preview, 180),
    preview: preview.preview,
    body: content,
    href: `/api/repo-file?path=${encodeURIComponent(ref)}`,
  };
}

function normalizePrRef(ref) {
  const raw = String(ref || '').trim();
  const simple = raw.match(/^#?(\d+)$/);
  if (simple && repoInfo) return { ...repoInfo, number: Number(simple[1]), crossRepo: false };
  const qualified = raw.match(/^([^/]+)\/([^#]+)#(\d+)$/);
  if (qualified) {
    return {
      owner: qualified[1],
      repo: qualified[2],
      number: Number(qualified[3]),
      crossRepo: !repoInfo || qualified[1] !== repoInfo.owner || qualified[2] !== repoInfo.repo,
    };
  }
  return null;
}

function ciBucketFromState(state) {
  switch (state) {
    case 'success': return 'passing';
    case 'failure':
    case 'error': return 'failing';
    case 'pending': return 'pending';
    default: return 'unknown';
  }
}

function getPrAttachment(ref) {
  const parsed = normalizePrRef(ref);
  if (!parsed) {
    return {
      kind: 'pr',
      ref,
      status: 'missing',
      label: 'PR',
      title: ref,
      subtitle: '(missing)',
    };
  }
  if (parsed.crossRepo) {
    return {
      kind: 'pr',
      ref,
      status: 'missing',
      label: `PR #${parsed.number}`,
      title: `${parsed.owner}/${parsed.repo}#${parsed.number}`,
      subtitle: '(missing)',
    };
  }
  return withAttachmentCache(`pr:${parsed.owner}/${parsed.repo}#${parsed.number}`, () => {
    try {
      const pr = ghApi(`repos/${parsed.owner}/${parsed.repo}/pulls/${parsed.number}`);
      const combined = ghApi(`repos/${parsed.owner}/${parsed.repo}/commits/${pr.head.sha}/status`);
      const merged = pr.merged_at ? 'merged' : pr.state;
      const ciBucket = ciBucketFromState(combined.state);
      return {
        kind: 'pr',
        ref,
        status: 'ok',
        label: `PR #${pr.number}`,
        title: pr.title,
        subtitle: `${merged} · CI ${ciBucket}`,
        prNumber: pr.number,
        prState: merged,
        checks: ciBucket,
        href: pr.html_url,
      };
    } catch (e) {
      return {
        kind: 'pr',
        ref,
        status: 'error',
        label: `PR #${parsed.number}`,
        title: `${parsed.owner}/${parsed.repo}#${parsed.number}`,
        subtitle: '(missing)',
        error: e.message,
      };
    }
  });
}

function discordHref(ref) {
  if (/^https?:\/\//.test(ref)) return ref;
  const cleaned = String(ref || '').trim().replace(/^discord:\/\//, '').replace(/^\/+/, '');
  return `discord://-/channels/${cleaned}`;
}

function getDiscordAttachment(ref, display) {
  const label = display || `#${slugify(ref).replace(/-/g, '-') || 'discord'}`;
  return {
    kind: 'discord',
    ref,
    status: ref ? 'ok' : 'missing',
    label: 'discord',
    title: label,
    subtitle: ref ? previewText(ref, 80) : '(missing)',
    href: ref ? discordHref(ref) : undefined,
  };
}

function getUrlAttachment(ref, display) {
  return {
    kind: 'url',
    ref,
    status: ref ? 'ok' : 'missing',
    label: display || 'url',
    title: display || ref,
    subtitle: ref ? previewText(ref, 90) : '(missing)',
    href: ref || undefined,
  };
}

function getFileAttachment(ref) {
  const absPath = safeRepoPath(ref);
  if (!absPath || !fs.existsSync(absPath)) {
    return {
      kind: 'file',
      ref,
      status: 'missing',
      label: 'file',
      title: ref,
      subtitle: '(missing)',
    };
  }
  let firstLine = '';
  try {
    firstLine = fs.readFileSync(absPath, 'utf8').split(/\r?\n/, 1)[0] || '';
  } catch {
    firstLine = '';
  }
  return {
    kind: 'file',
    ref,
    status: 'ok',
    label: 'file',
    title: ref,
    subtitle: previewText(firstLine || 'open file', 120),
    href: `/api/repo-file?path=${encodeURIComponent(ref)}`,
  };
}

function getAttachmentEnrichment(threadId, attachmentId) {
  if (!busDB) return null;
  const attachment = busDB.prepare(`
    SELECT id, thread_id, kind, ref, display, created_at
    FROM attachments
    WHERE thread_id = ? AND id = ?
  `).get(threadId, attachmentId);
  if (!attachment) return null;
  switch (attachment.kind) {
    case 'spec':
      return getSpecAttachment(attachment.ref);
    case 'pr':
      return getPrAttachment(attachment.ref);
    case 'task':
      return getTaskAttachment(attachment.ref);
    case 'discord':
      return getDiscordAttachment(attachment.ref, attachment.display);
    case 'url':
      return getUrlAttachment(attachment.ref, attachment.display);
    case 'file':
      return getFileAttachment(attachment.ref);
    default:
      return {
        kind: attachment.kind,
        ref: attachment.ref,
        status: 'missing',
        label: attachment.kind,
        title: attachment.display || attachment.ref,
        subtitle: '(missing)',
      };
  }
}

function getRepoFile(query) {
  const relPath = query.get('path');
  const absPath = safeRepoPath(relPath);
  if (!absPath || !fs.existsSync(absPath)) return null;
  return {
    path: relPath,
    content: fs.readFileSync(absPath, 'utf8'),
  };
}

function listRunsRecent(query) {
  if (!kanbanDB) return { runs: [] };
  const limit = Math.min(Number(query.get('limit') || 50), 500);
  const rows = kanbanDB.prepare(`
    SELECT r.id, r.task_id, t.title AS task_title, t.assignee AS task_assignee,
           r.profile, r.step_key, r.status,
           r.started_at, r.ended_at, r.outcome, r.summary, r.error
    FROM task_runs r
    LEFT JOIN tasks t ON t.id = r.task_id
    ORDER BY r.started_at DESC
    LIMIT ?
  `).all(limit);
  return { runs: rows };
}

function listArgusFindings(query) {
  if (!argusDB) return { findings: [], note: 'argus index unavailable' };
  const limit = Math.min(Number(query.get('limit') || 100), 500);
  const candidates = [
    `SELECT * FROM findings ORDER BY ts DESC LIMIT ?`,
    `SELECT * FROM findings ORDER BY created_at DESC LIMIT ?`,
    `SELECT * FROM findings ORDER BY id DESC LIMIT ?`,
  ];
  for (const sql of candidates) {
    try { return { findings: argusDB.prepare(sql).all(limit) }; }
    catch { continue; }
  }
  try {
    const tables = argusDB.prepare("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").all().map(r => r.name);
    return { findings: [], tables };
  } catch (e) { return { findings: [], error: e.message }; }
}

function argusInfo() {
  if (!argusDB) return { available: false };
  try {
    const tables = argusDB.prepare("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").all().map(r => r.name);
    const counts = {};
    for (const t of tables) {
      try { counts[t] = argusDB.prepare(`SELECT COUNT(*) as n FROM "${t}"`).get().n; } catch { counts[t] = 0; }
    }
    return { available: true, tables, counts };
  } catch (e) { return { available: true, error: e.message }; }
}

function listEloLeaderboard() {
  if (!clawtaDB) return { rows: [] };
  try {
    const rows = clawtaDB.prepare(`
      SELECT id, driver, model, role, task_class, complexity_bucket,
             elo_score, dispatches_count, last_dispatch_id,
             first_scored_at, last_updated
      FROM swarm_elo
      ORDER BY elo_score DESC
      LIMIT 200
    `).all();
    return { rows };
  } catch (e) { return { rows: [], error: e.message }; }
}

function chainKey(evt) {
  return evt.chain_id || evt.session_id || evt.envelope_id || null;
}

function listSessions(query) {
  const limit = Math.min(Number(query.get('limit') || 50), 500);
  const sessions = new Map();
  for (const file of listLedgerFiles({ maxDays: 14 })) {
    for (const evt of readJsonl(file)) {
      const sid = chainKey(evt);
      if (!sid) continue;
      let s = sessions.get(sid);
      if (!s) {
        s = {
          chain_id: sid,
          driver: evt.driver,
          agent: evt.agent,
          role: evt.role,
          firstTs: evt.ts,
          lastTs: evt.ts,
          events: 0,
          allowed: 0,
          denied: 0,
          heuristic: 0,
          costUsd: 0,
          inputBytes: 0,
          tools: new Set(),
          tickets: new Set(),
          actions: new Map(),
        };
        sessions.set(sid, s);
      }
      s.events += 1;
      if (!s.driver && evt.driver) s.driver = evt.driver;
      if (!s.agent  && evt.agent)  s.agent = evt.agent;
      if (!s.role   && evt.role)   s.role  = evt.role;
      const a = (evt.allowed === true) || evt.decision === 'allow';
      const d = (evt.allowed === false) || evt.decision === 'deny';
      const h = evt.decision === 'heuristic-allow' || evt.effect === 'heuristic';
      if (a) s.allowed += 1;
      if (d) s.denied += 1;
      if (h) s.heuristic += 1;
      if (typeof evt.cost_usd === 'number') s.costUsd += evt.cost_usd;
      if (typeof evt.input_bytes === 'number') s.inputBytes += evt.input_bytes;
      if (evt.tool_name)    s.tools.add(evt.tool_name);
      else if (evt.action_type) s.tools.add(evt.action_type);
      if (evt.ticket_id)    s.tickets.add(evt.ticket_id);
      if (evt.action_type) s.actions.set(evt.action_type, (s.actions.get(evt.action_type) || 0) + 1);
      if (!s.firstTs || (evt.ts && evt.ts < s.firstTs)) s.firstTs = evt.ts;
      if (evt.ts && evt.ts > s.lastTs) s.lastTs = evt.ts;
    }
  }
  const arr = Array.from(sessions.values()).map(s => ({
    ...s,
    tools: Array.from(s.tools),
    tickets: Array.from(s.tickets),
    actions: Object.fromEntries(s.actions),
  }));
  arr.sort((a, b) => String(b.lastTs || '').localeCompare(String(a.lastTs || '')));
  return { sessions: arr.slice(0, limit), totalSeen: arr.length };
}

function getSession(chainId) {
  const events = [];
  for (const file of listLedgerFiles({ maxDays: 30 })) {
    for (const evt of readJsonl(file)) {
      const sid = chainKey(evt);
      if (sid !== chainId) continue;
      events.push(evt);
    }
  }
  if (!events.length) return null;
  events.sort((a, b) => String(a.ts || '').localeCompare(String(b.ts || '')));
  let costUsd = 0, inputBytes = 0, allowed = 0, denied = 0, heuristic = 0;
  const toolCounts = new Map();
  const ruleCounts = new Map();
  for (const e of events) {
    if (typeof e.cost_usd === 'number') costUsd += e.cost_usd;
    if (typeof e.input_bytes === 'number') inputBytes += e.input_bytes;
    if (e.allowed === true) allowed += 1;
    else if (e.allowed === false) denied += 1;
    if (e.decision === 'heuristic-allow') heuristic += 1;
    const tool = e.tool_name || e.action_type || 'unknown';
    toolCounts.set(tool, (toolCounts.get(tool) || 0) + 1);
    if (e.rule_id) ruleCounts.set(e.rule_id, (ruleCounts.get(e.rule_id) || 0) + 1);
  }
  return {
    chain_id: chainId,
    eventCount: events.length,
    firstTs: events[0].ts,
    lastTs: events[events.length - 1].ts,
    driver: events.find(e => e.driver)?.driver,
    agent: events.find(e => e.agent)?.agent,
    role: events.find(e => e.role)?.role,
    costUsd, inputBytes, allowed, denied, heuristic,
    toolCounts: Object.fromEntries([...toolCounts.entries()].sort((a, b) => b[1] - a[1])),
    ruleCounts: Object.fromEntries([...ruleCounts.entries()].sort((a, b) => b[1] - a[1])),
    events,
  };
}

function getPolicy() {
  try {
    const raw = fs.readFileSync(POLICY_FILE, 'utf8');
    const stat = fs.statSync(POLICY_FILE);
    return { path: POLICY_FILE, size: stat.size, modified: stat.mtimeMs, content: raw };
  } catch (e) { return { path: POLICY_FILE, error: e.message }; }
}

function getAnalyzerSuggestions(query) {
  if (!analyzerDB) {
    return {
      enabled: false,
      note: `analyzer db unavailable at ${ANALYZER_DB}`,
      suggestions: [],
      filters: { type: '', target: '', sort: 'created_at_desc' },
    };
  }
  const type = (query.get('type') || '').trim();
  const target = (query.get('target') || '').trim().toLowerCase();
  const sort = (query.get('sort') || 'created_at_desc').trim();
  const clauses = [];
  const params = {};
  if (type) {
    clauses.push('type = @type');
    params.type = type;
  }
  if (target) {
    clauses.push('LOWER(target) LIKE @target');
    params.target = `%${target}%`;
  }
  const where = clauses.length ? `WHERE ${clauses.join(' AND ')}` : '';
  const order = sort === 'created_at_asc' ? 'created_at ASC' : 'created_at DESC';
  const suggestions = analyzerDB.prepare(`
    SELECT id, type, target, diff, rationale, applied, created_at
    FROM analyzer_suggestions
    ${where}
    ORDER BY ${order}, id ASC
    LIMIT 500
  `).all(params);
  return {
    enabled: true,
    note: suggestions.length ? '' : 'No suggestions yet. Run the analyzer to generate a fresh batch.',
    suggestions,
    filters: { type, target, sort },
  };
}

function runAnalyzer(body) {
  const window = String(body?.window || '24h').trim() || '24h';
  const args = [
    '-m', 'analysis.analyzer',
    '--window', window,
    '--db-path', ANALYZER_DB,
    '--policy-file', POLICY_FILE,
  ];
  if (body?.skip_llm === true) args.push('--skip-llm');
  const proc = spawnSync('python3', args, {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    timeout: 120_000,
    env: { ...process.env, PYTHONPATH: 'python' },
  });
  reopen();
  let summary = null;
  try {
    summary = JSON.parse(proc.stdout || '{}');
  } catch {}
  if (proc.status !== 0) {
    return {
      status: 502,
      body: {
        ok: false,
        error: 'analyzer_failed',
        exit: proc.status,
        stdout: (proc.stdout || '').slice(-2000),
        stderr: (proc.stderr || '').slice(-2000),
      },
    };
  }
  return {
    status: 200,
    body: {
      ok: true,
      summary,
      suggestions: getAnalyzerSuggestions(new URLSearchParams()).suggestions,
    },
  };
}

function listClawtaDecisions(query) {
  if (!clawtaDecisionsDB) return { decisions: [] };
  const limit = Math.min(Number(query.get('limit') || 100), 500);
  try {
    const rows = clawtaDecisionsDB.prepare(`
      SELECT id, ticket_id, driver, model, selection_mode, reasoning, ts
      FROM clawta_decisions ORDER BY ts DESC LIMIT ?
    `).all(limit);
    return { decisions: rows };
  } catch (e) { return { decisions: [], error: e.message }; }
}

function getCostHistogram() {
  const bins = new Array(24).fill(0);
  const now = Date.now();
  for (const file of listLedgerFiles({ maxDays: 2 })) {
    for (const evt of readJsonl(file)) {
      if (typeof evt.cost_usd !== 'number' || !evt.ts) continue;
      const t = Date.parse(evt.ts);
      if (Number.isNaN(t)) continue;
      const hoursAgo = Math.floor((now - t) / 3_600_000);
      if (hoursAgo < 0 || hoursAgo >= 24) continue;
      bins[23 - hoursAgo] += evt.cost_usd;
    }
  }
  return { bins, totalUsd: bins.reduce((a, b) => a + b, 0) };
}

// ---------- Routing ----------
const handlers = [
  [/^\/api\/health$/,                () => ({ ok: true, board: CURRENT_BOARD, ts: Date.now() })],
  [/^\/api\/stats$/,                 () => getStats()],
  [/^\/api\/tasks$/,                 (req, q) => listTasks(q)],
  [/^\/api\/tasks\/(t_[a-zA-Z0-9]+)$/, (req, q, m) => getTask(m[1])],
  [/^\/api\/threads$/,               (req, q) => listThreads(q)],
  [/^\/api\/threads\/(\d+)$/,        (req, q, m) => getThread(Number(m[1]))],
  [/^\/api\/threads\/(\d+)\/attachments\/(\d+)$/, (req, q, m) => getAttachmentEnrichment(Number(m[1]), Number(m[2]))],
  [/^\/api\/assignees$/,             () => listAssignees()],
  [/^\/api\/runs\/recent$/,          (req, q) => listRunsRecent(q)],
  [/^\/api\/argus\/info$/,           () => argusInfo()],
  [/^\/api\/argus\/findings$/,       (req, q) => listArgusFindings(q)],
  [/^\/api\/elo$/,                   () => listEloLeaderboard()],
  [/^\/api\/sessions$/,              (req, q) => listSessions(q)],
  [/^\/api\/sessions\/([a-zA-Z0-9_\-:.]+)$/, (req, q, m) => getSession(decodeURIComponent(m[1]))],
  [/^\/api\/policy$/,                () => getPolicy()],
  [/^\/api\/suggestions$/,           (req, q) => getAnalyzerSuggestions(q)],
  [/^\/api\/clawta\/decisions$/,     (req, q) => listClawtaDecisions(q)],
  [/^\/api\/cost\/histogram$/,       () => getCostHistogram()],
];

// Write handlers. Body is the parsed JSON request body. Each handler
// returns { status, body } or throws.
//
// Writes route through the legacy kanban-flow CLI which still mutates
// the hermes-side DB (the swarm's source of truth during the cutover
// window). After every successful write we refresh the chitin-owned
// mirror by running `chitin-kernel kanban migrate <board>` so the next
// read returns the post-write state. Yes, that's a small latency
// blip; acceptable until Plan 4 retires the hermes DB.
const writeHandlers = [
  [/^\/api\/tasks\/(t_[a-zA-Z0-9]+)\/status$/, (req, body, m) => postTaskStatus(m[1], body)],
  [/^\/api\/analyze$/, (req, body) => runAnalyzer(body)],
];

function postTaskStatus(taskId, body) {
  const status = (body?.status || '').trim();
  const author = (body?.author || os.userInfo().username || 'console').trim();
  const reason = (body?.reason || '').trim();
  const allowed = new Set(['start', 'ready', 'unblock', 'block', 'demote', 'done']);
  if (!allowed.has(status)) {
    return { status: 400, body: { error: 'invalid_status', detail: `status must be one of ${[...allowed].join(', ')}` } };
  }
  if ((status === 'block' || status === 'demote') && !reason) {
    return { status: 400, body: { error: 'reason_required', detail: `${status} requires a reason` } };
  }
  if (status === 'done' && !reason) {
    return { status: 400, body: { error: 'result_required', detail: 'done requires a result summary in `reason`' } };
  }

  const flowArgs = [status, taskId, '--author', author];
  if (status === 'block' || status === 'demote') flowArgs.push(reason);
  if (status === 'done') flowArgs.push('--result', reason);

  const flow = spawnSync('kanban-flow', flowArgs, {
    encoding: 'utf8',
    env: { ...process.env, KANBAN_BOARD: CURRENT_BOARD },
    timeout: 15_000,
  });
  if (flow.status !== 0) {
    return { status: 502, body: {
      error: 'kanban_flow_failed',
      exit: flow.status,
      stderr: (flow.stderr || '').slice(-1000),
      stdout: (flow.stdout || '').slice(-1000),
    }};
  }

  // Refresh the chitin-owned mirror so the next GET reflects the write.
  // If migration fails we still return success — the write landed in
  // hermes, and the next migrate cycle (whether manual or via a cron)
  // will catch up.
  const migrate = spawnSync('chitin-kernel', ['kanban', 'migrate', CURRENT_BOARD], {
    encoding: 'utf8',
    timeout: 30_000,
  });
  const refreshed = migrate.status === 0;
  if (refreshed) reopen();

  return { status: 200, body: {
    ok: true,
    task_id: taskId,
    status,
    flow_stdout: (flow.stdout || '').trim(),
    refreshed,
    refresh_error: refreshed ? null : (migrate.stderr || '').slice(-500),
    task: getTask(taskId),
  }};
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let total = 0;
    req.on('data', (c) => {
      total += c.length;
      if (total > 64 * 1024) { reject(new Error('body_too_large')); req.destroy(); return; }
      chunks.push(c);
    });
    req.on('end', () => {
      const raw = Buffer.concat(chunks).toString('utf8');
      if (!raw) return resolve({});
      try { resolve(JSON.parse(raw)); }
      catch (e) { reject(new Error('invalid_json: ' + e.message)); }
    });
    req.on('error', reject);
  });
}

const server = http.createServer(async (req, res) => {
  if (req.method === 'OPTIONS') {
    res.writeHead(204, {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
      'Access-Control-Allow-Headers': 'content-type',
    });
    res.end();
    return;
  }
  const url = new URL(req.url, `http://${HOST}:${PORT}`);

  if (req.method === 'POST') {
    for (const [re, fn] of writeHandlers) {
      const m = url.pathname.match(re);
      if (m) {
        let body;
        try { body = await readBody(req); }
        catch (e) { return json(res, 400, { error: 'bad_body', detail: String(e.message || e) }); }
        try {
          const out = fn(req, body, m);
          return json(res, out.status, out.body);
        } catch (e) { return serverErr(res, e); }
      }
    }
    return json(res, 404, { error: 'not_found' });
  }

  if (req.method !== 'GET') return json(res, 405, { error: 'method_not_allowed' });

  if (url.pathname === '/api/repo-file') {
    const file = getRepoFile(url.searchParams);
    if (!file) return notFound(res);
    res.writeHead(200, {
      'Content-Type': 'text/plain; charset=utf-8',
      'Cache-Control': 'no-store',
      'Access-Control-Allow-Origin': '*',
    });
    res.end(file.content);
    return;
  }
  for (const [re, fn] of handlers) {
    const m = url.pathname.match(re);
    if (m) {
      try {
        const out = fn(req, url.searchParams, m);
        if (out == null) return notFound(res);
        return json(res, 200, out);
      } catch (e) { return serverErr(res, e); }
    }
  }
  if (STATIC_ROOT && !url.pathname.startsWith('/api/')) {
    return serveStatic(req, res, url.pathname);
  }
  notFound(res);
});

function serveStatic(req, res, pathname) {
  // SPA shape: every non-/api GET that doesn't match a file on disk
  // falls back to index.html so the Angular router can pick up the URL.
  let rel = decodeURIComponent(pathname).replace(/^\/+/, '');
  if (rel === '') rel = 'index.html';
  const safe = path.normalize(rel);
  if (safe.startsWith('..') || path.isAbsolute(safe)) {
    return json(res, 400, { error: 'bad_path' });
  }
  let abs = path.join(STATIC_ROOT, safe);
  let stat;
  try { stat = fs.statSync(abs); } catch { stat = null; }
  if (stat && stat.isDirectory()) {
    abs = path.join(abs, 'index.html');
    try { stat = fs.statSync(abs); } catch { stat = null; }
  }
  if (!stat || !stat.isFile()) {
    // SPA fallback.
    abs = path.join(STATIC_ROOT, 'index.html');
    try { stat = fs.statSync(abs); } catch { stat = null; }
    if (!stat) return notFound(res);
  }
  const ext = path.extname(abs).toLowerCase();
  const mime = STATIC_MIME[ext] || 'application/octet-stream';
  // Hashed bundles get a long cache; index.html is never cached.
  const isHashed = /-[A-Z0-9]{8,}\.(?:js|css|woff2?|svg|png|jpe?g|ico|map)$/i.test(path.basename(abs));
  const cacheControl = isHashed ? 'public, max-age=31536000, immutable' : 'no-store';
  res.writeHead(200, {
    'Content-Type': mime,
    'Content-Length': stat.size,
    'Cache-Control': cacheControl,
    'Access-Control-Allow-Origin': '*',
  });
  fs.createReadStream(abs).pipe(res);
}

server.listen(PORT, HOST, () => {
  console.log(`[chitin-console-api] localhost-only listening on http://${HOST}:${PORT}`);
  console.log(`[chitin-console-api] board=${CURRENT_BOARD}`);
  console.log(`[chitin-console-api] kanban=${BOARD_DB} source=${BOARD_DB_SOURCE} (${kanbanDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] bus=${BUS_DB} (${busDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] argus=${ARGUS_DB} (${argusDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] clawta_decisions=${CLAWTA_DECISIONS_DB} (${clawtaDecisionsDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] clawta=${CLAWTA_DB} (${clawtaDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] analyzer=${ANALYZER_DB} (${analyzerDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] policy=${POLICY_FILE}`);
  console.log(`[chitin-console-api] static_root=${STATIC_ROOT || '(none — /api only)'}`);
});

process.on('SIGINT',  () => { server.close(); process.exit(0); });
process.on('SIGTERM', () => { server.close(); process.exit(0); });
