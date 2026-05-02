import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  runResearcher,
  appendCandidates,
  getExistingCandidateIds,
  __test__,
  type RawSignal,
  type Fetcher,
} from '../src/researcher.ts';

const { keywordMatch, stripHtml, CANDIDATES_HEADING } = __test__;

function makeSignal(overrides: Partial<RawSignal> = {}): RawSignal {
  return {
    source: 'test',
    id: 'sig-1',
    url: 'https://example.com/1',
    summary: 'sample signal',
    ...overrides,
  };
}

// ─── getExistingCandidateIds ─────────────────────────────────────────────

describe('getExistingCandidateIds', () => {
  it('returns an empty set when the candidates section is absent', () => {
    expect(getExistingCandidateIds('# Roadmap\n\nIntro prose.\n')).toEqual(new Set());
  });

  it('extracts ids from a populated candidates section', () => {
    const md = `# Roadmap\n\n${CANDIDATES_HEADING}\n\n- [arxiv] [2511.13646v3](https://arxiv.org/abs/2511.13646v3) — Live-SWE-agent v3\n- [hn] [123456](https://news.ycombinator.com/item?id=123456) — agent thread\n`;
    expect(getExistingCandidateIds(md)).toEqual(new Set(['2511.13646v3', '123456']));
  });

  it('stops at the next ## heading (does not bleed into adjacent sections)', () => {
    const md = `${CANDIDATES_HEADING}\n\n- [arxiv] [a-1](url) — note\n\n## Other section\n\n- [arxiv] [should-not-be-extracted](url) — bait\n`;
    expect(getExistingCandidateIds(md)).toEqual(new Set(['a-1']));
  });

  it('handles ids with dots and slashes (gh repo@tag style)', () => {
    const md = `${CANDIDATES_HEADING}\n\n- [gh:openclaw/openclaw] [openclaw/openclaw@v0.4.2](https://example.com) — release\n`;
    expect(getExistingCandidateIds(md)).toEqual(new Set(['openclaw/openclaw@v0.4.2']));
  });
});

// ─── appendCandidates ─────────────────────────────────────────────────────

describe('appendCandidates', () => {
  it('returns the input unchanged when no signals are passed', () => {
    const md = '# Roadmap\n';
    expect(appendCandidates(md, [])).toBe(md);
  });

  it('creates the candidates section when absent', () => {
    const md = '# Roadmap\n\nIntro prose.\n';
    const out = appendCandidates(md, [makeSignal({ id: 's1', summary: 'first' })]);
    expect(out).toContain(CANDIDATES_HEADING);
    expect(out).toContain('- [test] [s1](https://example.com/1) — first');
  });

  it('appends to an existing candidates section without duplicating the heading', () => {
    const md = `# Roadmap\n\n${CANDIDATES_HEADING}\n\n- [arxiv] [old] (https://example.com/old) — older entry\n`;
    const out = appendCandidates(md, [makeSignal({ id: 'new', summary: 'newer' })]);
    expect(out.match(new RegExp(CANDIDATES_HEADING.replace(/\s/g, '\\s'), 'g'))).toHaveLength(1);
    expect(out).toContain('older entry');
    expect(out).toContain('[new]');
  });

  it('inserts before the next ## section when candidates is not the last section', () => {
    const md = `# Roadmap\n\n${CANDIDATES_HEADING}\n\n- [arxiv] [old](u) — older\n\n## Notes\n\nblah\n`;
    const out = appendCandidates(md, [makeSignal({ id: 'fresh', summary: 'fresh' })]);
    // The new row goes before "## Notes"
    const newRowIdx = out.indexOf('[fresh]');
    const notesIdx = out.indexOf('## Notes');
    expect(newRowIdx).toBeGreaterThan(0);
    expect(newRowIdx).toBeLessThan(notesIdx);
    expect(out).toContain('## Notes');
    expect(out).toContain('older');
  });
});

// ─── keywordMatch / stripHtml ────────────────────────────────────────────

describe('keywordMatch', () => {
  it('matches title containing a keyword (case-insensitive)', () => {
    expect(keywordMatch('Building an Agent Framework')).toBe(true);
    expect(keywordMatch('SWARM coordination paper')).toBe(true);
    expect(keywordMatch('orchestration patterns')).toBe(true);
  });

  it('rejects titles without any keyword', () => {
    expect(keywordMatch('cat photos go viral on lemmy')).toBe(false);
  });
});

describe('stripHtml', () => {
  it('removes simple tags', () => {
    expect(stripHtml('<p>hello <b>world</b></p>')).toBe('hello world');
  });

  it('strips CDATA wrappers (arxiv format)', () => {
    expect(stripHtml('<![CDATA[a paper title]]>')).toBe('a paper title');
  });

  it('decodes named entities', () => {
    expect(stripHtml('a &amp; b &lt;c&gt; "d"')).toContain('a & b');
  });
});

// ─── runResearcher ───────────────────────────────────────────────────────

