import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  appendLessons,
  extractLessonHeuristic,
  getRecentLessonsSync,
  parseLessons,
  runLessonsExtractor,
  __test__,
  type MergedPr,
} from '../src/lessons.ts';

const { ENTRY_RE, capLength, LESSONS_HEADER } = __test__;

function makePr(overrides: Partial<MergedPr> = {}): MergedPr {
  return {
    number: 100,
    title: 'feat(foo): add the bar',
    body: '## Summary\n\nDoes the thing.\n',
    mergedAt: '2026-05-02T12:00:00Z',
    url: 'https://github.com/chitinhq/chitin/pull/100',
    headRefName: 'swarm/swarm-foo-1',
    ...overrides,
  };
}

// ─── parseLessons ─────────────────────────────────────────────────────────

describe('parseLessons', () => {
  it('returns empty array when text has no entries', () => {
    expect(parseLessons('# Header\n\nNo bullets yet.\n')).toEqual([]);
  });

  it('extracts well-formed bullet rows', () => {
    const md = `# Lessons\n\n- 2026-05-02 #150 — fix bundler to drop runtime ExecutionRequestSchema\n- 2026-05-01 #142 — PyYAML auto-converts ISO-8601 timestamps to datetime\n`;
    expect(parseLessons(md)).toEqual([
      { date: '2026-05-02', pr_number: 150, lesson: 'fix bundler to drop runtime ExecutionRequestSchema' },
      { date: '2026-05-01', pr_number: 142, lesson: 'PyYAML auto-converts ISO-8601 timestamps to datetime' },
    ]);
  });

  it('ignores non-matching lines (operator hand-edits)', () => {
    const md = `- 2026-05-02 #150 — real lesson\nrandom note\n- nonsense\n- 2026-05-01 #142 — another lesson\n`;
    expect(parseLessons(md)).toHaveLength(2);
  });
});

// ─── appendLessons ────────────────────────────────────────────────────────

describe('appendLessons', () => {
  it('returns input unchanged when newEntries is empty', () => {
    const md = '# x\n';
    expect(appendLessons(md, [])).toBe(md);
  });

  it('creates the canonical header when input is empty', () => {
    const out = appendLessons('', [
      { date: '2026-05-02', pr_number: 150, lesson: 'do not import contracts barrel from workflow' },
    ]);
    expect(out).toContain('# Swarm lessons learned');
    expect(out).toContain('- 2026-05-02 #150 — do not import contracts barrel');
  });

  it('inserts new rows BEFORE existing entries (newest-first)', () => {
    const md = `${LESSONS_HEADER}- 2026-05-01 #100 — older\n`;
    const out = appendLessons(md, [
      { date: '2026-05-02', pr_number: 150, lesson: 'newer' },
    ]);
    const newerIdx = out.indexOf('#150');
    const olderIdx = out.indexOf('#100');
    expect(newerIdx).toBeLessThan(olderIdx);
    // Header is preserved.
    expect(out).toContain('# Swarm lessons learned');
  });

  it('preserves operator hand-edits in surrounding prose', () => {
    const md = `# Swarm lessons learned\n\nOperator edited prose here — keep it!\n\n- 2026-05-01 #100 — older\n`;
    const out = appendLessons(md, [
      { date: '2026-05-02', pr_number: 150, lesson: 'newer' },
    ]);
    expect(out).toContain('Operator edited prose here — keep it!');
    expect(out).toContain('#100');
    expect(out).toContain('#150');
  });
});

// ─── extractLessonHeuristic ──────────────────────────────────────────────

