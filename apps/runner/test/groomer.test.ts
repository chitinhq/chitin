import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  appendBacklogEntry,
  candidateToEntryId,
  parseBacklogIds,
  parseRoadmapCandidates,
  renderDraftEntry,
  runGroomer,
  __test__,
  type RoadmapCandidate,
} from '../src/groomer.ts';

const { CANDIDATES_HEADING, DEFAULT_CAP, DEFAULT_PROMOTABLE_SOURCES } = __test__;

function makeCand(overrides: Partial<RoadmapCandidate> = {}): RoadmapCandidate {
  return {
    source: 'arxiv',
    id: '2511.13646v3',
    url: 'https://arxiv.org/abs/2511.13646v3',
    summary: 'Live-SWE-agent v3: tool-registry pattern + benchmark loop',
    ...overrides,
  };
}

// ─── parseRoadmapCandidates ──────────────────────────────────────────────

describe('parseRoadmapCandidates', () => {
  it('returns empty when the section is missing', () => {
    expect(parseRoadmapCandidates('# Roadmap\n\nIntro\n')).toEqual([]);
  });

  it('returns empty when the section exists but has no rows', () => {
    expect(parseRoadmapCandidates(`${CANDIDATES_HEADING}\n\n(none yet)\n`)).toEqual([]);
  });

  it('parses well-formed candidate rows', () => {
    const md = `${CANDIDATES_HEADING}\n\n- [arxiv] [2511.13646v3](https://arxiv.org/abs/2511.13646v3) — Live-SWE-agent v3\n- [reddit] [1abc](https://reddit.com/r/x/1abc) — qwen3 thread\n`;
    const cs = parseRoadmapCandidates(md);
    expect(cs).toHaveLength(2);
    expect(cs[0]).toEqual({
      source: 'arxiv',
      id: '2511.13646v3',
      url: 'https://arxiv.org/abs/2511.13646v3',
      summary: 'Live-SWE-agent v3',
    });
    expect(cs[1].source).toBe('reddit');
  });

  it('stops at the next ## heading (does not bleed into adjacent sections)', () => {
    const md = `${CANDIDATES_HEADING}\n\n- [arxiv] [a-1](u) — note\n\n## Other section\n\n- [arxiv] [bait](u) — should not appear\n`;
    const cs = parseRoadmapCandidates(md);
    expect(cs).toHaveLength(1);
    expect(cs[0].id).toBe('a-1');
  });

  it('handles ids with dots and slashes (gh release tag form)', () => {
    const md = `${CANDIDATES_HEADING}\n\n- [gh:openclaw/openclaw] [openclaw/openclaw@v0.4.2](https://example.com) — release\n`;
    const cs = parseRoadmapCandidates(md);
    expect(cs[0].id).toBe('openclaw/openclaw@v0.4.2');
  });
});

// ─── parseBacklogIds ─────────────────────────────────────────────────────

describe('parseBacklogIds', () => {
  it('extracts ids from `### \\`<id>\\`` headings', () => {
    const md = `# Backlog\n\n### \`role-typed-backlog-entries\`\n\nblah\n\n### \`debt-ledger-analysis-loader\`\n\n`;
    expect(parseBacklogIds(md)).toEqual(
      new Set(['role-typed-backlog-entries', 'debt-ledger-analysis-loader']),
    );
  });

  it('returns empty when no headings match', () => {
    expect(parseBacklogIds('# Backlog\n\nIntro\n')).toEqual(new Set());
  });
});

// ─── candidateToEntryId ──────────────────────────────────────────────────

describe('candidateToEntryId', () => {
  it('slugifies arxiv ids cleanly', () => {
    expect(candidateToEntryId(makeCand({ source: 'arxiv', id: '2511.13646v3' }))).toBe(
      'arxiv-2511-13646v3-investigate',
    );
  });

  it('slugifies reddit-style ids', () => {
    expect(candidateToEntryId(makeCand({ source: 'reddit', id: '1t1n6o8' }))).toBe(
      'reddit-1t1n6o8-investigate',
    );
  });

  it('caps the slug length to keep entry ids tractable', () => {
    const id = 'a'.repeat(200);
    expect(candidateToEntryId(makeCand({ id })).length).toBeLessThan(80);
  });

  it("two different candidates produce different entry ids", () => {
    expect(candidateToEntryId(makeCand({ id: 'a' }))).not.toBe(
      candidateToEntryId(makeCand({ id: 'b' })),
    );
  });
});

// ─── renderDraftEntry ────────────────────────────────────────────────────

describe('renderDraftEntry', () => {
  it('produces a complete `in_design` backlog entry', () => {
    const out = renderDraftEntry(makeCand(), 'arxiv-x-investigate', '2026-05-02T18:00:00Z');
    expect(out).toContain('### `arxiv-x-investigate`');
    expect(out).toContain('status: in_design');
    expect(out).toContain('tier: TBD');
    expect(out).toContain('file: TBD');
    expect(out).toContain('Live-SWE-agent v3');  // summary survives
    expect(out).toContain('arxiv-x-investigate');
  });

  it('embeds the source URL as references_signal', () => {
    const out = renderDraftEntry(
      makeCand({ url: 'https://arxiv.org/abs/foo' }),
      'arxiv-foo-investigate',
      '2026-05-02T18:00:00Z',
    );
    expect(out).toContain('references_signal: https://arxiv.org/abs/foo');
  });

  it('names the next-step expectation (operator/groomer fills tier/file/loc)', () => {
    const out = renderDraftEntry(makeCand(), 'arxiv-x-investigate', '2026-05-02T18:00:00Z');
    expect(out.toLowerCase()).toContain('next step');
    expect(out.toLowerCase()).toContain('groomer');
  });
});

