// Stale-doc detector — tech-writer role's debt-detection half.
//
// Purpose: every time a swarm PR renames or deletes a file, every doc
// that referenced that path goes stale silently. Operator notices
// when they grep for the old path and find nothing useful, often
// weeks later. With a daily scan, those refs get filed to debt-ledger
// the moment they break.
//
// What we scan: every `docs/**/*.md` file. Every reference of the
// shape `apps/...`, `libs/...`, `go/...`, `python/...`, `infra/...`,
// `scripts/...`, `tests/...` (the project-relative path families).
// We resolve each reference against the working tree; if it doesn't
// exist, we file an entry.
//
// What we DON'T scan: external URLs (different concern, different
// fix), bare filenames (too lossy), references inside code blocks
// (the operator may quote a deleted path intentionally as a
// historical artifact). The scope is operator-facing prose paths.
//
// Pattern matches researcher.ts / lessons.ts / debt-curator.ts /
// groomer.ts / alarm-feeder.ts: cron-fired, idempotent, telemetry log
// line, no Temporal involvement.

import { fileURLToPath } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { existsSync, readdirSync } from 'node:fs';
import { resolve, relative } from 'node:path';

// ─── Types ────────────────────────────────────────────────────────────────

export interface StaleRef {
  /** Doc file containing the broken ref. */
  doc: string;
  /** 1-based line number in the doc. */
  line: number;
  /** The path the doc references. */
  ref: string;
  /** A short snippet of context (~60 chars) so the operator sees the
   *  surrounding prose. */
  context: string;
}

export interface DetectorOptions {
  repoRoot: string;
  /** Where to look for docs. Default: 'docs'. */
  docsRoot: string;
  /** Where to write the debt entries. */
  ledgerPath: string;
  cap: number;
  /** ISO-8601 timestamp injected by runner. */
  now: string;
  log?: (line: string) => void;
}

export interface DetectorResult {
  total_refs_scanned: number;
  stale_refs: number;
  new_entries: number;
  duplicates_skipped: number;
}

// ─── Reference extraction ────────────────────────────────────────────────

// The path families we scan for. We restrict to project-relative
// paths so we don't false-positive on URLs, package names, or shell
// arguments. Each entry is a path PREFIX; matches start with the
// prefix and run until a delimiter (whitespace, `)`, `'`, `"`, `,`,
// `;`, end of line).
const SCAN_PREFIXES = [
  'apps/',
  'libs/',
  'go/',
  'python/',
  'infra/',
  'scripts/',
  'tests/',
  'docs/',
];

// One ref candidate per regex match. The match is the longest
// prefix-anchored path-shaped substring on the line. We post-process
// to drop trailing markdown / punctuation that snuck in.
const REF_RE = new RegExp(
  '(?:' +
    SCAN_PREFIXES.map((p) => p.replace(/\//g, '\\/')).join('|') +
    ')[A-Za-z0-9_./@\\-]+',
  'g',
);

// Strip trailing punctuation that the regex might capture (markdown
// renderers tolerate "see apps/foo.ts." but the period isn't part of
// the path).
function trimRefPunctuation(s: string): string {
  return s.replace(/[.,;:!?)\]]+$/, '');
}

/**
 * Extract every project-relative path reference from the doc text.
 * Returns raw matches (one entry per occurrence). Caller dedups +
 * resolves against the filesystem.
 *
 * Two skip rules:
 *   1. Lines inside yaml-fenced blocks — those are config (e.g.,
 *      debt-ledger entries with `file: docs/x.md`), not prose. The
 *      detector would otherwise ingest its own previous output.
 *   2. Matches whose match-position follows a `://` on the same line
 *      — those are URL path components, not project-relative paths.
 *      Different concern, different fix.
 */
