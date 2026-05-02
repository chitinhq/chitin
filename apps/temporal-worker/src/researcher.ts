// Periodic external-signal collector. Fetches recent activity from
// arxiv / Reddit / HN / openclaw / ollama / awesome-openclaw-agents,
// dedups against the existing "Candidates from external signal"
// section of docs/roadmap.md, caps new finds at 5/run, appends, and
// emits a structured telemetry log line for the daily rollup.
//
// Design pointer: docs/design/2026-05-02-swarm-as-software-factory.md
// §3 (researcher role) + §9 Phase 3.
//
// Fired by infra/systemd/chitin-researcher.timer (every 4h). The
// systemd unit invokes:
//   pnpm exec tsx apps/temporal-worker/src/researcher.ts
//
// v1 scope (this file): raw passthrough — each signal becomes one
// candidate row with the source's native title/url/short-blurb. The
// agent-synthesis step (where buildResearcherPrompt is called from
// researcher-prompts.ts to filter/why-line) lives behind --synthesize
// when we wire the Temporal dispatch path. For now the human reader
// of roadmap.md filters; the cap keeps that bounded.
//
// PR #138's bug list (encoded as invariants tests pin):
//   - ESM only: fileURLToPath(import.meta.url), no __dirname / require.main
//   - No new deps: native fetch + node:fs/promises + node:child_process
//   - Cap on new candidates per run (env override)
//   - Telemetry log line for the rollup to consume

import { fileURLToPath } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

// ─── Types ────────────────────────────────────────────────────────────────

/**
 * One raw signal a fetcher returns. `id` is the source's stable
 * native id (arxiv id, reddit post id, gh tag) — stays the dedup key
 * across runs.
 */
export interface RawSignal {
  source: string;
  id: string;
  url: string;
  summary: string;
}

/** A fetcher returns recent items for one source. */
export type Fetcher = () => Promise<RawSignal[]>;

export interface ResearcherRunOptions {
  /** Path to docs/roadmap.md. Tests inject a tmp path. */
  roadmapPath: string;
  /** Source-name → fetcher map. Tests inject canned fetchers; the
   *  default uses the live network fetchers below. */
  fetchers: Record<string, Fetcher>;
  /** Cap on new candidates added per run. Bursty news days can't
   *  flood roadmap.md. */
  cap: number;
  /** Optional log sink (defaults to console.log). Tests pass an array
   *  collector to assert the telemetry shape. */
  log?: (line: string) => void;
}

export interface ResearcherRunResult {
  candidates_opened: number;
  sources_scanned: number;
  duplicates_skipped: number;
  fetcher_errors: { source: string; error: string }[];
}

// ─── Roadmap section management ──────────────────────────────────────────

const CANDIDATES_HEADING = '## Candidates from external signal';

/**
 * Walk the existing roadmap.md text for ids inside the "Candidates
 * from external signal" section. Each candidate row format is:
 *   - [<source>] [<id>](url) — <why>
 * We extract the bracketed id. Returns the empty set when the section
 * is absent (researcher's first run on this roadmap).
 */
export function getExistingCandidateIds(roadmap: string): Set<string> {
  const ids = new Set<string>();
  const sectionStart = roadmap.indexOf(CANDIDATES_HEADING);
  if (sectionStart < 0) return ids;
  const after = roadmap.slice(sectionStart + CANDIDATES_HEADING.length);
  // Stop at the next ## or end-of-file.
  const sectionEnd = after.search(/\n## /);
  const body = sectionEnd >= 0 ? after.slice(0, sectionEnd) : after;
  // The id is the second `[...]` group on a candidate row:
  //   "- [arxiv] [2511.13646v3](https://...) — ..."
  // The regex captures ids that may contain dots, slashes, hyphens.
  const rowRe = /^- \[[^\]]+\] \[([^\]]+)\]\(/gm;
  let match: RegExpExecArray | null;
  while ((match = rowRe.exec(body)) !== null) {
    ids.add(match[1]);
  }
  return ids;
}

/**
 * Append `signals` to the "Candidates from external signal" section of
 * `roadmap`, creating the section if it doesn't exist. New rows go at
 * the END of the section so the existing curation stays in operator-
 * chosen order; readers see "newest at the bottom".
 */
