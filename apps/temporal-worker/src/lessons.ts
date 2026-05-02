// Lessons-learned sidecar — the tech-writer role's first deliverable.
//
// Purpose: every merged swarm PR teaches the next worker something
// (about the codebase, about chitin's own patterns, about prior bugs
// avoided). Without a sidecar, that knowledge dies in PR descriptions
// nobody reads. With it, the dispatcher prepends the most-recent N
// lessons to every programmer prompt, so the next swarm worker
// starts where the last one finished.
//
// Two halves:
//
// 1. `runLessonsExtractor` — scheduled periodically via
//    chitin-lessons.timer. Lists merged swarm PRs, dedups against the
//    sidecar, distills a one-sentence lesson per PR (heuristic in v1;
//    LLM swap point baked in), appends to docs/swarm-lessons.md.
//
// 2. `getRecentLessons` — pure, called by role-prompts.ts at programmer-
//    prompt-build time. Returns the most recent N entries as a string
//    block to prepend.
//
// Why a script, not a workflow: this is deterministic file I/O over
// `gh` output. The swarm's other periodic scripts (researcher.ts,
// swarm-rollup) follow the same systemd-fired-script pattern. Running
// it as a Temporal workflow would buy nothing — there's no
// retry/wall-timeout shape worth Temporal's overhead. Pattern matches
// researcher.ts deliberately so a future operator reads them
// together.
//
// Heuristic vs LLM tradeoff for v1: heuristic only. Title + first
// body paragraph + a couple of file/diff signals. LLM distillation
// (claude-code-headless extracting a *real* lesson from the diff +
// commit messages) is a follow-up entry — when/if heuristic-quality
// proves insufficient. Heuristic ships now because it's deterministic,
// dep-free, and can't accidentally leak secrets through an LLM call.

import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// ─── Types ────────────────────────────────────────────────────────────────

export interface MergedPr {
  number: number;
  title: string;
  body: string;
  mergedAt: string; // ISO-8601
  url: string;
  /** Branch that landed (e.g., `swarm/swarm-foo-12345`). Empty when
   *  the gh API didn't return it (rare). */
  headRefName: string;
}

export interface LessonEntry {
  pr_number: number;
  date: string; // YYYY-MM-DD
  lesson: string; // one sentence
}

export interface LessonsExtractorOptions {
  lessonsPath: string;
  /** Limits the gh PR scan window. v1 default = 30 PRs back. */
  scanLimit: number;
  /** Distillation function. Tests inject a deterministic mock; live
   *  uses the heuristic. Future LLM-backed version swaps in here. */
  distill: (pr: MergedPr) => string;
  /** PR fetcher. Tests inject; live uses the gh CLI wrapper. */
  fetchMergedPrs: (limit: number) => Promise<MergedPr[]>;
  log?: (line: string) => void;
}

export interface LessonsExtractorResult {
  total_scanned: number;
  new_entries: number;
  duplicates_skipped: number;
}

// ─── Lessons file format ─────────────────────────────────────────────────

const LESSONS_HEADER = `# Swarm lessons learned

One-sentence lesson per merged swarm PR. The dispatcher prepends the
most recent N entries to every programmer prompt — so the next swarm
worker starts with what the last one learned.

Format: \`- YYYY-MM-DD #<pr-number> — <lesson>\`. Newest first.

Curated by:
- chitin-lessons.timer (periodic) — heuristic distillation from PR
  title + body + diff stat
- operator (manual) — high-leverage corrections that the heuristic
  missed; preserved across runs (the extractor only appends, never
  rewrites existing entries)

`;

const ENTRY_RE = /^- (\d{4}-\d{2}-\d{2}) #(\d+) — (.+)$/;

/**
 * Parse the lessons file into structured entries. Tolerant of
 * operator hand-edits — anything that doesn't match the row regex
 * is preserved verbatim (we only manipulate the bullet list, not the
 * surrounding doc).
 */