export function extractRefs(text: string): { ref: string; line: number; context: string }[] {
  const out: { ref: string; line: number; context: string }[] = [];
  const lines = text.split('\n');
  let inYamlFence = false;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.trim().startsWith('```')) {
      // Toggle fenced-block state. Both opening (```yaml) and closing
      // (```) lines hit this branch, so the toggle is symmetric.
      const fenceOpen = line.trim().match(/^```\w*/);
      if (fenceOpen && !inYamlFence) {
        inYamlFence = true;
        continue;
      }
      if (inYamlFence) {
        inYamlFence = false;
        continue;
      }
    }
    if (inYamlFence) continue;

    REF_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = REF_RE.exec(line)) !== null) {
      // URL detection: if the match position is preceded by `://` on
      // the same line (https://, http://, file://, etc.), skip.
      const before = line.slice(0, m.index);
      if (/:\/\//.test(before)) continue;

      // Cross-repo path detection: `gh api repos/<owner>/<repo>/contents/<path>`
      // and `github.com/<owner>/<repo>/blob/<branch>/<path>` are the
      // two common shapes. The path after `contents/` or `blob/<ref>/`
      // is RELATIVE TO THE OTHER REPO, not chitin. Live first run on
      // 2026-05-02 caught 100+ such false positives in
      // docs/observations/research/ where notes reference openclaw +
      // ollama paths via gh api curl examples.
      if (/repos\/[\w.-]+\/[\w.-]+\/contents\/$/.test(before)) continue;
      if (/github\.com\/[\w.-]+\/[\w.-]+\/blob\/[\w.-]+\/$/.test(before)) continue;

      const raw = m[0];
      const ref = trimRefPunctuation(raw);
      if (!ref) continue;
      // Strip optional `:line` or `:line-line` suffix that markdown
      // notation appends (we only resolve the path, not line numbers).
      const pathPart = ref.replace(/:\d+(-\d+)?$/, '').replace(/#.*$/, '');
      out.push({
        ref: pathPart,
        line: i + 1,
        context: shortContext(line),
      });
    }
  }
  return out;
}

function shortContext(line: string): string {
  const trimmed = line.trim().replace(/\s+/g, ' ');
  return trimmed.length > 80 ? `${trimmed.slice(0, 79)}…` : trimmed;
}

// ─── Filesystem resolution ────────────────────────────────────────────────

/**
 * Does the repo-relative path exist on disk? Empty/blank → false
 * (no ref to check). The detector treats absent as "stale", but the
 * resolver itself is just a cache around fs.existsSync.
 */
export function refExists(repoRoot: string, ref: string): boolean {
  if (!ref) return false;
  // Normalize: strip any trailing slash so existsSync agrees on dirs.
  const normalized = ref.replace(/\/+$/, '');
  return existsSync(resolve(repoRoot, normalized));
}

/**
 * Walk `docsRoot` recursively, returning every .md file (repo-relative).
 */
export function walkDocs(repoRoot: string, docsRoot: string): string[] {
  const out: string[] = [];
  const stack = [resolve(repoRoot, docsRoot)];
  while (stack.length > 0) {
    const dir = stack.pop()!;
    let entries;
    try {
      entries = readdirSync(dir, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const ent of entries) {
      const full = resolve(dir, ent.name);
      if (ent.isDirectory()) {
        stack.push(full);
        continue;
      }
      if (ent.isFile() && ent.name.endsWith('.md')) {
        out.push(relative(repoRoot, full));
      }
    }
  }
  return out.sort();
}

// ─── Debt ledger integration (mirrors debt-curator.ts shape) ─────────────

const ENTRY_BLOCK_RE = /```yaml\s*([\s\S]*?)```/g;
const ID_FIELD_RE = /^id:\s*(.+)$/m;

export function parseLedgerIds(text: string): Set<string> {
  const ids = new Set<string>();
  for (const match of text.matchAll(ENTRY_BLOCK_RE)) {
    const block = match[1];
    const idMatch = block.match(ID_FIELD_RE);
    if (idMatch) ids.add(idMatch[1].trim());
  }
  return ids;
}

/**
 * Stable id from a stale ref: `stale-doc-<doc-slug>-<ref-slug>-<sha8>`.
 * Same input → same id (idempotent dedup across runs).
 */
export function staleRefId(stale: StaleRef): string {
  const canonical = `${stale.doc}|${stale.ref}|${stale.context}`;
  let hash = 5381;
  for (let i = 0; i < canonical.length; i++) {
    hash = ((hash << 5) + hash + canonical.charCodeAt(i)) | 0;
  }
  const sha = (hash >>> 0).toString(16).padStart(8, '0').slice(0, 8);
  const docSlug = stale.doc
    .replace(/\..+$/, '')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 30);
  const refSlug = stale.ref
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 30);
  return `stale-doc-${docSlug}-${refSlug}-${sha}`;
}

