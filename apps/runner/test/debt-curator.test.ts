import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  appendEntries,
  candidateId,
  classifyCategory,
  parseGitGrepOutput,
  parseLedgerIds,
  renderEntry,
  runDebtCurator,
  __test__,
  type DebtCandidateInput,
  type DebtEntry,
} from '../src/debt-curator.ts';

const { SCAN_MARKERS } = __test__;

function makeCandidate(overrides: Partial<DebtCandidateInput> = {}): DebtCandidateInput {
  return {
    file: 'apps/foo.ts',
    line: 42,
    marker: 'TODO',
    context: 'extract this into a helper',
    ...overrides,
  };
}

function makeEntry(overrides: Partial<DebtEntry> = {}): DebtEntry {
  return {
    id: 'todo-foo-deadbeef',
    discovered_at: '2026-05-02T18:00:00Z',
    discovered_by: 'swarm',
    severity: 'low',
    category: 'code-debt',
    file: 'apps/foo.ts',
    status: 'open',
    description: 'Auto-detected from TODO marker.',
    ...overrides,
  };
}

// ─── parseLedgerIds ──────────────────────────────────────────────────────

describe('parseLedgerIds', () => {
  it('returns empty set for an empty ledger', () => {
    expect(parseLedgerIds('# Debt Ledger\n\nIntro.\n')).toEqual(new Set());
  });

  it('extracts ids from yaml-fenced entry blocks', () => {
    const md = `# Debt Ledger\n\n\`\`\`yaml\nid: load-marker-duplication\nseverity: medium\n\`\`\`\n\n\`\`\`yaml\nid: writeworktree-overwrite\nseverity: high\n\`\`\`\n`;
    expect(parseLedgerIds(md)).toEqual(new Set(['load-marker-duplication', 'writeworktree-overwrite']));
  });

  it('skips blocks without an id field', () => {
    const md = `\`\`\`yaml\nseverity: low\n\`\`\`\n\n\`\`\`yaml\nid: real-one\n\`\`\`\n`;
    expect(parseLedgerIds(md)).toEqual(new Set(['real-one']));
  });
});

// ─── candidateId ─────────────────────────────────────────────────────────

describe('candidateId', () => {
  it('generates stable ids — same input → same id', () => {
    const c = makeCandidate();
    expect(candidateId(c)).toBe(candidateId(c));
  });

  it('generates different ids for different files (same context)', () => {
    expect(candidateId(makeCandidate({ file: 'a.ts' }))).not.toBe(
      candidateId(makeCandidate({ file: 'b.ts' })),
    );
  });

  it('generates different ids for different contexts (same file)', () => {
    expect(candidateId(makeCandidate({ context: 'one thing' }))).not.toBe(
      candidateId(makeCandidate({ context: 'another thing' })),
    );
  });

  it('generates different ids for different markers (same file + context)', () => {
    expect(candidateId(makeCandidate({ marker: 'TODO' }))).not.toBe(
      candidateId(makeCandidate({ marker: 'FIXME' })),
    );
  });

  it('produces an id with the marker as a prefix', () => {
    expect(candidateId(makeCandidate({ marker: 'TODO' }))).toMatch(/^todo-/);
    expect(candidateId(makeCandidate({ marker: 'FIXME' }))).toMatch(/^fixme-/);
  });

  it('slugifies the file path safely (no special chars in id)', () => {
    const id = candidateId(makeCandidate({ file: 'apps/runner/src/foo.ts' }));
    expect(id).toMatch(/^[a-z0-9-]+$/);
  });
});

// ─── parseGitGrepOutput ──────────────────────────────────────────────────

