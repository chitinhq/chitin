import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  appendEntries,
  extractRefs,
  parseLedgerIds,
  refExists,
  renderEntry,
  runStaleDocDetector,
  staleRefId,
  walkDocs,
  __test__,
  type StaleRef,
} from '../src/stale-doc-detector.ts';

const { SCAN_PREFIXES, shortContext, trimRefPunctuation } = __test__;

// ─── extractRefs ──────────────────────────────────────────────────────────

describe('extractRefs', () => {
  it('extracts a single ref from a line', () => {
    const refs = extractRefs('See `apps/temporal-worker/src/foo.ts` for the impl.\n');
    expect(refs).toHaveLength(1);
    expect(refs[0].ref).toBe('apps/temporal-worker/src/foo.ts');
    expect(refs[0].line).toBe(1);
  });

  it("extracts every supported prefix family", () => {
    const text = `
See:
- apps/foo.ts
- libs/bar.ts
- go/baz.go
- python/qux.py
- infra/systemd/x.service
- scripts/y.sh
- tests/z.test.ts
- docs/w.md
`;
    const refs = extractRefs(text);
    const found = refs.map((r) => r.ref);
    for (const prefix of SCAN_PREFIXES) {
      expect(found.some((f) => f.startsWith(prefix))).toBe(true);
    }
  });

  it("does NOT match URLs (different concern)", () => {
    const refs = extractRefs('See https://github.com/owner/apps/foo.ts for context.\n');
    expect(refs).toHaveLength(0);
  });

  it("does NOT match gh-api cross-repo path refs", () => {
    const refs = extractRefs(
      '`gh api repos/openclaw/openclaw/contents/docs/concepts/agent-workspace.md`\n',
    );
    expect(refs).toHaveLength(0);
  });

  it("does NOT match github.com/owner/repo/blob/<ref>/<path> shapes", () => {
    const refs = extractRefs(
      'See github.com/openclaw/openclaw/blob/main/docs/concepts/foo.md for context.\n',
    );
    expect(refs).toHaveLength(0);
  });

  it("does NOT match bare filenames (too lossy)", () => {
    const refs = extractRefs('See foo.ts and bar.go.\n');
    expect(refs).toHaveLength(0);
  });

  it('trims trailing markdown / sentence punctuation', () => {
    expect(extractRefs('See apps/foo.ts.\n')[0].ref).toBe('apps/foo.ts');
    expect(extractRefs('At apps/foo.ts, then ...\n')[0].ref).toBe('apps/foo.ts');
    expect(extractRefs('In `apps/foo.ts`)\n')[0].ref).toBe('apps/foo.ts');
  });

  it('strips :line and :line-range suffixes (we only resolve the path)', () => {
    expect(extractRefs('apps/foo.ts:42 has the bug\n')[0].ref).toBe('apps/foo.ts');
    expect(extractRefs('apps/foo.ts:42-50 covers it\n')[0].ref).toBe('apps/foo.ts');
  });

  it("strips #anchor suffixes (markdown linkage)", () => {
    expect(extractRefs('docs/foo.md#section-name\n')[0].ref).toBe('docs/foo.md');
  });

  it('captures the line number (1-based)', () => {
    const refs = extractRefs('a\nb\napps/x.ts on line 3\n');
    expect(refs[0].line).toBe(3);
  });

  it('captures a short context excerpt', () => {
    const refs = extractRefs('See `apps/foo.ts` for the auto-merge impl.\n');
    expect(refs[0].context).toContain('apps/foo.ts');
    expect(refs[0].context.length).toBeLessThanOrEqual(80);
  });

  it('handles multiple refs on the same line', () => {
    const refs = extractRefs('Both apps/a.ts and libs/b.ts changed.\n');
    expect(refs).toHaveLength(2);
    expect(refs[0].ref).toBe('apps/a.ts');
    expect(refs[1].ref).toBe('libs/b.ts');
  });
});

// ─── refExists ────────────────────────────────────────────────────────────