export function appendCandidates(roadmap: string, signals: RawSignal[]): string {
  if (signals.length === 0) return roadmap;
  const rows = signals
    .map((s) => `- [${s.source}] [${s.id}](${s.url}) — ${s.summary}`)
    .join('\n');

  if (!roadmap.includes(CANDIDATES_HEADING)) {
    // Section absent — append at end. Newline-separate from the prior
    // body to avoid collapsing into the previous section.
    const trimmed = roadmap.replace(/\s+$/, '');
    return `${trimmed}\n\n${CANDIDATES_HEADING}\n\n${rows}\n`;
  }

  // Section exists — find its boundary and insert before the next ##
  // (or at EOF if it's the last section).
  const sectionStart = roadmap.indexOf(CANDIDATES_HEADING);
  const after = roadmap.slice(sectionStart + CANDIDATES_HEADING.length);
  const sectionEndOffset = after.search(/\n## /);
  if (sectionEndOffset < 0) {
    // Last section in the file — append rows at end.
    const trimmed = roadmap.replace(/\s+$/, '');
    return `${trimmed}\n${rows}\n`;
  }
  const before = roadmap.slice(0, sectionStart + CANDIDATES_HEADING.length + sectionEndOffset);
  const tail = roadmap.slice(sectionStart + CANDIDATES_HEADING.length + sectionEndOffset);
  // Trim trailing whitespace within the section so we don't pile up
  // blank lines on each run.
  const beforeTrimmed = before.replace(/\s+$/, '');
  return `${beforeTrimmed}\n${rows}\n${tail}`;
}

// ─── Live fetchers ───────────────────────────────────────────────────────

// Each fetcher is a thin native-fetch wrapper. The HTTP boundary is
// what makes them flaky — wrap each in try/catch so one source's
// outage doesn't stall the run. Live fetchers are NOT exported as the
// default; the runner constructs them so test code can swap.

async function fetchArxiv(): Promise<RawSignal[]> {
  // arxiv RSS — a reliable, tiny endpoint. cs.SE + cs.AI cover the
  // surface that intersects swarm/agent work.
  const out: RawSignal[] = [];
  for (const cat of ['cs.SE', 'cs.AI']) {
    const url = `https://export.arxiv.org/rss/${cat}`;
    const res = await fetch(url);
    if (!res.ok) continue;
    const xml = await res.text();
    const items = xml.matchAll(/<item>([\s\S]*?)<\/item>/g);
    for (const item of items) {
      const titleMatch = item[1].match(/<title>([\s\S]*?)<\/title>/);
      const linkMatch = item[1].match(/<link>([\s\S]*?)<\/link>/);
      if (!titleMatch || !linkMatch) continue;
      const arxivIdMatch = linkMatch[1].match(/abs\/([0-9.v]+)/);
      if (!arxivIdMatch) continue;
      out.push({
        source: 'arxiv',
        id: arxivIdMatch[1],
        url: linkMatch[1].trim(),
        summary: stripHtml(titleMatch[1]).slice(0, 160),
      });
    }
  }
  return out;
}

async function fetchReddit(): Promise<RawSignal[]> {
  // r/LocalLLaMA top-of-day — JSON API is unauthenticated for read.
  const url = 'https://www.reddit.com/r/LocalLLaMA/top.json?t=day&limit=15';
  const res = await fetch(url, { headers: { 'user-agent': 'chitin-researcher/1.0' } });
  if (!res.ok) return [];
  const data = (await res.json()) as { data?: { children?: { data?: Record<string, unknown> }[] } };
  const posts = data.data?.children ?? [];
  const out: RawSignal[] = [];
  for (const post of posts) {
    const p = post.data ?? {};
    const title = String(p.title ?? '');
    if (!keywordMatch(title)) continue;
    out.push({
      source: 'reddit',
      id: String(p.id ?? ''),
      url: `https://reddit.com${String(p.permalink ?? '')}`,
      summary: title.slice(0, 160),
    });
  }
  return out;
}

async function fetchHN(): Promise<RawSignal[]> {
  // HN search algolia — last 24h, agent/swarm/coding-agent territory.
  const since = Math.floor((Date.now() - 24 * 60 * 60 * 1000) / 1000);
  const url = `https://hn.algolia.com/api/v1/search_by_date?query=AI+coding+agent&numericFilters=created_at_i>${since}&hitsPerPage=20`;
  const res = await fetch(url);
  if (!res.ok) return [];
  const data = (await res.json()) as { hits?: { objectID?: string; title?: string; url?: string; story_id?: number }[] };
  return (data.hits ?? [])
    .filter((h) => h.title && (h.url || h.story_id))
    .map((h) => ({
      source: 'hn',
      id: String(h.objectID ?? ''),
      url: h.url ?? `https://news.ycombinator.com/item?id=${h.story_id}`,
      summary: String(h.title ?? '').slice(0, 160),
    }));
}

async function fetchOpenclawReleases(): Promise<RawSignal[]> {
  return ghReleases('openclaw/openclaw');
}

async function fetchOllamaReleases(): Promise<RawSignal[]> {
  return ghReleases('ollama/ollama');
}

async function ghReleases(repo: string): Promise<RawSignal[]> {
  const res = await fetch(`https://api.github.com/repos/${repo}/releases?per_page=5`, {
    headers: { 'user-agent': 'chitin-researcher/1.0', accept: 'application/vnd.github+json' },
  });
  if (!res.ok) return [];
  const releases = (await res.json()) as { tag_name?: string; html_url?: string; name?: string }[];
  return releases
    .filter((r) => r.tag_name && r.html_url)
    .map((r) => ({
      source: `gh:${repo}`,
      id: `${repo}@${r.tag_name}`,
      url: String(r.html_url),
      summary: String(r.name ?? r.tag_name ?? '').slice(0, 160),
    }));
}

// ─── Filtering helpers ───────────────────────────────────────────────────

const KEYWORDS = [
  'agent',
  'swarm',
  'orchestrat',
  'mcp',
  'temporal',
  'tool-use',
  'tool use',
  'cline',
  'continue',
  'aider',
  'qwen',
  'deepseek',
];

function keywordMatch(title: string): boolean {
  const lower = title.toLowerCase();
  return KEYWORDS.some((k) => lower.includes(k));
}

function stripHtml(s: string): string {
  return s
    .replace(/<!\[CDATA\[/g, '')
    .replace(/\]\]>/g, '')
    .replace(/<[^>]+>/g, '')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .trim();
}

// ─── Runner ──────────────────────────────────────────────────────────────

/**
 * Run one researcher tick. Pure modulo file IO and the injected
 * fetchers — tests pass canned fetchers + a tmp roadmap path.
 */
export async function runResearcher(
  opts: ResearcherRunOptions,
): Promise<ResearcherRunResult> {
  const log = opts.log ?? ((line: string) => console.log(line));

  const roadmap = await readFile(opts.roadmapPath, 'utf8').catch(() => '');
  const existingIds = getExistingCandidateIds(roadmap);

  const sources = Object.entries(opts.fetchers);
  const fetcher_errors: { source: string; error: string }[] = [];
  const collected: RawSignal[] = [];

  // Run fetchers in parallel — one slow source shouldn't gate the
  // others. Promise.allSettled so a single rejection doesn't tank
  // the whole run.
  const settled = await Promise.allSettled(sources.map(([, fn]) => fn()));
  for (let i = 0; i < settled.length; i++) {
    const [source] = sources[i];
    const result = settled[i];
    if (result.status === 'fulfilled') {
      collected.push(...result.value);
    } else {
      fetcher_errors.push({
        source,
        error: result.reason instanceof Error ? result.reason.message : String(result.reason),
      });
    }
  }

  // Dedup against existing roadmap candidates AND against duplicates
  // within this run (same id from two fetchers).
  const seen = new Set(existingIds);
  const unique: RawSignal[] = [];
  let duplicates_skipped = 0;
  for (const sig of collected) {
    if (seen.has(sig.id)) {
      duplicates_skipped++;
      continue;
    }
    seen.add(sig.id);
    unique.push(sig);
  }

  // Cap. Keep the head (each fetcher returns recent-first; the cap
  // implicitly favors arxiv > reddit > hn > gh-releases by order in
  // the fetchers map, which is the operator-tunable filter).
  const candidates = unique.slice(0, opts.cap);

  if (candidates.length > 0) {
    const updated = appendCandidates(roadmap, candidates);
    await writeFile(opts.roadmapPath, updated, 'utf8');
  }

  const result: ResearcherRunResult = {
    candidates_opened: candidates.length,
    sources_scanned: sources.length,
    duplicates_skipped,
    fetcher_errors,
  };

  // Telemetry. The daily rollup (chitin-swarm-rollup) consumes this.
  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      component: 'researcher',
      ...result,
    }),
  );

  return result;
}

// ─── Main ────────────────────────────────────────────────────────────────

const DEFAULT_FETCHERS: Record<string, Fetcher> = {
  arxiv: fetchArxiv,
  reddit: fetchReddit,
  hn: fetchHN,
  openclaw: fetchOpenclawReleases,
  ollama: fetchOllamaReleases,
};

async function main(): Promise<void> {
  const cap = Number(process.env.CHITIN_RESEARCHER_CAP ?? '5');
  const roadmapPath = resolve(process.cwd(), 'docs/roadmap.md');
  await runResearcher({
    roadmapPath,
    fetchers: DEFAULT_FETCHERS,
    cap,
  });
}

// ESM-equivalent of `if (require.main === module)`. PR #138's first cut
// used `require.main` which was undefined under tsx/ESM and never
// fired — encoded explicitly here so the test reads it back.
const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'researcher',
        msg: 'researcher tick fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    process.exit(1);
  });
}

export const __test__ = {
  KEYWORDS,
  keywordMatch,
  stripHtml,
  CANDIDATES_HEADING,
};
