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
import { spawnSync } from 'node:child_process';
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
const ARGUS_DB = process.env.ARGUS_INDEX_DB || path.join(HOME, '.argus', 'index.db');
const CLAWTA_DECISIONS_DB = process.env.CLAWTA_DECISIONS_DB
  || path.join(HOME, '.openclaw', 'data', 'clawta_decisions.db');
const CLAWTA_DB = process.env.CLAWTA_DB || path.join(HOME, '.openclaw', 'data', 'clawta.db');
const CHAIN_LEDGER_DIR = path.join(HOME, '.chitin');
const POLICY_FILE = process.env.CHITIN_POLICY_FILE || path.join(REPO_ROOT, 'chitin.yaml');
const BUS_DB_PATH = process.env.CHITIN_AGENT_BUS_DB || path.join(HOME, '.chitin', 'agent-bus', 'bus.db');

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

// Reports root — pre-existing static HTML reports previously served on
// port 8888 by `python -m http.server`. Mounting them under /reports/
// on this server folds them into the single Tailscale URL.
const REPORTS_ROOT = process.env.CHITIN_REPORTS_ROOT
  || (fs.existsSync(path.join(HOME, 'labs', 'local-ai-lab', 'wiki', 'assets'))
        ? path.join(HOME, 'labs', 'local-ai-lab', 'wiki', 'assets')
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
let argusDB = null;
let clawtaDecisionsDB = null;
let clawtaDB = null;
let busDB = null;

function openIfExists(p, readonly = true) {
  try {
    if (!fs.existsSync(p)) return null;
    return new Database(p, { readonly, fileMustExist: true });
  } catch (e) {
    console.warn(`[console-api] could not open ${p}: ${e.message}`);
    return null;
  }
}

function reopen() {
  kanbanDB?.close(); argusDB?.close(); clawtaDecisionsDB?.close(); clawtaDB?.close(); busDB?.close();
  kanbanDB           = openIfExists(BOARD_DB);
  argusDB            = openIfExists(ARGUS_DB);
  clawtaDecisionsDB  = openIfExists(CLAWTA_DECISIONS_DB);
  clawtaDB           = openIfExists(CLAWTA_DB);
  // bus.db needs read+write since the threads page lets you reply.
  busDB              = openIfExists(BUS_DB_PATH, false);
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

// ---------- Helpers ----------
function* readJsonl(file) {
  const raw = fs.readFileSync(file, 'utf8');
  for (const line of raw.split('\n')) {
    if (!line.trim()) continue;
    try { yield JSON.parse(line); } catch {}
  }
}

// ---------- Industry-scan parser ----------
// Pulls structured paper cards + telemetry from the latest
// industry-scan-*.html generated by the cron. Uses regex against
// the report's stable class names — adding a real HTML parser
// dependency would dwarf the parser size for what this needs.
function parseIndustryScan() {
  if (!REPORTS_ROOT) return null;
  let file;
  try {
    const latest = path.join(REPORTS_ROOT, 'industry-scan-latest.html');
    if (fs.existsSync(latest)) {
      file = latest;
    } else {
      const candidates = fs.readdirSync(REPORTS_ROOT)
        .filter(f => /^industry-scan-\d{4}-\d{2}-\d{2}\.html$/.test(f))
        .map(f => path.join(REPORTS_ROOT, f))
        .map(p => ({ path: p, mtime: fs.statSync(p).mtimeMs }))
        .sort((a, b) => b.mtime - a.mtime);
      if (!candidates.length) return null;
      file = candidates[0].path;
    }
  } catch { return null; }

  let html;
  try { html = fs.readFileSync(file, 'utf8'); }
  catch { return null; }

  const stat = fs.statSync(file);
  const dateMatch = html.match(/Industry Scan\s*[—-]\s*(\d{4}-\d{2}-\d{2})/);

  // Telemetry meta-bar — 4 stat boxes
  const telemetry = [];
  const metaRe = /<div class="meta-box">\s*<div class="value(?:[^"]*)">([^<]+)<\/div>\s*<div class="label">([^<]+)<\/div>/g;
  let m;
  while ((m = metaRe.exec(html)) !== null) {
    telemetry.push({ value: stripTags(m[1]).trim(), label: stripTags(m[2]).trim() });
  }

  // Section h2 → cards beneath it (until next h2 or end)
  const sections = [];
  const h2Re = /<h2>([^<]+)<\/h2>/g;
  const h2Hits = [];
  while ((m = h2Re.exec(html)) !== null) h2Hits.push({ title: stripTags(m[1]).trim(), index: m.index, end: m.index + m[0].length });
  for (let i = 0; i < h2Hits.length; i++) {
    const start = h2Hits[i].end;
    const end = (i + 1 < h2Hits.length) ? h2Hits[i + 1].index : html.length;
    const slice = html.slice(start, end);
    // Skip the telemetry h2 — already captured above
    if (/Telemetry/i.test(h2Hits[i].title)) continue;
    const papers = parsePapers(slice);
    if (papers.length) sections.push({ title: h2Hits[i].title, papers });
  }

  // Action items at the bottom — usually <ul><li>…</li></ul> inside .action-items
  const actionMatch = html.match(/<div class="action-items">([\s\S]*?)<\/div>/);
  const actions = [];
  if (actionMatch) {
    const liRe = /<li>([\s\S]*?)<\/li>/g;
    let li;
    while ((li = liRe.exec(actionMatch[1])) !== null) {
      actions.push(stripTags(li[1]).trim());
    }
  }

  return {
    file: path.basename(file),
    date: dateMatch ? dateMatch[1] : null,
    generatedAt: stat.mtimeMs,
    telemetry,
    sections,
    actions,
  };
}

