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
   *  uses the heuristic OR an LLM-backed distiller when
   *  CHITIN_LESSONS_USE_LLM=1. Returning '' (or whitespace-only) is
   *  the "skip this PR" signal — runner won't write a blank row. */
  distill: (pr: MergedPr) => string | Promise<string>;
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

// ─── LLM-backed distiller (v2) ──────────────────────────────────────────

/**
 * Reflective distillation via `claude -p` headless. Reads the PR's
 * title + body + diff and asks Claude for a one-sentence lesson the
 * NEXT swarm worker would benefit from knowing — a non-obvious
 * gotcha, contract change, or lesson learned. Falls back to the
 * heuristic on any failure (claude binary missing, timeout, parse
 * failure, empty output) so a flaky LLM call never breaks the
 * extractor's idempotency.
 *
 * Cost: a 1-2k-token PR diff at haiku rates is ~$0.0003 per call.
 * Daily extraction over a month of merged PRs is ~$0.01. The price
 * is worth the quality gap between heuristic (descriptive) and LLM
 * (reflective).
 *
 * Why not the kernel's claudecode driver: that path goes through
 * Temporal + activity layer; this is a one-shot script call from
 * inside another one-shot script. Direct `claude -p` is the right
 * tool for the lifetime + scope.
 */
export async function distillWithClaude(pr: MergedPr): Promise<string> {
  const diff = await fetchPrDiff(pr.number);
  const prompt = buildClaudeDistillPrompt(pr, diff);
  try {
    const out = await runClaudeHeadless(prompt);
    const lesson = parseClaudeLesson(out);
    if (lesson) return lesson;
    // Empty / unparseable → fall through to heuristic.
    return extractLessonHeuristic(pr);
  } catch {
    return extractLessonHeuristic(pr);
  }
}

/**
 * Build the LLM prompt. The agent gets PR metadata + a truncated diff
 * and is asked for a single sentence — no preamble, no explanation.
 * The marker `<<<LESSON>>>` lets the parser pick the right line out
 * of any chatty preamble Claude might emit despite instructions.
 */
export function buildClaudeDistillPrompt(pr: MergedPr, diff: string): string {
  // Diffs can be large; cap at ~8k chars (haiku context budget for
  // headroom on the response). Tail-truncate so the most-relevant
  // changes (typically end-of-diff are tests confirming the fix)
  // are preserved.
  const cappedDiff = diff.length > 8000 ? `…[truncated]…\n${diff.slice(-7900)}` : diff;
  return `You are distilling a one-sentence lesson from a merged swarm PR. Future swarm workers see a list of these prepended to their prompts; the lesson should help them avoid re-discovering the same gotcha.

PR title: ${pr.title}
PR body (truncated):
${pr.body.slice(0, 1500)}

Diff (truncated):
${cappedDiff}

Output rules:
- ONE sentence (≤ 200 chars). No preamble, no markdown bullet, no quotes.
- Capture the LESSON, not the WORK. "What was done" is in the title; you should add what the next worker would learn from this.
- If the change is a routine merge with no transferable lesson (e.g., docs typo, version bump), respond with the literal token \`SKIP\` and nothing else.
- Prefix your line with \`<<<LESSON>>>\` so the parser can find it.

Examples:
<<<LESSON>>>Workflow code can't import from @chitin/contracts barrel — node:crypto pulls in via hash.ts and the bundler rejects it; use type-only imports.
<<<LESSON>>>PyYAML auto-converts unquoted ISO-8601 timestamps to datetime; coerce back to string before validating against a schema that expects string.
<<<LESSON>>>SKIP

Now distill the PR above.`;
}

/**
 * Spawn `claude -p` with the prompt on stdin. Times out at 60s
 * (haiku is fast — anything past this is a stuck binary). Returns
 * stdout. Errors propagate so distillWithClaude can fall back.
 */
async function runClaudeHeadless(prompt: string): Promise<string> {
  // Lazy import to keep test paths from spawning child processes.
  const { spawn } = await import('node:child_process');
  return new Promise((resolveStdout, rejectErr) => {
    const child = spawn('claude', ['-p', '--model', 'claude-haiku-4-5'], {
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    let out = '';
    let err = '';
    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      rejectErr(new Error('claude -p timed out'));
    }, 60_000);
    child.stdout.on('data', (b) => (out += b.toString()));
    child.stderr.on('data', (b) => (err += b.toString()));
    child.on('error', (e) => {
      clearTimeout(timer);
      rejectErr(e);
    });
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code === 0) resolveStdout(out);
      else rejectErr(new Error(`claude exit=${code} stderr=${err.slice(-500)}`));
    });
    child.stdin.write(prompt);
    child.stdin.end();
  });
}

const LESSON_MARKER = '<<<LESSON>>>';

/**
 * Pull the lesson out of `claude -p` stdout. Last-marker-wins
 * (matches the reviewer-prompts.ts pattern). `SKIP` → empty
 * (caller falls back to heuristic). Caps at 220 chars.
 */
export function parseClaudeLesson(stdout: string): string {
  const idx = stdout.lastIndexOf(LESSON_MARKER);
  if (idx < 0) return '';
  const after = stdout.slice(idx + LESSON_MARKER.length);
  const lineEnd = after.indexOf('\n');
  const line = (lineEnd >= 0 ? after.slice(0, lineEnd) : after).trim();
  if (!line || line === 'SKIP') return '';
  return capLength(line, 220);
}

/**
 * Fetch the PR diff via gh CLI. Returns the raw diff text or empty
 * string on failure (claude can still emit a lesson from title +
 * body alone).
 */
