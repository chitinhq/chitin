// Debt-curator role v1 — scans the repo for TODO / FIXME / HACK / XXX
// markers, dedups against existing entries in docs/debt-ledger.md, and
// appends new finds. The role's job per the swarm-as-software-factory
// design (§3 refactorer + debt-curator): "surface duplication / dead
// code / hot-path debt; maintain docs/debt-ledger.md."
//
// v1 scope: comment markers only (TODO/FIXME/HACK/XXX with optional
// trailing context). Cheapest valuable signal — every such marker is
// the previous author flagging "I'd come back to this" debt that
// usually never gets resurrected. Future versions can layer:
//   - high-churn-file detection (top-N files by `git log --since=30d`)
//   - duplication detection via static analysis
//   - dead-code (unreferenced exports)
//
// Why the scope is intentionally narrow now: the auto-curated entries
// land at severity:'low' so a noisy run can't drown the ledger. The
// operator can promote anything they care about. Heuristic-only —
// no LLM call, no judgment call about *whether* something is debt.
// The marker-existing IS the operator's intent to log it; we just
// surface it.
//
// Pattern-matches researcher.ts and lessons.ts: cron-fired script,
// idempotent, telemetry log line, no Temporal involvement.

import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// ─── Types ────────────────────────────────────────────────────────────────

export interface DebtEntry {
  id: string;
  discovered_at: string; // ISO-8601
  discovered_by: 'swarm' | 'operator' | 'user';
  severity: 'blocking' | 'high' | 'medium' | 'low';
  category: 'code-debt' | 'doc-debt' | 'infra-debt' | 'governance-debt';
  file: string;
  status: 'open' | 'claimed' | 'shipped';
  shipped_in?: string;
  description: string;
}

export interface DebtCandidateInput {
  file: string;
  line: number;
  /** The matched marker (TODO / FIXME / HACK / XXX). */
  marker: string;
  /** The text following the marker on the same line, trimmed. */
  context: string;
}

export interface DebtCuratorOptions {
  ledgerPath: string;
  /** Repo-relative root the scan starts from. */
  scanRoot: string;
  /** Cap on new entries written per run. Bursty days can't flood the
   *  ledger with hundreds of low-priority markers. */
  cap: number;
  /** ISO-8601 timestamp injected by the runner. Tests pass a fixed
   *  value; live uses Date.now(). */
  now: string;
  /** Marker scanner. Tests inject canned candidates; live uses
   *  scanForMarkersGitGrep below. */
  scanForMarkers: (root: string) => DebtCandidateInput[];
  log?: (line: string) => void;
}

export interface DebtCuratorResult {
  total_scanned: number;
  new_entries: number;
  duplicates_skipped: number;
}

// ─── Ledger format ───────────────────────────────────────────────────────

const ENTRY_BLOCK_RE = /```yaml\s*([\s\S]*?)```/g;
const ID_FIELD_RE = /^id:\s*(.+)$/m;

/**
 * Parse the existing debt-ledger.md and return the set of known entry
 * ids. Tolerant of operator hand-edits — only entries with a `id:`
 * field count.
 */
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
 * Convert a candidate to a stable id: `<marker-lower>-<file-slug>-<sha8>`.
 * The sha8 is the first 8 chars of a sha256 over the canonical context
 * string, so re-running on an unchanged repo produces identical ids
 * (idempotent dedup).
 */
export function candidateId(c: DebtCandidateInput): string {
  // Pure JS sha-ish: a tiny stable hash over the canonical string. Not
  // cryptographic — just a dedup discriminator. We avoid node:crypto
  // here on principle (this file is workflow-bundle-adjacent in spirit
  // even though it's a runner; staying off node:crypto keeps the
  // option to inline-import elsewhere later).
  const canonical = `${c.marker}|${c.file}|${c.context}`;
  let hash = 5381;
  for (let i = 0; i < canonical.length; i++) {
    hash = ((hash << 5) + hash + canonical.charCodeAt(i)) | 0;
  }
  const sha = (hash >>> 0).toString(16).padStart(8, '0').slice(0, 8);
  const fileSlug = c.file
    .replace(/\..+$/, '') // drop extension
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40);
  return `${c.marker.toLowerCase()}-${fileSlug}-${sha}`;
}

/**
 * Render one yaml entry block as a markdown section the way debt-
 * ledger.md expects. Mirrors the schema in the file's header.
 */