describe('parseGitGrepOutput', () => {
  it('parses a typical git grep -n line', () => {
    const raw = `apps/foo.ts:42:  // TODO: extract helper\nlibs/bar.ts:7:  /* FIXME bad logic */\n`;
    const cs = parseGitGrepOutput(raw);
    expect(cs).toHaveLength(2);
    expect(cs[0]).toMatchObject({
      file: 'apps/foo.ts',
      line: 42,
      marker: 'TODO',
    });
    expect(cs[0].context).toContain('extract helper');
    expect(cs[1].marker).toBe('FIXME');
  });

  it('skips lines without a marker (defensive against grep glob noise)', () => {
    const raw = `apps/foo.ts:1:plain line, no marker\n`;
    expect(parseGitGrepOutput(raw)).toHaveLength(0);
  });

  it('skips lines that do not match the path:line:body shape', () => {
    expect(parseGitGrepOutput('not-a-grep-line\n')).toHaveLength(0);
  });

  it('caps context length so a long line does not blow up the entry', () => {
    const long = 'x'.repeat(500);
    const raw = `apps/foo.ts:42:  // TODO: ${long}\n`;
    const cs = parseGitGrepOutput(raw);
    expect(cs[0].context.length).toBeLessThanOrEqual(240);
  });

  it("uses '<marker> (no context)' when the marker has no trailing text", () => {
    const raw = `apps/foo.ts:42://TODO\n`;
    const cs = parseGitGrepOutput(raw);
    expect(cs[0].context).toContain('no context');
  });
});

// ─── classifyCategory ────────────────────────────────────────────────────

describe('classifyCategory', () => {
  it('classifies docs/* and *.md as doc-debt', () => {
    expect(classifyCategory('docs/foo.md')).toBe('doc-debt');
    expect(classifyCategory('README.md')).toBe('doc-debt');
  });

  it('classifies infra/* and yaml configs as infra-debt', () => {
    expect(classifyCategory('infra/systemd/foo.service')).toBe('infra-debt');
    expect(classifyCategory('config.yaml')).toBe('infra-debt');
  });

  it('classifies go-kernel governance paths as governance-debt', () => {
    expect(classifyCategory('go/execution-kernel/internal/gov/normalize.go')).toBe('governance-debt');
    expect(classifyCategory('chitin.yaml')).toBe('governance-debt');
  });

  it('falls back to code-debt for everything else', () => {
    expect(classifyCategory('apps/runner/src/foo.ts')).toBe('code-debt');
    expect(classifyCategory('libs/contracts/src/bar.ts')).toBe('code-debt');
  });
});

// ─── renderEntry / appendEntries ─────────────────────────────────────────

describe('renderEntry', () => {
  it('renders a yaml-fenced markdown block with all fields', () => {
    const out = renderEntry(makeEntry());
    expect(out).toContain('```yaml');
    expect(out).toContain('id: todo-foo-deadbeef');
    expect(out).toContain('severity: low');
    expect(out).toContain('category: code-debt');
  });

  it('emits shipped_in: <PR> when set; empty value otherwise', () => {
    expect(renderEntry(makeEntry({ shipped_in: '142' }))).toContain('shipped_in: 142');
    expect(renderEntry(makeEntry({ shipped_in: undefined }))).toMatch(/shipped_in:\s*$/m);
  });

  it('multi-line description renders with `description: |` block scalar', () => {
    const out = renderEntry(makeEntry({ description: 'line1\nline2' }));
    expect(out).toContain('description: |');
    expect(out).toContain('  line1');
    expect(out).toContain('  line2');
  });
});

describe('appendEntries', () => {
  it('returns input unchanged when entries is empty', () => {
    const md = '# x\n';
    expect(appendEntries(md, [])).toBe(md);
  });

  it('appends rendered blocks to the end of the doc', () => {
    const md = '# Debt Ledger\n\n## Entries\n\n(none yet)\n';
    const out = appendEntries(md, [makeEntry({ id: 'fresh-1' })]);
    expect(out).toContain('# Debt Ledger');
    expect(out).toContain('id: fresh-1');
    // New rendered block should be AFTER the existing prose.
    expect(out.indexOf('id: fresh-1')).toBeGreaterThan(out.indexOf('(none yet)'));
  });
});

// ─── runDebtCurator ──────────────────────────────────────────────────────