export function parseLessons(text: string): LessonEntry[] {
  const entries: LessonEntry[] = [];
  for (const line of text.split('\n')) {
    const m = line.match(ENTRY_RE);
    if (m) {
      entries.push({ date: m[1], pr_number: Number(m[2]), lesson: m[3].trim() });
    }
  }
  return entries;
}

/**
 * Append `newEntries` to the file's bullet list. Newest-first order.
 * Preserves the doc's prose (header, footnotes) verbatim — we only
 * touch the bullet list. If the file is empty / missing the header,
 * we create the canonical structure.
 */
export function appendLessons(text: string, newEntries: LessonEntry[]): string {
  if (newEntries.length === 0) return text;
  const newRows = newEntries
    .map((e) => `- ${e.date} #${e.pr_number} — ${e.lesson}`)
    .join('\n');

  if (!text || !text.includes('#')) {
    return `${LESSONS_HEADER}${newRows}\n`;
  }

  // Find the first existing entry row; insert new rows before it
  // (newest first). If no entries yet, append at the end of the
  // file's prose.
  const lines = text.split('\n');
  const firstEntryIdx = lines.findIndex((l) => ENTRY_RE.test(l));
  if (firstEntryIdx < 0) {
    const trimmed = text.replace(/\s+$/, '');
    return `${trimmed}\n\n${newRows}\n`;
  }
  return [...lines.slice(0, firstEntryIdx), newRows, ...lines.slice(firstEntryIdx)].join('\n');
}

// ─── Heuristic lesson extractor ──────────────────────────────────────────

/**
 * Deterministic v1 distillation. Given a merged PR, produce a one-
 * sentence lesson. Strategy:
 *
 *   1. If the PR body has a "## Lesson" or "## Why" section, take its
 *      first sentence.
 *   2. Otherwise: PR title (stripped of conventional-commit prefixes)
 *      + " (PR body trim)" if the body's first sentence adds value.
 *   3. Cap at ~200 chars.
 *
 * Heuristic by design — see the file header for the rationale. Errors
 * fall through to a minimal "merged" stub rather than throwing,
 * because the periodic extractor must never crash the timer tick on
 * one weird PR.
 */
export function extractLessonHeuristic(pr: MergedPr): string {
  try {
    // Look for a "## Lesson" or "## Why" section in the body.
    const sectionRe = /^##+\s+(Lesson|Why|Insight)s?\s*\n+([\s\S]*?)(?=\n##+\s|$)/im;
    const sectionMatch = pr.body.match(sectionRe);
    if (sectionMatch) {
      const body = sectionMatch[2].trim();
      const firstSentence = body.split(/(?<=[.!?])\s+/)[0];
      return capLength(firstSentence, 220);
    }
    // Fall back to PR title with conventional-commit prefix stripped.
    const cleanedTitle = pr.title.replace(/^(\w+)(?:\([^)]+\))?:\s*/, '').trim();
    if (cleanedTitle) return capLength(cleanedTitle, 220);
    // Title missing entirely (rare, but encountered in CI test
    // fixtures) — emit a stub so the row still contributes to the
    // dedup set.
    return capLength(`merged PR #${pr.number}`, 220);
  } catch {
    return capLength(pr.title || `merged PR #${pr.number}`, 220);
  }
}

function capLength(s: string, max: number): string {
  const trimmed = s.trim().replace(/\s+/g, ' ');
  return trimmed.length > max ? `${trimmed.slice(0, max - 1)}…` : trimmed;
}

// ─── PR fetcher (live) ───────────────────────────────────────────────────

interface GhPrJsonRow {
  number: number;
  title: string;
  body: string;
  mergedAt: string;
  url: string;
  headRefName: string;
}

/**
 * Live PR fetcher. Uses `gh pr list` filtered to merged + branch
 * prefix `swarm/`. Returns rows newest-first. Failure throws —
 * runLessonsExtractor catches.
 */
