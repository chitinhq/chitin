import { mkdtempSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  askWorkspace,
  compileWorkspace,
  ingestWorkspace,
  lintWorkspace,
  splitMarkdownIntoSections,
} from '../src/wiki';

describe('splitMarkdownIntoSections', () => {
  it('preserves heading hierarchy and ignores headings inside code fences', () => {
    const sections = splitMarkdownIntoSections(
      [
        '# Root',
        'overview',
        '## Child',
        'child body',
        '```md',
        '## Not a heading',
        '```',
        '### Grandchild',
        'deep body',
      ].join('\n'),
      'docs/test.md',
    );

    expect(sections.map((section) => section.title)).toEqual([
      'Root',
      'Child',
      'Grandchild',
    ]);
    expect(sections[1]?.headingPath).toEqual(['Root', 'Child']);
    expect(sections[1]?.content).toContain('## Not a heading');
    expect(sections[2]?.headingPath).toEqual(['Root', 'Child', 'Grandchild']);
  });
});

describe('wiki pipeline', () => {
  it('ingests, compiles, and answers with citations', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-wiki-'));
    mkdirSync(join(workspace, 'docs', 'decisions'), { recursive: true });
    writeFileSync(
      join(workspace, 'docs', 'decisions', 'dispatch.md'),
      [
        '# Dispatch Gate',
        'The dispatch gate evaluates branch protection and path bounds before push-shaped actions.',
        '## Branch Protection',
        'Protected branches require PR flow and explicit branch checks.',
      ].join('\n'),
      'utf8',
    );

    const ingest = ingestWorkspace(workspace);
    expect(ingest.processed).toBe(1);
    expect(ingest.skipped).toBe(0);

    const compile = compileWorkspace(workspace);
    expect(compile.report.sectionCount).toBeGreaterThan(0);
    expect(compile.report.compiledBytes).toBeGreaterThan(0);

    const ask = askWorkspace(workspace, 'how does the dispatch gate evaluate branch protection?');
    expect(ask.result?.answer).toContain('branch protection');
    expect(ask.result?.citations[0]).toContain('docs/decisions/dispatch.md#');
  });

  it('skips unchanged files on repeated ingest and refreshes when content changes', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-wiki-'));
    mkdirSync(join(workspace, 'docs'), { recursive: true });
    const source = join(workspace, 'docs', 'guide.md');
    writeFileSync(source, '# Guide\noriginal', 'utf8');

    const first = ingestWorkspace(workspace);
    const rawFile = first.manifest.sources[0]?.rawFile;
    const firstMtime = statSync(join(first.paths.rawDir, rawFile)).mtimeMs;

    const second = ingestWorkspace(workspace);
    expect(second.processed).toBe(0);
    expect(second.skipped).toBe(1);

    writeFileSync(source, '# Guide\nupdated', 'utf8');
    const third = ingestWorkspace(workspace);
    expect(third.processed).toBe(1);
    const nextRaw = readFileSync(join(third.paths.rawDir, rawFile), 'utf8');
    expect(nextRaw).toContain('updated');
    expect(statSync(join(third.paths.rawDir, rawFile)).mtimeMs).toBeGreaterThanOrEqual(firstMtime);
  });

  it('reports broken internal references and stale sources in lint', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-wiki-'));
    mkdirSync(join(workspace, 'docs'), { recursive: true });
    writeFileSync(
      join(workspace, 'chitin.wiki.json'),
      JSON.stringify({ sources: ['docs'], staleDays: 1 }),
      'utf8',
    );
    const source = join(workspace, 'docs', 'lint.md');
    writeFileSync(
      source,
      '# Lint\nSee [missing](./missing.md#nope).',
      'utf8',
    );

    ingestWorkspace(workspace);
    compileWorkspace(workspace);

    const report = lintWorkspace(workspace).report;
    expect(report.brokenReferenceCount).toBe(1);
    expect(report.issues.some((issue) => issue.code === 'broken-reference')).toBe(true);
  });

  it('returns null ask result when no compiled knowledge base exists', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-wiki-'));
    const ask = askWorkspace(workspace, 'anything');
    expect(ask.result).toBeNull();
  });
});