describe('extractLessonHeuristic', () => {
  it("strips conventional-commit prefix from PR title", () => {
    expect(extractLessonHeuristic(makePr({ title: 'feat(dispatcher): add blocks-respect logic' }))).toBe(
      'add blocks-respect logic',
    );
    expect(extractLessonHeuristic(makePr({ title: 'fix: regex tighten' }))).toBe('regex tighten');
  });

  it("preserves a title that doesn't have a prefix", () => {
    expect(extractLessonHeuristic(makePr({ title: 'plain title' }))).toBe('plain title');
  });

  it('uses the "## Lesson" body section when present', () => {
    const pr = makePr({
      body: `## Summary\n\nblah\n\n## Lesson\n\nNever import barrel from workflow code — pulls node:crypto.\n\n## Test plan\n\n...\n`,
    });
    expect(extractLessonHeuristic(pr)).toContain('Never import barrel');
  });

  it('uses the "## Why" section as a fallback', () => {
    const pr = makePr({
      body: `## Summary\n\nblah\n\n## Why\n\nThe bundler can't reach node:fs from a workflow. Fixed by deduplicating the import.\n`,
    });
    expect(extractLessonHeuristic(pr)).toContain("bundler can't reach node:fs");
  });

  it('caps at ~220 chars with ellipsis', () => {
    const longLesson = 'x'.repeat(400);
    const pr = makePr({ body: `## Lesson\n\n${longLesson}\n` });
    const out = extractLessonHeuristic(pr);
    expect(out.length).toBeLessThanOrEqual(220);
    expect(out.endsWith('…')).toBe(true);
  });

  it('falls back gracefully on bizarre input rather than throwing', () => {
    const pr = makePr({ title: '', body: '' });
    expect(extractLessonHeuristic(pr)).toBeTruthy();
  });
});

// ─── runLessonsExtractor ─────────────────────────────────────────────────

describe('runLessonsExtractor', () => {
  let scratch: string;
  let lessonsPath: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'lessons-test-'));
    lessonsPath = join(scratch, 'swarm-lessons.md');
    logs = [];
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('appends new entries when the file is missing on first run', async () => {
    const r = await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: extractLessonHeuristic,
      fetchMergedPrs: async () => [
        makePr({ number: 100, title: 'feat: alpha', mergedAt: '2026-05-02T10:00:00Z' }),
        makePr({ number: 99, title: 'fix: beta', mergedAt: '2026-05-01T10:00:00Z' }),
      ],
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(2);
    expect(r.duplicates_skipped).toBe(0);
    expect(r.total_scanned).toBe(2);
    const text = readFileSync(lessonsPath, 'utf8');
    expect(text).toContain('#100');
    expect(text).toContain('#99');
    // Telemetry log line
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('lessons-extractor');
    expect(parsed.new_entries).toBe(2);
  });

  it('dedups against existing entries (idempotent re-runs add 0)', async () => {
    writeFileSync(
      lessonsPath,
      `${LESSONS_HEADER}- 2026-05-02 #100 — alpha\n- 2026-05-01 #99 — beta\n`,
      'utf8',
    );
    const r = await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: extractLessonHeuristic,
      fetchMergedPrs: async () => [
        makePr({ number: 100, title: 'feat: alpha' }),
        makePr({ number: 99, title: 'fix: beta' }),
      ],
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(0);
    expect(r.duplicates_skipped).toBe(2);
  });

  it('appends new entries above existing ones (newest first)', async () => {
    writeFileSync(
      lessonsPath,
      `${LESSONS_HEADER}- 2026-05-01 #99 — beta\n`,
      'utf8',
    );
    await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: extractLessonHeuristic,
      fetchMergedPrs: async () => [
        makePr({ number: 100, title: 'feat: alpha new', mergedAt: '2026-05-02T12:00:00Z' }),
      ],
      log: (l) => logs.push(l),
    });
    const text = readFileSync(lessonsPath, 'utf8');
    const newerIdx = text.indexOf('#100');
    const olderIdx = text.indexOf('#99');
    expect(newerIdx).toBeLessThan(olderIdx);
  });

  it('uses the injected distill function (LLM swap point)', async () => {
    let distillCalls = 0;
    await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: (pr) => {
        distillCalls++;
        return `LLM-distilled lesson for #${pr.number}`;
      },
      fetchMergedPrs: async () => [makePr({ number: 100 })],
      log: (l) => logs.push(l),
    });
    expect(distillCalls).toBe(1);
    const text = readFileSync(lessonsPath, 'utf8');
    expect(text).toContain('LLM-distilled lesson for #100');
  });

  it('skips empty distillations rather than writing blank rows', async () => {
    const r = await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: () => '',  // distillation returned nothing usable
      fetchMergedPrs: async () => [makePr({ number: 100 })],
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(0);
    // No file should be created when nothing landed.
    expect(() => readFileSync(lessonsPath, 'utf8')).toThrow();
  });

  it('does not touch the file when there is nothing new', async () => {
    writeFileSync(lessonsPath, `${LESSONS_HEADER}- 2026-05-01 #99 — beta\n`, 'utf8');
    const before = readFileSync(lessonsPath, 'utf8');
    await runLessonsExtractor({
      lessonsPath,
      scanLimit: 5,
      distill: extractLessonHeuristic,
      fetchMergedPrs: async () => [makePr({ number: 99, title: 'fix: beta' })],
      log: (l) => logs.push(l),
    });
    expect(readFileSync(lessonsPath, 'utf8')).toBe(before);
  });
});