export function renderEntry(entry: DebtEntry): string {
  const lines: string[] = ['---', '', '```yaml'];
  lines.push(`id: ${entry.id}`);
  lines.push(`discovered_at: ${entry.discovered_at}`);
  lines.push(`discovered_by: ${entry.discovered_by}`);
  lines.push(`severity: ${entry.severity}`);
  lines.push(`category: ${entry.category}`);
  lines.push(`file: ${entry.file}`);
  lines.push(`status: ${entry.status}`);
  if (entry.shipped_in) lines.push(`shipped_in: ${entry.shipped_in}`);
  else lines.push('shipped_in:');
  lines.push('description: |');
  for (const line of entry.description.split('\n')) {
    lines.push(`  ${line}`);
  }
  lines.push('```');
  lines.push('');
  return lines.join('\n');
}

/**
 * Append entries to the ledger. Inserts at the END of the document
 * (operator's "newest at the bottom" convention; the date field is
 * the chronological signal). Preserves all existing prose verbatim.
 */
export function appendEntries(text: string, entries: DebtEntry[]): string {
  if (entries.length === 0) return text;
  const rendered = entries.map(renderEntry).join('\n');
  const trimmed = text.replace(/\s+$/, '');
  return `${trimmed}\n\n${rendered}`;
}

// ─── Marker scanner (live) ───────────────────────────────────────────────

const SCAN_MARKERS = ['TODO', 'FIXME', 'HACK', 'XXX'] as const;

// Paths intentionally NOT scanned. v1 false-positive sources caught
// on first live run (2026-05-02): the curator's own source/tests
// mention the marker strings as test fixtures + module-level docs;
// systemd unit files reference the markers in [Description=] /
// comment lines. Excluding those classes keeps the ledger free of
// self-references — the operator was getting 19 entries the first
// run, ~70% of which were the scanner finding its own scan vocabulary.
const SCAN_EXCLUDE_GLOBS = [
  // Docs: debt-ledger.md itself has the schema, design docs quote markers.
  ':(exclude)*.md',
  ':(exclude)docs/',
  // Build / cache / lockfile noise.
  ':(exclude)node_modules/',
  ':(exclude).cache/',
  ':(exclude)tsconfig.tsbuildinfo',
  ':(exclude)*.lock',
  ':(exclude)pnpm-lock.yaml',
  ':(exclude)go.sum',
  // Self-references: the curator's own source + tests use
  // TODO/FIXME/HACK/XXX as literal string fixtures.
  ':(exclude)apps/temporal-worker/src/debt-curator.ts',
  ':(exclude)apps/temporal-worker/test/debt-curator.test.ts',
  // Systemd units describe themselves in [Description=]/comments.
  ':(exclude)infra/systemd/*.service',
  ':(exclude)infra/systemd/*.timer',
  // Go test files use the markers as string sentinels in fixtures.
  ':(exclude)*_test.go',
  // Prompt templates that quote the markers as part of LLM
  // instructions ("a TODO that should be filed"). These get
  // compounded: anything that explains debt vocabulary will
  // mention TODO/FIXME by name.
  ':(exclude)apps/temporal-worker/src/reviewer-prompts.ts',
  ':(exclude)apps/temporal-worker/src/researcher-prompts.ts',
  ':(exclude)apps/temporal-worker/src/role-prompts.ts',
];

// Stricter scan regex: require a comment prefix (//, #, --, /*, *)
// before the marker AND a separator (:, -, space) after. Cuts the
// false-positive rate from ~95% (literal-string mentions) to ~0%.
// Trade-off: rare cases like `marker = 'TODO'` inside a non-test
// file still get matched, but those are usually self-references the
// path excludes catch.
const SCAN_REGEX = `(^|[^a-zA-Z])(//|#|--|\\*)[[:space:]]*(${SCAN_MARKERS.join('|')})([[:space:]]|:|-|\\()`;

/**
 * Live scanner using `git grep`. Returns one candidate per match.
 * Errors (no git, repo too small) are caught and surface as empty —
 * the runner's idempotency rules handle that.
 */
export function scanForMarkersGitGrep(scanRoot: string): DebtCandidateInput[] {
  let raw: string;
  try {
    raw = execFileSync(
      'git',
      ['grep', '-n', '-E', SCAN_REGEX, '--', scanRoot, ...SCAN_EXCLUDE_GLOBS],
      { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 },
    );
  } catch (err) {
    // git grep exits non-zero when there are no matches.
    if ((err as NodeJS.ErrnoException).code === 1 || (err as { status?: number }).status === 1) {
      return [];
    }
    throw err;
  }
  return parseGitGrepOutput(raw);
}