function parsePapers(htmlSlice) {
  const cards = [];
  const cardRe = /<div class="card">([\s\S]*?)<\/div>\s*(?=<div class="card">|<\/div>|$)/g;
  // The above is too greedy; use a simpler approach: each card-title triggers a window that ends at the next card-title.
  const titleRe = /<div class="card-title">\s*<a href="([^"]+)">([^<]+)<\/a>\s*<\/div>/g;
  const titles = [];
  let m;
  while ((m = titleRe.exec(htmlSlice)) !== null) {
    titles.push({ url: m[1], title: stripTags(m[2]).trim(), start: m.index });
  }
  for (let i = 0; i < titles.length; i++) {
    const start = titles[i].start;
    const end = (i + 1 < titles.length) ? titles[i + 1].start : htmlSlice.length;
    const block = htmlSlice.slice(start, end);
    // Authors
    const authorsM = block.match(/<div class="card-authors">([^<]+)<\/div>/);
    // Stars — count "★" before the dim wrapper
    const starsM = block.match(/<span class="stars">([\s\S]*?)<\/span>/);
    let starsFilled = 0;
    if (starsM) {
      const before = starsM[1].split('<span class="dim">')[0];
      starsFilled = (before.match(/★/g) || []).length;
    }
    // Tags
    const tagRe = /<span class="tag tag-([a-z-]+)">([^<]+)<\/span>/g;
    const tags = [];
    let t;
    while ((t = tagRe.exec(block)) !== null) {
      tags.push({ kind: t[1], label: stripTags(t[2]).trim() });
    }
    // Insight
    const insightM = block.match(/<div class="insight-box">[\s\S]*?<span class="emoji">[^<]+<\/span>\s*([\s\S]*?)<\/div>/);
    // Summary — everything inside .summary
    const summaryM = block.match(/<div class="summary">([\s\S]*?)<\/div>/);
    cards.push({
      title: titles[i].title,
      url: titles[i].url,
      authors: authorsM ? stripTags(authorsM[1]).trim() : null,
      stars: starsFilled,
      tags,
      insight: insightM ? stripTags(insightM[1]).trim() : null,
      summary: summaryM ? stripTags(summaryM[1]).trim() : null,
    });
  }
  return cards;
}

function stripTags(s) {
  return String(s)
    .replace(/<[^>]+>/g, ' ')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, ' ');
}

// ---------- Agent-bus threads ----------
// Read+write surface for ~/.chitin/agent-bus/bus.db. The Phase 4
// Discord mirror runs as a separate process and propagates new
// messages both ways, so a reply posted via this endpoint shows up
// in #hermes (or whichever channel the thread is bound to) without
// any extra wiring here.
function listThreads(query) {
  if (!busDB) return { threads: [], note: 'agent-bus DB unavailable' };
  const limit = Math.min(Number(query.get('limit') || 100), 500);
  const board = query.get('board');
  const status = query.get('status');
  const conds = [];
  const params = {};
  if (board) { conds.push('board = @board'); params.board = board; }
  if (status) { conds.push('status = @status'); params.status = status; }
  const where = conds.length ? 'WHERE ' + conds.join(' AND ') : '';
  const rows = busDB.prepare(`
    SELECT t.id, t.board, t.task_id, t.title, t.author, t.audience, t.status,
           t.discord_thread_id, t.created_at, t.updated_at,
           (SELECT COUNT(*) FROM messages WHERE thread_id = t.id) AS message_count,
           (SELECT body FROM messages WHERE thread_id = t.id ORDER BY id DESC LIMIT 1) AS last_message_body,
           (SELECT author FROM messages WHERE thread_id = t.id ORDER BY id DESC LIMIT 1) AS last_message_author
    FROM threads t
    ${where}
    ORDER BY t.updated_at DESC
    LIMIT @limit
  `).all({ ...params, limit });
  return { threads: rows };
}