// ─── getRecentLessonsSync ────────────────────────────────────────────────

describe('getRecentLessonsSync', () => {
  let scratch: string;
  let lessonsPath: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'lessons-sync-test-'));
    lessonsPath = join(scratch, 'swarm-lessons.md');
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns empty string when the file is missing', () => {
    expect(getRecentLessonsSync(lessonsPath, 5)).toBe('');
  });

  it('returns empty string when the file has no entries', () => {
    writeFileSync(lessonsPath, '# header\n\nno entries yet\n', 'utf8');
    expect(getRecentLessonsSync(lessonsPath, 5)).toBe('');
  });

  it('returns the top N entries as a bullet block', () => {
    writeFileSync(
      lessonsPath,
      `${LESSONS_HEADER}- 2026-05-02 #150 — fix bundler\n- 2026-05-02 #149 — gatekeeper notify\n- 2026-05-01 #100 — older\n`,
      'utf8',
    );
    const out = getRecentLessonsSync(lessonsPath, 2);
    expect(out).toContain('#150');
    expect(out).toContain('#149');
    expect(out).not.toContain('#100');
  });

  it('returns all entries when n exceeds the file length', () => {
    writeFileSync(
      lessonsPath,
      `${LESSONS_HEADER}- 2026-05-02 #150 — only entry\n`,
      'utf8',
    );
    const out = getRecentLessonsSync(lessonsPath, 100);
    expect(out).toContain('#150');
    // Output is a single bullet, not padded with blanks.
    expect(out.split('\n')).toHaveLength(1);
  });
});

// ─── ENTRY_RE / capLength sanity ─────────────────────────────────────────

describe('ENTRY_RE', () => {
  it('matches a well-formed row', () => {
    const m = '- 2026-05-02 #150 — alpha'.match(ENTRY_RE);
    expect(m).not.toBeNull();
    if (m) {
      expect(m[1]).toBe('2026-05-02');
      expect(m[2]).toBe('150');
      expect(m[3]).toBe('alpha');
    }
  });

  it('rejects a row with the wrong shape', () => {
    expect('- 2026-05-02 alpha'.match(ENTRY_RE)).toBeNull();
    expect('- #150 alpha'.match(ENTRY_RE)).toBeNull();
  });
});

describe('capLength', () => {
  it('passes through short strings', () => {
    expect(capLength('short', 100)).toBe('short');
  });

  it('trims to max with ellipsis when over', () => {
    expect(capLength('x'.repeat(300), 100)).toHaveLength(100);
    expect(capLength('x'.repeat(300), 100).endsWith('…')).toBe(true);
  });

  it('collapses internal whitespace runs', () => {
    expect(capLength('a   b\n\tc', 100)).toBe('a b c');
  });
});