async function fetchPrDiff(prNumber: number): Promise<string> {
  try {
    return execFileSync('gh', ['pr', 'diff', String(prNumber)], {
      encoding: 'utf8',
      maxBuffer: 4 * 1024 * 1024,
    });
  } catch {
    return '';
  }
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
// --- Fast-path cache and parallel distillation additions ---
import { createHash } from 'node:crypto';
import { cpus } from 'node:os';

const LESSONS_CACHE_PATH = resolve(process.env.HOME || '', '.cache/chitin/lessons-cache.jsonl');
const N_PARALLEL = 4;

type LessonCacheEntry = {
  pr_number: number;
  head_sha: string;
  lesson: string;
};

function loadLessonCache(): Record<string, string> {
  try {
    const text = readFileSync(LESSONS_CACHE_PATH, 'utf8');
    const lines = text.split('\n').filter(Boolean);
    const cache: Record<string, string> = {};
    for (const line of lines) {
      try {
        const entry = JSON.parse(line) as LessonCacheEntry;
        cache[`${entry.pr_number}:${entry.head_sha}`] = entry.lesson;
      } catch {}
    }
    return cache;
  } catch {
    return {};
  }
}

function appendLessonCache(entries: LessonCacheEntry[]) {
  if (!entries.length) return;
  const fd = require('fs').openSync(LESSONS_CACHE_PATH, 'a');
  for (const entry of entries) {
    require('fs').writeSync(fd, JSON.stringify(entry) + '\n');
  }
  require('fs').closeSync(fd);
}

function is_distill_worthwhile(pr: MergedPr & { filesChanged?: number }): boolean {
  const title = pr.title.toLowerCase();
  if (/^(auto:|docs:|chore:|fix\(typo\)|bump:)/.test(title)) return false;
  if ((pr.body || '').length < 20) return false;
  if (pr.filesChanged !== undefined && pr.filesChanged <= 1) return false;
  return true;
}

async function fetchPrHeadSha(prNumber: number): Promise<string> {
  try {
    const out = execFileSync('gh', ['pr', 'view', String(prNumber), '--json', 'headRefOid'], { encoding: 'utf8' });
    const obj = JSON.parse(out);
    return obj.headRefOid || '';
  } catch {
    return '';
  }
}

async function fetchPrFilesChanged(prNumber: number): Promise<number> {
  try {
    const out = execFileSync('gh', ['pr', 'view', String(prNumber), '--json', 'files'], { encoding: 'utf8' });
    const obj = JSON.parse(out);
    return Array.isArray(obj.files) ? obj.files.length : 0;
  } catch {
    return 0;
  }
}

export async function runLessonsExtractor(
  opts: LessonsExtractorOptions,
): Promise<LessonsExtractorResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const text = await readFile(opts.lessonsPath, 'utf8').catch(() => '');
  const existing = parseLessons(text);
  const knownPrs = new Set(existing.map((e) => e.pr_number));

  const merged = await opts.fetchMergedPrs(opts.scanLimit);

  // --- Load cache ---
  const cache = loadLessonCache();
  const newEntries: LessonEntry[] = [];
  let duplicates_skipped = 0;
  const cacheAppends: LessonCacheEntry[] = [];

  // --- Prepare PRs with head_sha and filesChanged ---
  const mergedWithMeta = await Promise.all(
    merged.map(async (pr) => {
      const head_sha = await fetchPrHeadSha(pr.number);
      const filesChanged = await fetchPrFilesChanged(pr.number);
      return { ...pr, head_sha, filesChanged };
    })
  );

  // --- Parallel distillation ---
  const distillTargets = mergedWithMeta.filter(pr => !knownPrs.has(pr.number));
  const results: (LessonEntry | null)[] = [];
  for (let i = 0; i < distillTargets.length; i += N_PARALLEL) {
    const chunk = distillTargets.slice(i, i + N_PARALLEL);
    const chunkResults = await Promise.all(chunk.map(async (pr) => {
      const cacheKey = `${pr.number}:${pr.head_sha}`;
      if (cache[cacheKey]) {
        return {
          pr_number: pr.number,
          date: pr.mergedAt.slice(0, 10),
          lesson: cache[cacheKey],
        };
      }
      if (!is_distill_worthwhile(pr)) {
        return null;
      }
      const distilled = await opts.distill(pr);
      const lesson = distilled.trim();
      if (!lesson) return null;
      cacheAppends.push({ pr_number: pr.number, head_sha: pr.head_sha, lesson });
      return {
        pr_number: pr.number,
        date: pr.mergedAt.slice(0, 10),
        lesson,
      };
    }));
    results.push(...chunkResults);
  }

  for (const pr of mergedWithMeta) {
    if (knownPrs.has(pr.number)) {
      duplicates_skipped++;
    }
  }

  for (const entry of results) {
    if (entry) newEntries.push(entry);
  }

  if (newEntries.length > 0) {
    // Sort newest-first so they land at the top of the bullet list.
    newEntries.sort((a, b) => b.pr_number - a.pr_number);
    const updated = appendLessons(text, newEntries);
    await writeFile(opts.lessonsPath, updated, 'utf8');
    appendLessonCache(cacheAppends);
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
  // CHITIN_LESSONS_USE_LLM=1 → reflective distillation via `claude -p`.
  // Default off — heuristic is dep-free, idempotent, and what the
  // existing rig is calibrated against. Flip on once you've seen
  // a heuristic run land cleanly + the operator wants higher
  // signal-per-row in the lessons file.
  const useLlm = process.env.CHITIN_LESSONS_USE_LLM === '1';
  await runLessonsExtractor({
    lessonsPath: DEFAULT_LESSONS_PATH,
    scanLimit: limit,
    distill: useLlm ? distillWithClaude : extractLessonHeuristic,
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
  LESSON_MARKER,
  capLength,
  DEFAULT_LESSONS_PATH,
};
