// Groomer-as-candidate-promoter (v1).
//
// Closes the autonomy gap that "swarm runs out of work without
// operator." The researcher already drops candidates into
// docs/roadmap.md's "Candidates from external signal" section; an
// operator was the only path from there to the backlog. Now a daily
// timer reads the candidates, dedups against existing backlog
// entries, drafts an `in_design` entry per qualifying candidate,
// and the existing chitin-groom-pass.ts workflow picks it up to
// classify (tier, size, file scope) into a `ready` entry.
//
// Conservative gating in v1 (turn the dial up later if it's too
// quiet, down if it's too noisy):
//
//   - Source filter: only `arxiv` candidates promote auto. Reddit
//     and HN are noisier — operator manually tags those if wanted.
//     openclaw / ollama releases default to operator-curated too,
//     since "version bumped" usually doesn't translate to a
//     swarm-actionable entry without operator interpretation.
//   - Cap: at most 1 promotion per run.
//   - The drafted entry's status is `in_design`, NOT `ready` — the
//     existing groomer (groom-pass.ts) is the next step. v1 promotes
//     into the design queue; tier-classification + scope-naming is
//     the next role's job.
//
// What an "ageRequirement" would buy: insulation from rumored
// papers that get retracted within 24h. v1 skips this — arxiv IDs
// are stable, the source is curated. Add if a flapping candidate
// shows up.

import { fileURLToPath } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// ─── Types ────────────────────────────────────────────────────────────────

export interface RoadmapCandidate {
  source: string;
  id: string;
  url: string;
  summary: string;
}

export interface GroomerOptions {
  roadmapPath: string;
  backlogPath: string;
  cap: number;
  /** ISO-8601 timestamp injected by runner. */
  now: string;
  /** Source-allowlist. Candidates from sources outside this set are
   *  skipped — operator manually promotes those. v1 default is
   *  ['arxiv'] for high-signal-only autopromotion. */
  promotableSources: string[];
  log?: (line: string) => void;
}

export interface GroomerResult {
  total_candidates: number;
  filtered_to_promotable: number;
  promoted: number;
  duplicates_skipped: number;
}

// ─── Roadmap candidate parsing ───────────────────────────────────────────

const CANDIDATES_HEADING = '## Candidates from external signal';

// One candidate row format (matches researcher.ts's appendCandidates):
//   - [<source>] [<id>](url) — <summary>
// We accept whitespace variations and trailing operator notes.
const CANDIDATE_ROW_RE =
  /^- \[([^\]]+)\] \[([^\]]+)\]\(([^)]+)\)\s*[—\-]\s*(.+?)\s*$/;

/**
 * Parse the roadmap.md candidates section. Returns rows in their
 * declared order (newest-at-bottom per researcher's convention).
 * Tolerant of operator hand-edits — non-matching lines preserved
 * elsewhere; only the bullet rows count.
 */
export function parseRoadmapCandidates(text: string): RoadmapCandidate[] {
  const start = text.indexOf(CANDIDATES_HEADING);
  if (start < 0) return [];
  const after = text.slice(start + CANDIDATES_HEADING.length);
  const sectionEnd = after.search(/\n## /);
  const body = sectionEnd >= 0 ? after.slice(0, sectionEnd) : after;
  const out: RoadmapCandidate[] = [];
  for (const line of body.split('\n')) {
    const m = line.match(CANDIDATE_ROW_RE);
    if (m) {
      out.push({ source: m[1], id: m[2], url: m[3], summary: m[4] });
    }
  }
  return out;
}

// ─── Backlog id parsing ──────────────────────────────────────────────────

// Matches any `### \`id\`` heading in swarm-backlog.md.
const BACKLOG_ID_RE = /^### `([^`]+)`/gm;

export function parseBacklogIds(text: string): Set<string> {
  const ids = new Set<string>();
  for (const match of text.matchAll(BACKLOG_ID_RE)) {
    ids.add(match[1]);
  }
  return ids;
}

// ─── Drafted entry shape ─────────────────────────────────────────────────

/**
 * Turn a candidate id into a backlog-entry-shaped slug. arxiv IDs
 * have dots and 'v' suffixes; we slugify to fit the
 * `### \`<id>\`` style without introducing collisions.
 */
export function candidateToEntryId(c: RoadmapCandidate): string {
  const slug = c.id
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60);
  return `${c.source}-${slug}-investigate`;
}