function getThread(id) {
  if (!busDB) return null;
  const thread = busDB.prepare(`
    SELECT id, board, task_id, title, author, audience, status,
           discord_thread_id, created_at, updated_at
    FROM threads WHERE id = ?
  `).get(id);
  if (!thread) return null;
  const messages = busDB.prepare(`
    SELECT id, parent_id, author, audience, body, kind,
           discord_message_id, ack_required, created_at
    FROM messages WHERE thread_id = ?
    ORDER BY id ASC
  `).all(id);
  const attachments = busDB.prepare(`
    SELECT id, kind, ref, display, created_at
    FROM attachments WHERE thread_id = ?
    ORDER BY id ASC
  `).all(id);
  return { thread, messages, attachments };
}

function postThreadReply(threadId, body) {
  if (!busDB) return { status: 503, body: { error: 'bus_unavailable' } };
  const author = (body?.author || os.userInfo().username || 'console').trim();
  const text = (body?.body || '').trim();
  if (!text) return { status: 400, body: { error: 'body_required' } };
  if (text.length > 8000) return { status: 400, body: { error: 'body_too_long' } };
  const parentId = body?.parent_id != null ? Number(body.parent_id) : null;
  const kind = ['message', 'directive', 'ack', 'system'].includes(body?.kind) ? body.kind : 'message';
  const audience = body?.audience || null;
  const ackRequired = body?.ack_required ? 1 : 0;

  const thread = busDB.prepare('SELECT id FROM threads WHERE id = ?').get(threadId);
  if (!thread) return { status: 404, body: { error: 'thread_not_found' } };

  const now = Math.floor(Date.now() / 1000);
  const insert = busDB.prepare(`
    INSERT INTO messages (thread_id, parent_id, author, audience, body, kind, ack_required, created_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  `);
  const touchThread = busDB.prepare('UPDATE threads SET updated_at = ? WHERE id = ?');
  let messageId;
  try {
    const tx = busDB.transaction(() => {
      const r = insert.run(threadId, parentId, author, audience, text, kind, ackRequired, now);
      messageId = r.lastInsertRowid;
      touchThread.run(now, threadId);
    });
    tx();
  } catch (e) {
    return { status: 500, body: { error: 'write_failed', detail: String(e.message || e) } };
  }
  return { status: 200, body: { ok: true, message_id: messageId, thread: getThread(threadId) } };
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

// ---------- Endpoints ----------
function percentile(sorted, p) {
  if (!sorted.length) return null;
  const idx = Math.min(sorted.length - 1, Math.floor(p * sorted.length));
  return sorted[idx];
}

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

  // Cycle time = completed_at - started_at (work-in-progress duration).
  // Lead time   = completed_at - created_at (idea → done).
  // 30-day window over completed tickets. Tasks with NULL started_at
  // (done without ever flipping in_progress) skip the cycle bucket.
  let cycle = { p50: null, p90: null, n: 0 };
  let lead  = { p50: null, p90: null, n: 0 };
  try {
    const completed = kanbanDB.prepare(`
      SELECT
        (completed_at - started_at) AS cycle,
        (completed_at - created_at) AS lead
      FROM tasks
      WHERE status = 'done'
        AND completed_at IS NOT NULL
        AND completed_at > strftime('%s','now') - 30*86400
    `).all();
    const cycleSamples = completed
      .map(r => r.cycle)
      .filter(v => Number.isFinite(v) && v > 0)
      .sort((a, b) => a - b);
    const leadSamples = completed
      .map(r => r.lead)
      .filter(v => Number.isFinite(v) && v > 0)
      .sort((a, b) => a - b);
    cycle = { p50: percentile(cycleSamples, 0.5), p90: percentile(cycleSamples, 0.9), n: cycleSamples.length };
    lead  = { p50: percentile(leadSamples, 0.5),  p90: percentile(leadSamples, 0.9),  n: leadSamples.length };
  } catch {}
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
  } catch {}
  return {
    board: CURRENT_BOARD,
    lanes,
    completedLast7Days: last7,
    inFlight,
    medianAgeSecondsActive: median,
    successRate7d: successRate,
    runsLast24,
    runsCompleted24,
    cycleTime30d: cycle,
    leadTime30d: lead,
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
  try { comments = kanbanDB.prepare('SELECT id, author, body, created_at FROM task_comments WHERE task_id = ? ORDER BY id ASC').all(id); } catch {}
  let links = [];
  try { links = kanbanDB.prepare('SELECT id, rel, ref, created_at FROM task_links WHERE task_id = ? ORDER BY id ASC').all(id); } catch {}
  let clawtaDecisions = [];
  if (clawtaDecisionsDB) {
    try {
      clawtaDecisions = clawtaDecisionsDB.prepare(
        'SELECT id, driver, model, selection_mode, reasoning, ts FROM clawta_decisions WHERE ticket_id = ? ORDER BY ts DESC LIMIT 25'
      ).all(id);
    } catch {}
  }
  return { task, runs, events, comments, links, clawtaDecisions };
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
    catch {}
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
      try { counts[t] = argusDB.prepare(`SELECT COUNT(*) as n FROM "${t}"`).get().n; } catch {}
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

function getAnalyzerSuggestions() {
  return {
    enabled: false,
    note: 'Analyzer cron (slice 5 of the dashboard epic) not yet implemented. ' +
          'Once `analyzer-cron.lobster` ships, this endpoint will read from ' +
          'analyzer_suggestions(id, type, target, diff, rationale, applied, created_at).',
    suggestions: [],
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
  [/^\/api\/assignees$/,             () => listAssignees()],
  [/^\/api\/runs\/recent$/,          (req, q) => listRunsRecent(q)],
  [/^\/api\/argus\/info$/,           () => argusInfo()],
  [/^\/api\/argus\/findings$/,       (req, q) => listArgusFindings(q)],
  [/^\/api\/elo$/,                   () => listEloLeaderboard()],
  [/^\/api\/sessions$/,              (req, q) => listSessions(q)],
  [/^\/api\/sessions\/([a-zA-Z0-9_\-:.]+)$/, (req, q, m) => getSession(decodeURIComponent(m[1]))],
  [/^\/api\/policy$/,                () => getPolicy()],
  [/^\/api\/suggestions$/,           () => getAnalyzerSuggestions()],
  [/^\/api\/clawta\/decisions$/,     (req, q) => listClawtaDecisions(q)],
  [/^\/api\/cost\/histogram$/,       () => getCostHistogram()],
  [/^\/api\/reports\/industry-scan$/, () => parseIndustryScan()],
  [/^\/api\/threads$/,               (req, q) => listThreads(q)],
  [/^\/api\/threads\/(\d+)$/,        (req, q, m) => getThread(Number(m[1]))],
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
  [/^\/api\/tasks\/(t_[a-zA-Z0-9]+)\/status$/,   (req, body, m) => postTaskStatus(m[1], body)],
  [/^\/api\/tasks\/(t_[a-zA-Z0-9]+)\/comment$/,  (req, body, m) => postTaskComment(m[1], body)],
  [/^\/api\/threads\/(\d+)\/reply$/,             (req, body, m) => postThreadReply(Number(m[1]), body)],
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

function postTaskComment(taskId, body) {
  const author = (body?.author || os.userInfo().username || 'console').trim();
  const text = (body?.body || '').trim();
  if (!text) {
    return { status: 400, body: { error: 'body_required', detail: 'comment body is required' } };
  }
  if (text.length > 8000) {
    return { status: 400, body: { error: 'body_too_long', detail: 'comment must be <= 8000 chars' } };
  }

  // Verify the task exists before we touch the DB. getTask uses the
  // current kanbanDB connection which is the chitin-owned mirror.
  const existing = getTask(taskId);
  if (!existing) {
    return { status: 404, body: { error: 'task_not_found', detail: taskId } };
  }

  // Writes need a writable handle. We open a fresh non-readonly
  // connection to the hermes-side DB (source of truth during the
  // cutover window) so kanban-flow's audit invariant holds — every
  // comment also gets a task_events row.
  const home = os.homedir();
  const hermesDB = path.join(home, '.hermes', 'kanban', 'boards', CURRENT_BOARD, 'kanban.db');
  if (!fs.existsSync(hermesDB)) {
    return { status: 500, body: { error: 'no_writable_db', detail: hermesDB } };
  }

  let writeDB;
  try {
    writeDB = new Database(hermesDB, { readonly: false, fileMustExist: true });
  } catch (e) {
    return { status: 500, body: { error: 'open_write_db_failed', detail: String(e.message || e) } };
  }

  try {
    const now = Math.floor(Date.now() / 1000);
    const insertComment = writeDB.prepare(
      'INSERT INTO task_comments (task_id, author, body, created_at) VALUES (?, ?, ?, ?)'
    );
    const insertEvent = writeDB.prepare(
      'INSERT INTO task_events (task_id, kind, payload, created_at) VALUES (?, ?, ?, ?)'
    );
    const tx = writeDB.transaction(() => {
      const c = insertComment.run(taskId, author, text, now);
      insertEvent.run(taskId, 'comment_added', JSON.stringify({ author, comment_id: c.lastInsertRowid }), now);
    });
    tx();
  } catch (e) {
    return { status: 500, body: { error: 'write_failed', detail: String(e.message || e) } };
  } finally {
    writeDB.close();
  }

  // Refresh the chitin mirror so subsequent reads include the new
  // comment. Same pattern as postTaskStatus — soft-fail if migrate
  // returns non-zero; the next migrate cycle catches up.
  const migrate = spawnSync('chitin-kernel', ['kanban', 'migrate', CURRENT_BOARD], {
    encoding: 'utf8',
    timeout: 30_000,
  });
  const refreshed = migrate.status === 0;
  if (refreshed) reopen();

  return { status: 200, body: {
    ok: true,
    task_id: taskId,
    author,
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
  // /api/reports/legacy/* — bridge to the pre-existing cron-generated
  // HTML reports. Kept around so anyone bookmarking a specific report
  // URL still resolves. The Angular /reports route renders the data
  // natively from the same upstream sources.
  if (REPORTS_ROOT && url.pathname.startsWith('/api/reports/legacy/')) {
    return serveReports(req, res, url.pathname.replace('/api/reports/legacy', ''));
  }
  if (STATIC_ROOT && !url.pathname.startsWith('/api/')) {
    return serveStatic(req, res, url.pathname);
  }
  notFound(res);
});

function serveReports(req, res, pathname) {
  // Strip the leading slash and treat as a path under REPORTS_ROOT.
  let rel = pathname.replace(/^\/+/, '');
  if (rel === '') rel = 'index.html';
  rel = decodeURIComponent(rel);
  const safe = path.normalize(rel);
  if (safe.startsWith('..') || path.isAbsolute(safe)) {
    return json(res, 400, { error: 'bad_path' });
  }
  let abs = path.join(REPORTS_ROOT, safe);
  let stat;
  try { stat = fs.statSync(abs); } catch { stat = null; }
  if (stat && stat.isDirectory()) {
    abs = path.join(abs, 'index.html');
    try { stat = fs.statSync(abs); } catch { stat = null; }
  }
  if (!stat || !stat.isFile()) return notFound(res);
  const ext = path.extname(abs).toLowerCase();
  const mime = STATIC_MIME[ext] || 'application/octet-stream';
  // Reports are mutable (regenerated by crons) — never cache.
  res.writeHead(200, {
    'Content-Type': mime,
    'Content-Length': stat.size,
    'Cache-Control': 'no-store',
    'Access-Control-Allow-Origin': '*',
  });
  fs.createReadStream(abs).pipe(res);
}

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
  console.log(`[chitin-console-api] argus=${ARGUS_DB} (${argusDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] clawta_decisions=${CLAWTA_DECISIONS_DB} (${clawtaDecisionsDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] clawta=${CLAWTA_DB} (${clawtaDB ? 'OK' : 'MISSING'})`);
  console.log(`[chitin-console-api] policy=${POLICY_FILE}`);
  console.log(`[chitin-console-api] static_root=${STATIC_ROOT || '(none — /api only)'}`);
  console.log(`[chitin-console-api] reports_root=${REPORTS_ROOT || '(none)'}`);
  console.log(`[chitin-console-api] agent-bus=${BUS_DB_PATH} (${busDB ? 'OK' : 'MISSING'})`);
});

process.on('SIGINT',  () => { server.close(); process.exit(0); });
process.on('SIGTERM', () => { server.close(); process.exit(0); });