export async function fetchMergedSwarmPrs(limit: number): Promise<MergedPr[]> {
  const out = execFileSync(
    'gh',
    [
      'pr',
      'list',
      '--state',
      'merged',
      '--search',
      'head:swarm/',
      '--limit',
      String(limit),
      '--json',
      'number,title,body,mergedAt,url,headRefName',
    ],
    { encoding: 'utf8' },
  );
  const rows = JSON.parse(out) as GhPrJsonRow[];
  return rows.map((r) => ({
    number: r.number,
    title: r.title,
    body: r.body ?? '',
    mergedAt: r.mergedAt,
    url: r.url,
    headRefName: r.headRefName ?? '',
  }));
}

// ─── Runner ──────────────────────────────────────────────────────────────

/**
 * Run one extractor tick. Reads the lessons file, fetches recent
 * merged swarm PRs, dedups by pr_number, distills new ones, appends.
 * Idempotent: re-runs on the same input produce zero new entries.
 */
export async function runLessonsExtractor(
  opts: LessonsExtractorOptions,
): Promise<LessonsExtractorResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const text = await readFile(opts.lessonsPath, 'utf8').catch(() => '');
  const existing = parseLessons(text);
  const knownPrs = new Set(existing.map((e) => e.pr_number));

  const merged = await opts.fetchMergedPrs(opts.scanLimit);

  const newEntries: LessonEntry[] = [];
  let duplicates_skipped = 0;
  for (const pr of merged) {
    if (knownPrs.has(pr.number)) {
      duplicates_skipped++;
      continue;
    }
    const lesson = opts.distill(pr).trim();
    if (!lesson) continue;
    newEntries.push({
      pr_number: pr.number,
      date: pr.mergedAt.slice(0, 10),
      lesson,
    });
  }

  if (newEntries.length > 0) {
    // Sort newest-first so they land at the top of the bullet list.
    newEntries.sort((a, b) => b.pr_number - a.pr_number);
    const updated = appendLessons(text, newEntries);
    await writeFile(opts.lessonsPath, updated, 'utf8');
  }

  const result: LessonsExtractorResult = {
    total_scanned: merged.length,
    new_entries: newEntries.length,
    duplicates_skipped,
  };

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'lessons-extractor',
      ...result,
    }),
  );

  return result;
}

// ─── Prompt-injection helper ─────────────────────────────────────────────

/**
 * Read the lessons file and return the most-recent `n` entries as a
 * markdown bullet block suitable for prepending to a programmer
 * prompt. Returns the empty string when there are no lessons yet
 * (the prompt builder skips the section in that case rather than
 * showing an empty heading).
 *
 * Sync by design — called from buildProgrammerPrompt at every
 * dispatcher dispatch. Async would force the prompt builder up the
 * call chain to be async too. The file is small (a few KB at steady
 * state), so a sync read is cheaper than the refactor.
 */
export function getRecentLessonsSync(lessonsPath: string, n: number): string {
  let text = '';
  try {
    text = readFileSync(lessonsPath, 'utf8');
  } catch {
    return '';
  }
  if (!text) return '';
  const entries = parseLessons(text);
  if (entries.length === 0) return '';
  const head = entries.slice(0, n);
  return head.map((e) => `- #${e.pr_number}: ${e.lesson}`).join('\n');
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_LESSONS_PATH = resolve(process.cwd(), 'docs/swarm-lessons.md');

async function main(): Promise<void> {
  const limit = Number(process.env.CHITIN_LESSONS_SCAN_LIMIT ?? '30');
  await runLessonsExtractor({
    lessonsPath: DEFAULT_LESSONS_PATH,
    scanLimit: limit,
    distill: extractLessonHeuristic,
    fetchMergedPrs: fetchMergedSwarmPrs,
  });
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'lessons-extractor',
        msg: 'lessons tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  LESSONS_HEADER,
  ENTRY_RE,
  capLength,
  DEFAULT_LESSONS_PATH,
};