/**
 * Parse `git grep -n` output into typed candidates. Pure (export +
 * test). Format: `path:line:context`. We only keep one match per file
 * line (multiple markers on the same line are vanishingly rare).
 */
export function parseGitGrepOutput(raw: string): DebtCandidateInput[] {
  const out: DebtCandidateInput[] = [];
  for (const line of raw.split('\n')) {
    if (!line) continue;
    const m = line.match(/^([^:]+):(\d+):(.*)$/);
    if (!m) continue;
    const [, file, lineNumStr, context] = m;
    const lineNum = Number(lineNumStr);
    const markerMatch = context.match(/\b(TODO|FIXME|HACK|XXX)\b(.*)/);
    if (!markerMatch) continue;
    const marker = markerMatch[1];
    const trailing = markerMatch[2].trim().replace(/^[:\-]\s*/, '');
    out.push({
      file,
      line: lineNum,
      marker,
      // Trim noise — the leading marker fragments + any closing
      // comment punctuation ("*/", "#", "//") past the actual text.
      context: trailing.replace(/[\s*/#-]+$/, '').slice(0, 240) || `${marker} (no context)`,
    });
  }
  return out;
}

// ─── Runner ──────────────────────────────────────────────────────────────

/**
 * Run one debt-curator tick. Pure modulo file IO + scanForMarkers.
 * Tests inject scanForMarkers + a fixed `now`.
 */
export async function runDebtCurator(opts: DebtCuratorOptions): Promise<DebtCuratorResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const text = await readFile(opts.ledgerPath, 'utf8').catch(() => '');
  const knownIds = parseLedgerIds(text);

  const candidates = opts.scanForMarkers(opts.scanRoot);

  const newEntries: DebtEntry[] = [];
  let duplicates_skipped = 0;
  const seenInRun = new Set<string>();
  for (const c of candidates) {
    const id = candidateId(c);
    if (knownIds.has(id) || seenInRun.has(id)) {
      duplicates_skipped++;
      continue;
    }
    seenInRun.add(id);
    newEntries.push({
      id,
      discovered_at: opts.now,
      discovered_by: 'swarm',
      severity: 'low',
      category: classifyCategory(c.file),
      file: c.file,
      status: 'open',
      description:
        `Auto-detected from ${c.marker} marker at ${c.file}:${c.line}.\n` +
        `Context: ${c.context}\n` +
        `Surfaced by chitin-debt-curator.timer; operator promotes severity / closes / claims as appropriate.`,
    });
    if (newEntries.length >= opts.cap) break;
  }

  if (newEntries.length > 0) {
    const updated = appendEntries(text, newEntries);
    await writeFile(opts.ledgerPath, updated, 'utf8');
  }

  const result: DebtCuratorResult = {
    total_scanned: candidates.length,
    new_entries: newEntries.length,
    duplicates_skipped,
  };

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'debt-curator',
      ...result,
    }),
  );

  return result;
}

/**
 * Heuristic file → category map. The 4 categories from the ledger
 * schema. Most code is `code-debt`; specific paths get a more honest
 * label so the operator can filter.
 */
export function classifyCategory(file: string): DebtEntry['category'] {
  // Governance paths checked FIRST: chitin.yaml ends with .yaml so the
  // infra-debt branch would shadow it otherwise.
  if (file.startsWith('go/execution-kernel/internal/gov/') || file.includes('chitin.yaml')) {
    return 'governance-debt';
  }
  if (file.startsWith('docs/') || file.endsWith('.md')) return 'doc-debt';
  if (file.startsWith('infra/') || file.endsWith('.yml') || file.endsWith('.yaml')) {
    return 'infra-debt';
  }
  return 'code-debt';
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_LEDGER_PATH = resolve(process.cwd(), 'docs/debt-ledger.md');
const DEFAULT_SCAN_ROOT = '.';
const DEFAULT_CAP = 20;

async function main(): Promise<void> {
  const cap = Number(process.env.CHITIN_DEBT_CURATOR_CAP ?? String(DEFAULT_CAP));
  await runDebtCurator({
    ledgerPath: DEFAULT_LEDGER_PATH,
    scanRoot: DEFAULT_SCAN_ROOT,
    cap,
    now: new Date().toISOString(),
    scanForMarkers: scanForMarkersGitGrep,
  });
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'debt-curator',
        msg: 'debt-curator tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  SCAN_MARKERS,
  SCAN_EXCLUDE_GLOBS,
  ENTRY_BLOCK_RE,
  ID_FIELD_RE,
  DEFAULT_CAP,
};