describe('runResearcher', () => {
  let scratch: string;
  let roadmapPath: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'researcher-test-'));
    roadmapPath = join(scratch, 'roadmap.md');
    logs = [];
    writeFileSync(roadmapPath, '# Roadmap\n\nintro\n', 'utf8');
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  function fetcherReturning(...signals: RawSignal[]): Fetcher {
    return async () => signals;
  }

  it('appends new candidates to roadmap.md and emits telemetry', async () => {
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(
          makeSignal({ source: 'arxiv', id: 'a-1', summary: 'First arxiv hit' }),
          makeSignal({ source: 'arxiv', id: 'a-2', summary: 'Second arxiv hit' }),
        ),
      },
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(2);
    expect(result.sources_scanned).toBe(1);
    expect(result.duplicates_skipped).toBe(0);
    const md = readFileSync(roadmapPath, 'utf8');
    expect(md).toContain('[a-1]');
    expect(md).toContain('[a-2]');
    // Telemetry shape: must include component:"researcher" and counts.
    expect(logs).toHaveLength(1);
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('researcher');
    expect(parsed.candidates_opened).toBe(2);
    expect(parsed.sources_scanned).toBe(1);
  });

  it('dedups against ids already in roadmap', async () => {
    writeFileSync(
      roadmapPath,
      `# Roadmap\n\n${CANDIDATES_HEADING}\n\n- [arxiv] [a-1](u) — already here\n`,
      'utf8',
    );
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(
          makeSignal({ source: 'arxiv', id: 'a-1', summary: 'duplicate' }),
          makeSignal({ source: 'arxiv', id: 'a-2', summary: 'new' }),
        ),
      },
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(1);
    expect(result.duplicates_skipped).toBe(1);
    const md = readFileSync(roadmapPath, 'utf8');
    expect(md.match(/\[a-1\]/g)).toHaveLength(1); // not duplicated
    expect(md).toContain('[a-2]');
  });

  it('caps the number of candidates added per run', async () => {
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(
          makeSignal({ id: 'a-1' }),
          makeSignal({ id: 'a-2' }),
          makeSignal({ id: 'a-3' }),
          makeSignal({ id: 'a-4' }),
          makeSignal({ id: 'a-5' }),
          makeSignal({ id: 'a-6' }),
          makeSignal({ id: 'a-7' }),
        ),
      },
      cap: 3,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(3);
    const md = readFileSync(roadmapPath, 'utf8');
    expect(md).toContain('[a-1]');
    expect(md).toContain('[a-3]');
    expect(md).not.toContain('[a-7]');
  });

  it('captures fetcher errors per-source without tanking the run', async () => {
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(makeSignal({ id: 'a-1' })),
        broken: async () => {
          throw new Error('network down');
        },
        reddit: fetcherReturning(makeSignal({ id: 'r-1' })),
      },
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(2); // 'broken' contributed 0
    expect(result.fetcher_errors).toHaveLength(1);
    expect(result.fetcher_errors[0].source).toBe('broken');
    expect(result.fetcher_errors[0].error).toContain('network');
  });

  it("does NOT touch roadmap.md when there's nothing new to write", async () => {
    const before = readFileSync(roadmapPath, 'utf8');
    const result = await runResearcher({
      roadmapPath,
      fetchers: { arxiv: fetcherReturning() }, // empty
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(0);
    expect(readFileSync(roadmapPath, 'utf8')).toBe(before);
    // Telemetry still fires — operator wants to know the tick ran.
    expect(logs).toHaveLength(1);
  });

  it('handles a missing roadmap.md file gracefully (creates the section on first write)', async () => {
    rmSync(roadmapPath); // simulate first-ever run on a fresh repo
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(makeSignal({ id: 'first', summary: 'kicks it off' })),
      },
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(1);
    const md = readFileSync(roadmapPath, 'utf8');
    expect(md).toContain(CANDIDATES_HEADING);
    expect(md).toContain('[first]');
  });

  it('dedups within a single run when two fetchers surface the same id', async () => {
    const result = await runResearcher({
      roadmapPath,
      fetchers: {
        arxiv: fetcherReturning(makeSignal({ id: 'shared', source: 'arxiv' })),
        // Some other source (cross-posted) lists the same id.
        cross: fetcherReturning(makeSignal({ id: 'shared', source: 'cross' })),
      },
      cap: 5,
      log: (l) => logs.push(l),
    });
    expect(result.candidates_opened).toBe(1);
    expect(result.duplicates_skipped).toBe(1);
  });

  it('runs fetchers in parallel — one slow fetcher does not gate others', async () => {
    let arxivStarted = 0;
    let redditStarted = 0;
    const arxiv: Fetcher = async () => {
      arxivStarted = Date.now();
      await new Promise((r) => setTimeout(r, 50));
      return [makeSignal({ id: 'a-1' })];
    };
    const reddit: Fetcher = async () => {
      redditStarted = Date.now();
      return [makeSignal({ id: 'r-1' })];
    };
    await runResearcher({
      roadmapPath,
      fetchers: { arxiv, reddit },
      cap: 5,
      log: (l) => logs.push(l),
    });
    // Both started within ~10ms of each other = parallel. Sequential
    // would have reddit start after arxiv's 50ms timeout.
    expect(Math.abs(redditStarted - arxivStarted)).toBeLessThan(20);
  });

  it('preserves operator-curated existing candidates (appends, never rewrites)', async () => {
    writeFileSync(
      roadmapPath,
      `# Roadmap\n\n${CANDIDATES_HEADING}\n\n- [arxiv] [curated-1](u) — operator wrote this manually\n- [arxiv] [curated-2](u) — and this\n`,
      'utf8',
    );
    await runResearcher({
      roadmapPath,
      fetchers: { arxiv: fetcherReturning(makeSignal({ id: 'auto-1', summary: 'fresh' })) },
      cap: 5,
      log: (l) => logs.push(l),
    });
    const md = readFileSync(roadmapPath, 'utf8');
    // Operator's curated entries survive verbatim.
    expect(md).toContain('operator wrote this manually');
    expect(md).toContain('[curated-2]');
    expect(md).toContain('[auto-1]');
  });
});