describe('refExists', () => {
  let scratch: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'stale-doc-test-'));
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns true for an existing file', () => {
    writeFileSync(join(scratch, 'foo.ts'), '');
    expect(refExists(scratch, 'foo.ts')).toBe(true);
  });

  it('returns false for a non-existent file', () => {
    expect(refExists(scratch, 'nope.ts')).toBe(false);
  });

  it('returns true for an existing directory', () => {
    mkdirSync(join(scratch, 'apps'));
    expect(refExists(scratch, 'apps/')).toBe(true);
    expect(refExists(scratch, 'apps')).toBe(true);
  });

  it('returns false for empty / blank ref', () => {
    expect(refExists(scratch, '')).toBe(false);
  });
});

// ─── walkDocs ─────────────────────────────────────────────────────────────

describe('walkDocs', () => {
  let scratch: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'stale-doc-walk-test-'));
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns empty when the docs root is missing', () => {
    expect(walkDocs(scratch, 'no-such-docs')).toEqual([]);
  });

  it('returns markdown files under the docs root, recursively', () => {
    mkdirSync(join(scratch, 'docs/sub'), { recursive: true });
    writeFileSync(join(scratch, 'docs/a.md'), '');
    writeFileSync(join(scratch, 'docs/sub/b.md'), '');
    writeFileSync(join(scratch, 'docs/c.txt'), ''); // not markdown
    const out = walkDocs(scratch, 'docs');
    expect(out).toContain('docs/a.md');
    expect(out).toContain('docs/sub/b.md');
    expect(out).not.toContain('docs/c.txt');
  });
});

// ─── ledger integration ──────────────────────────────────────────────────

describe('parseLedgerIds', () => {
  it("extracts ids from yaml-fenced entries", () => {
    const md = `# Ledger\n\n\`\`\`yaml\nid: stale-doc-foo-1\nseverity: low\n\`\`\`\n\n\`\`\`yaml\nid: stale-doc-bar-2\n\`\`\`\n`;
    expect(parseLedgerIds(md)).toEqual(new Set(['stale-doc-foo-1', 'stale-doc-bar-2']));
  });

  it('returns empty for ledger without ids', () => {
    expect(parseLedgerIds('# Ledger\n\nintro\n')).toEqual(new Set());
  });
});

describe('staleRefId', () => {
  function makeStale(overrides: Partial<StaleRef> = {}): StaleRef {
    return {
      doc: 'docs/x.md',
      line: 5,
      ref: 'apps/foo.ts',
      context: 'See apps/foo.ts',
      ...overrides,
    };
  }

  it('produces stable ids — same input = same id', () => {
    const a = makeStale();
    expect(staleRefId(a)).toBe(staleRefId(a));
  });

  it('produces different ids for different doc/ref/context', () => {
    const base = makeStale();
    expect(staleRefId(base)).not.toBe(staleRefId({ ...base, doc: 'docs/y.md' }));
    expect(staleRefId(base)).not.toBe(staleRefId({ ...base, ref: 'apps/bar.ts' }));
    expect(staleRefId(base)).not.toBe(staleRefId({ ...base, context: 'different' }));
  });

  it("matches the canonical id format `stale-doc-<docslug>-<refslug>-<sha8>`", () => {
    expect(staleRefId(makeStale())).toMatch(/^stale-doc-[a-z0-9-]+-[a-f0-9]{8}$/);
  });
});

describe('renderEntry', () => {
  it('renders a yaml-fenced entry with category:doc-debt + severity:low', () => {
    const out = renderEntry(
      { doc: 'docs/a.md', line: 5, ref: 'apps/gone.ts', context: 'see apps/gone.ts' },
      'stale-doc-x',
      '2026-05-02T18:00:00Z',
    );
    expect(out).toContain('id: stale-doc-x');
    expect(out).toContain('category: doc-debt');
    expect(out).toContain('severity: low');
    expect(out).toContain('docs/a.md');
    expect(out).toContain('apps/gone.ts');
  });
});

// ─── runStaleDocDetector ─────────────────────────────────────────────────