/**
 * Render a draft `in_design` backlog entry that the existing
 * groom-pass.ts flow can classify. Keeps the entry minimal — the
 * groomer agent's job is to fill in tier, file: scope, estimated_loc,
 * blocks. All we provide is the seed.
 */
export function renderDraftEntry(c: RoadmapCandidate, entryId: string, now: string): string {
  return [
    `### \`${entryId}\``,
    '',
    '```yaml',
    `id: ${entryId}`,
    'tier: TBD',
    'status: in_design',
    'estimated_loc: TBD',
    'blocks: []',
    'file: TBD',
    `references_signal: ${c.url}`,
    'role: programmer',
    '```',
    '',
    `Auto-promoted by chitin-groomer.timer at ${now} from \`docs/roadmap.md\` candidate:`,
    '',
    `> [${c.source}] [${c.id}](${c.url}) — ${c.summary}`,
    '',
    'Next step (groomer role): replace `tier: TBD`, `estimated_loc: TBD`, and `file: TBD` with concrete values; classify the work shape (programmer / researcher / refactorer); flip `status` to `ready` when the entry is dispatcher-shaped.',
    '',
  ].join('\n');
}

/**
 * Append a rendered draft entry to swarm-backlog.md. Inserts at the
 * end of the document (operator's chronological convention; the
 * existing groomer reads any in_design entry regardless of position).
 */
export function appendBacklogEntry(text: string, rendered: string): string {
  const trimmed = text.replace(/\s+$/, '');
  return `${trimmed}\n\n${rendered}`;
}

// ─── Runner ──────────────────────────────────────────────────────────────

/**
 * Run one groomer tick. Read roadmap candidates, dedup against
 * backlog, filter by source allowlist, draft up to `cap` new
 * `in_design` entries, append. Idempotent — re-runs on the same
 * input produce zero promotions.
 */
export async function runGroomer(opts: GroomerOptions): Promise<GroomerResult> {
  const log = opts.log ?? ((l: string) => console.log(l));

  const roadmap = await readFile(opts.roadmapPath, 'utf8').catch(() => '');
  const backlog = await readFile(opts.backlogPath, 'utf8').catch(() => '');

  const candidates = parseRoadmapCandidates(roadmap);
  const knownIds = parseBacklogIds(backlog);

  const allowedSources = new Set(opts.promotableSources);
  const promotable = candidates.filter((c) => allowedSources.has(c.source));

  let nextBacklog = backlog;
  let promoted = 0;
  let duplicates_skipped = 0;
  for (const c of promotable) {
    if (promoted >= opts.cap) break;
    const entryId = candidateToEntryId(c);
    if (knownIds.has(entryId)) {
      duplicates_skipped++;
      continue;
    }
    const rendered = renderDraftEntry(c, entryId, opts.now);
    nextBacklog = appendBacklogEntry(nextBacklog, rendered);
    knownIds.add(entryId); // guard against in-run duplicates from the same id
    promoted++;
  }

  if (promoted > 0) {
    await writeFile(opts.backlogPath, nextBacklog, 'utf8');
  }

  const result: GroomerResult = {
    total_candidates: candidates.length,
    filtered_to_promotable: promotable.length,
    promoted,
    duplicates_skipped,
  };

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'groomer',
      ...result,
    }),
  );

  return result;
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_ROADMAP_PATH = resolve(process.cwd(), 'docs/roadmap.md');
const DEFAULT_BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');
const DEFAULT_CAP = 1;
const DEFAULT_PROMOTABLE_SOURCES = ['arxiv'];

async function main(): Promise<void> {
  const cap = Number(process.env.CHITIN_GROOMER_CAP ?? String(DEFAULT_CAP));
  const sources = (process.env.CHITIN_GROOMER_SOURCES ?? DEFAULT_PROMOTABLE_SOURCES.join(','))
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  await runGroomer({
    roadmapPath: DEFAULT_ROADMAP_PATH,
    backlogPath: DEFAULT_BACKLOG_PATH,
    cap,
    now: new Date().toISOString(),
    promotableSources: sources,
  });
}

const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'groomer',
        msg: 'groomer tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  CANDIDATES_HEADING,
  CANDIDATE_ROW_RE,
  BACKLOG_ID_RE,
  DEFAULT_PROMOTABLE_SOURCES,
  DEFAULT_CAP,
};