export function renderEntry(stale: StaleRef, id: string, now: string): string {
  return [
    '---',
    '',
    '```yaml',
    `id: ${id}`,
    `discovered_at: ${now}`,
    'discovered_by: swarm',
    'severity: low',
    'category: doc-debt',
    `file: ${stale.doc}`,
    'status: open',
    'shipped_in:',
    'description: |',
    `  Stale doc reference detected at ${stale.doc}:${stale.line}.`,
    `  Reference: ${stale.ref} (no longer exists in the working tree).`,
    `  Context: ${stale.context}`,
    `  Surfaced by chitin-stale-doc-detector.timer; operator updates the doc to remove or repoint the reference.`,
    '```',
    '',
  ].join('\n');
}

export function appendEntries(text: string, rendered: string[]): string {
  if (rendered.length === 0) return text;
  const trimmed = text.replace(/\s+$/, '');
  return `${trimmed}\n\n${rendered.join('\n')}`;
}

// ─── Runner ──────────────────────────────────────────────────────────────

export async function runStaleDocDetector(opts: DetectorOptions): Promise<DetectorResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const docs = walkDocs(opts.repoRoot, opts.docsRoot);

  const stale: StaleRef[] = [];
  let total_refs_scanned = 0;
  for (const doc of docs) {
    let text: string;
    try {
      text = await readFile(resolve(opts.repoRoot, doc), 'utf8');
    } catch {
      continue;
    }
    const refs = extractRefs(text);
    total_refs_scanned += refs.length;
    for (const r of refs) {
      if (!refExists(opts.repoRoot, r.ref)) {
        stale.push({ doc, line: r.line, ref: r.ref, context: r.context });
      }
    }
  }

  // Dedup against existing ledger entries.
  const ledgerText = await readFile(opts.ledgerPath, 'utf8').catch(() => '');
  const knownIds = parseLedgerIds(ledgerText);

  const renderedEntries: string[] = [];
  let duplicates_skipped = 0;
  const seenInRun = new Set<string>();
  for (const s of stale) {
    if (renderedEntries.length >= opts.cap) break;
    const id = staleRefId(s);
    if (knownIds.has(id) || seenInRun.has(id)) {
      duplicates_skipped++;
      continue;
    }
    seenInRun.add(id);
    renderedEntries.push(renderEntry(s, id, opts.now));
  }

  if (renderedEntries.length > 0) {
    const updated = appendEntries(ledgerText, renderedEntries);
    await writeFile(opts.ledgerPath, updated, 'utf8');
  }

  const result: DetectorResult = {
    total_refs_scanned,
    stale_refs: stale.length,
    new_entries: renderedEntries.length,
    duplicates_skipped,
  };

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'stale-doc-detector',
      ...result,
    }),
  );

  return result;
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_REPO_ROOT = process.env.CHITIN_REPO_ROOT ?? process.cwd();
const DEFAULT_DOCS_ROOT = 'docs';
const DEFAULT_LEDGER_PATH = resolve(DEFAULT_REPO_ROOT, 'docs/debt-ledger.md');
const DEFAULT_CAP = 10;

async function main(): Promise<void> {
  const cap = Number(process.env.CHITIN_STALE_DOC_CAP ?? String(DEFAULT_CAP));
  await runStaleDocDetector({
    repoRoot: DEFAULT_REPO_ROOT,
    docsRoot: DEFAULT_DOCS_ROOT,
    ledgerPath: DEFAULT_LEDGER_PATH,
    cap,
    now: new Date().toISOString(),
  });
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'stale-doc-detector',
        msg: 'stale-doc-detector tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  REF_RE,
  SCAN_PREFIXES,
  ENTRY_BLOCK_RE,
  ID_FIELD_RE,
  shortContext,
  trimRefPunctuation,
  DEFAULT_CAP,
};