describe('runDebtCurator', () => {
  let scratch: string;
  let ledgerPath: string;
  let logs: string[];

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'debt-curator-test-'));
    ledgerPath = join(scratch, 'debt-ledger.md');
    logs = [];
    writeFileSync(
      ledgerPath,
      '# Debt Ledger\n\nThis file tracks known debt.\n\n## Entries\n\n',
      'utf8',
    );
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  function scanReturning(...candidates: DebtCandidateInput[]) {
    return () => candidates;
  }

  it('appends new entries when none exist', async () => {
    const r = await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(
        makeCandidate({ file: 'a.ts', context: 'one' }),
        makeCandidate({ file: 'b.ts', context: 'two' }),
      ),
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(2);
    expect(r.duplicates_skipped).toBe(0);
    const md = readFileSync(ledgerPath, 'utf8');
    expect(md).toContain('one');
    expect(md).toContain('two');
    expect(md).toContain('discovered_by: swarm');
    expect(md).toContain("severity: low");
    // Telemetry log line.
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('debt-curator');
    expect(parsed.new_entries).toBe(2);
  });

  it('dedups against ids already in the ledger', async () => {
    const c = makeCandidate({ file: 'a.ts', context: 'pre-existing' });
    const knownId = candidateId(c);
    writeFileSync(
      ledgerPath,
      `# Debt Ledger\n\n\`\`\`yaml\nid: ${knownId}\nseverity: low\n\`\`\`\n`,
      'utf8',
    );
    const r = await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(c),
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(0);
    expect(r.duplicates_skipped).toBe(1);
  });

  it('dedups within a single run when the same marker shows up twice', async () => {
    const c = makeCandidate({ file: 'a.ts', context: 'same context' });
    const r = await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(c, c),
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(1);
    expect(r.duplicates_skipped).toBe(1);
  });

  it('caps entries written per run', async () => {
    const candidates = Array.from({ length: 50 }, (_, i) =>
      makeCandidate({ file: `f${i}.ts`, context: `ctx ${i}` }),
    );
    const r = await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 5,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(...candidates),
      log: (l) => logs.push(l),
    });
    expect(r.new_entries).toBe(5);
    const md = readFileSync(ledgerPath, 'utf8');
    expect((md.match(/discovered_by: swarm/g) ?? []).length).toBe(5);
  });

  it('does not touch the file when nothing new is found', async () => {
    const before = readFileSync(ledgerPath, 'utf8');
    await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(),
      log: (l) => logs.push(l),
    });
    expect(readFileSync(ledgerPath, 'utf8')).toBe(before);
  });

  it('uses the injected `now` timestamp for new entries', async () => {
    const stableNow = '2026-04-01T00:00:00Z';
    await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: stableNow,
      scanForMarkers: scanReturning(makeCandidate()),
      log: (l) => logs.push(l),
    });
    const md = readFileSync(ledgerPath, 'utf8');
    expect(md).toContain(`discovered_at: ${stableNow}`);
  });

  it('classifies category based on file path', async () => {
    await runDebtCurator({
      ledgerPath,
      scanRoot: '.',
      cap: 10,
      now: '2026-05-02T18:00:00Z',
      scanForMarkers: scanReturning(
        makeCandidate({ file: 'docs/x.md', context: 'doc todo' }),
        makeCandidate({ file: 'infra/y.yaml', context: 'infra todo' }),
        makeCandidate({ file: 'apps/z.ts', context: 'code todo' }),
      ),
      log: (l) => logs.push(l),
    });
    const md = readFileSync(ledgerPath, 'utf8');
    expect(md).toContain('category: doc-debt');
    expect(md).toContain('category: infra-debt');
    expect(md).toContain('category: code-debt');
  });
});

// ─── Sanity ───────────────────────────────────────────────────────────────

describe('SCAN_MARKERS', () => {
  it("includes the canonical four markers", () => {
    expect(SCAN_MARKERS).toEqual(['TODO', 'FIXME', 'HACK', 'XXX']);
  });
});