describe('runStaleDocDetector', () => {
  let scratch: string;
  let ledgerPath: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'stale-doc-runner-test-'));
    mkdirSync(join(scratch, 'docs'));
    mkdirSync(join(scratch, 'apps'));
    ledgerPath = join(scratch, 'docs/debt-ledger.md');
    writeFileSync(ledgerPath, '# Debt Ledger\n\nintro\n', 'utf8');
    logs = [];
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('files an entry per stale ref it finds', async () => {
    writeFileSync(
      join(scratch, 'docs/a.md'),
      'The fresh ref is `apps/exists.ts`.\nA broken ref: `apps/gone.ts`.\n',
      'utf8',
    );
    writeFileSync(join(scratch, 'apps/exists.ts'), '');

    const r = await runStaleDocDetector({
      repoRoot: scratch,
      docsRoot: 'docs',
      ledgerPath,
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.stale_refs).toBe(1);
    expect(r.new_entries).toBe(1);
    const md = readFileSync(ledgerPath, 'utf8');
    // The stale ref's path appears in the entry; the existing ref's
    // path does not (split across two lines so the context excerpts
    // don't bleed).
    expect(md).toMatch(/Reference: apps\/gone\.ts/);
    expect(md).not.toMatch(/Reference: apps\/exists\.ts/);
  });

  it('idempotent — re-run on same input adds zero', async () => {
    writeFileSync(join(scratch, 'docs/a.md'), 'See `apps/gone.ts`\n', 'utf8');
    const opts = {
      repoRoot: scratch,
      docsRoot: 'docs',
      ledgerPath,
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      log: (l: string) => logs.push(l),
    };
    const r1 = await runStaleDocDetector(opts);
    const r2 = await runStaleDocDetector(opts);
    expect(r1.new_entries).toBe(1);
    expect(r2.new_entries).toBe(0);
    expect(r2.duplicates_skipped).toBe(1);
  });

  it('caps entries written per run', async () => {
    const refs = Array.from({ length: 50 }, (_, i) => `\`apps/gone-${i}.ts\``);
    writeFileSync(join(scratch, 'docs/a.md'), refs.join('\n') + '\n', 'utf8');

    const r = await runStaleDocDetector({
      repoRoot: scratch,
      docsRoot: 'docs',
      ledgerPath,
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(5);
    expect(r.stale_refs).toBe(50);
  });

  it("does not touch the ledger when nothing is stale", async () => {
    mkdirSync(join(scratch, 'apps/sub'), { recursive: true });
    writeFileSync(join(scratch, 'apps/foo.ts'), '');
    writeFileSync(join(scratch, 'docs/a.md'), 'See `apps/foo.ts`.\n', 'utf8');

    const before = readFileSync(ledgerPath, 'utf8');
    const r = await runStaleDocDetector({
      repoRoot: scratch,
      docsRoot: 'docs',
      ledgerPath,
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(0);
    expect(readFileSync(ledgerPath, 'utf8')).toBe(before);
  });

  it("emits the canonical telemetry log line", async () => {
    writeFileSync(join(scratch, 'docs/a.md'), 'See `apps/gone.ts`\n', 'utf8');
    await runStaleDocDetector({
      repoRoot: scratch,
      docsRoot: 'docs',
      ledgerPath,
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      log: (l) => logs.push(l),
    });
    expect(logs).toHaveLength(1);
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('stale-doc-detector');
    expect(parsed.new_entries).toBe(1);
    expect(parsed.stale_refs).toBe(1);
  });
});

// ─── helpers (sanity) ────────────────────────────────────────────────────

describe('helpers', () => {
  it('shortContext caps at 80 chars with ellipsis', () => {
    const out = shortContext('x'.repeat(200));
    expect(out.length).toBeLessThanOrEqual(80);
    expect(out.endsWith('…')).toBe(true);
  });

  it('trimRefPunctuation strips trailing punctuation only', () => {
    expect(trimRefPunctuation('apps/foo.ts.')).toBe('apps/foo.ts');
    expect(trimRefPunctuation('apps/foo.ts,')).toBe('apps/foo.ts');
    expect(trimRefPunctuation('apps/foo.ts')).toBe('apps/foo.ts');
    expect(trimRefPunctuation('apps/foo.ts).')).toBe('apps/foo.ts');
  });
});