// ─── appendBacklogEntry ──────────────────────────────────────────────────

describe('appendBacklogEntry', () => {
  it('appends rendered entry at the end of the doc', () => {
    const md = '# Backlog\n\n### `existing-entry`\n\nblah\n';
    const rendered = '### `new-entry`\n\nfresh\n';
    const out = appendBacklogEntry(md, rendered);
    expect(out).toContain('existing-entry');
    expect(out).toContain('new-entry');
    expect(out.indexOf('new-entry')).toBeGreaterThan(out.indexOf('existing-entry'));
  });
});

// ─── runGroomer ──────────────────────────────────────────────────────────

describe('runGroomer', () => {
  let scratch: string;
  let roadmapPath: string;
  let backlogPath: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'groomer-test-'));
    roadmapPath = join(scratch, 'roadmap.md');
    backlogPath = join(scratch, 'swarm-backlog.md');
    logs = [];
    writeFileSync(backlogPath, '# Swarm Backlog\n\n## Entries\n\n', 'utf8');
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it("does nothing when roadmap has no candidates", async () => {
    writeFileSync(roadmapPath, '# Roadmap\n\nintro\n', 'utf8');
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(r.promoted).toBe(0);
    expect(r.total_candidates).toBe(0);
    // Backlog file untouched.
    expect(readFileSync(backlogPath, 'utf8')).toContain('## Entries');
  });

  it('promotes an arxiv candidate to an in_design backlog entry', async () => {
    writeFileSync(
      roadmapPath,
      `${CANDIDATES_HEADING}\n\n- [arxiv] [2511.13646v3](https://arxiv.org/abs/2511.13646v3) — Live-SWE-agent v3\n`,
      'utf8',
    );
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(r.promoted).toBe(1);
    const md = readFileSync(backlogPath, 'utf8');
    expect(md).toContain('### `arxiv-2511-13646v3-investigate`');
    expect(md).toContain('status: in_design');
    // Telemetry shape
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('groomer');
    expect(parsed.promoted).toBe(1);
  });

  it('skips reddit candidates by default (source allowlist)', async () => {
    writeFileSync(
      roadmapPath,
      `${CANDIDATES_HEADING}\n\n- [reddit] [1xxx](https://reddit.com/r/x/1xxx) — qwen3 thread\n`,
      'utf8',
    );
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(r.total_candidates).toBe(1);
    expect(r.filtered_to_promotable).toBe(0);
    expect(r.promoted).toBe(0);
  });

  it('respects the source allowlist when expanded by env', async () => {
    writeFileSync(
      roadmapPath,
      `${CANDIDATES_HEADING}\n\n- [reddit] [1xxx](https://reddit.com/r/x/1xxx) — qwen3 thread\n`,
      'utf8',
    );
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv', 'reddit'],
      log: (l) => logs.push(l),
    });
    expect(r.promoted).toBe(1);
  });

  it('dedups against existing backlog entries', async () => {
    writeFileSync(
      roadmapPath,
      `${CANDIDATES_HEADING}\n\n- [arxiv] [foo](https://x) — bar\n`,
      'utf8',
    );
    writeFileSync(
      backlogPath,
      '# Backlog\n\n### `arxiv-foo-investigate`\n\nalready promoted\n',
      'utf8',
    );
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(r.promoted).toBe(0);
    expect(r.duplicates_skipped).toBe(1);
  });

  it('caps promotions per run', async () => {
    const rows = Array.from(
      { length: 10 },
      (_, i) => `- [arxiv] [paper-${i}](https://arxiv.org/abs/paper-${i}) — paper ${i}`,
    ).join('\n');
    writeFileSync(roadmapPath, `${CANDIDATES_HEADING}\n\n${rows}\n`, 'utf8');
    const r = await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 3,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(r.promoted).toBe(3);
    const md = readFileSync(backlogPath, 'utf8');
    expect((md.match(/status: in_design/g) ?? []).length).toBe(3);
  });

  it('does not touch the backlog file when nothing was promoted', async () => {
    writeFileSync(roadmapPath, `${CANDIDATES_HEADING}\n\n`, 'utf8');
    const before = readFileSync(backlogPath, 'utf8');
    await runGroomer({
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l) => logs.push(l),
    });
    expect(readFileSync(backlogPath, 'utf8')).toBe(before);
  });

  it("idempotent on a second run — same candidates produce zero new entries", async () => {
    writeFileSync(
      roadmapPath,
      `${CANDIDATES_HEADING}\n\n- [arxiv] [foo](https://x) — bar\n`,
      'utf8',
    );
    const opts = {
      roadmapPath,
      backlogPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      promotableSources: ['arxiv'],
      log: (l: string) => logs.push(l),
    };
    const r1 = await runGroomer(opts);
    expect(r1.promoted).toBe(1);
    const r2 = await runGroomer(opts);
    expect(r2.promoted).toBe(0);
    expect(r2.duplicates_skipped).toBe(1);
  });
});

// ─── default constants sanity ────────────────────────────────────────────

describe('defaults', () => {
  it("DEFAULT_PROMOTABLE_SOURCES is a conservative allowlist (arxiv only)", () => {
    expect(DEFAULT_PROMOTABLE_SOURCES).toEqual(['arxiv']);
  });

  it('DEFAULT_CAP is 1 (drip-feed, not flood)', () => {
    expect(DEFAULT_CAP).toBe(1);
  });
});
