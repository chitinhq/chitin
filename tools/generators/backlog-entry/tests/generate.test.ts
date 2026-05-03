import { describe, expect, it } from 'vitest';
import { writeFileSync, unlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { buildSection, insertEntry, hasDuplicateId, type EntryOptions } from '../generate.js';
import { parseBacklog } from '../../../../apps/temporal-worker/src/grooming/parse-backlog.js';

const BASE_OPTS: EntryOptions = {
  id: 'my-new-feature',
  tier: 'T1',
  status: 'ready',
  role: 'programmer',
  file: 'apps/temporal-worker/src/foo.ts',
  blocks: [],
  estimated_loc: '80',
  references_finding: '',
  references_spec: '',
  references_design: '',
  description: 'Adds the foo capability.',
};

// ── buildSection ──────────────────────────────────────────────────────────

describe('buildSection', () => {
  it('heading id matches frontmatter id', () => {
    const section = buildSection(BASE_OPTS);
    expect(section).toContain('### `my-new-feature`');
    expect(section).toContain('id: my-new-feature');
  });

  it('emits required yaml fields', () => {
    const section = buildSection(BASE_OPTS);
    expect(section).toContain('tier: T1');
    expect(section).toContain('status: ready');
    expect(section).toContain('role: programmer');
    expect(section).toContain('blocks: []');
  });

  it('emits optional fields when set', () => {
    const section = buildSection({
      ...BASE_OPTS,
      estimated_loc: '120',
      references_finding: 'some-finding',
      references_spec: 'some-spec',
      references_design: 'docs/design/foo.md',
    });
    expect(section).toContain('estimated_loc: 120');
    expect(section).toContain('references_finding: some-finding');
    expect(section).toContain('references_spec: some-spec');
    expect(section).toContain('references_design: docs/design/foo.md');
  });

  it('omits optional fields when empty', () => {
    const section = buildSection(BASE_OPTS);
    expect(section).not.toContain('references_finding');
    expect(section).not.toContain('references_spec');
    expect(section).not.toContain('references_design');
  });

  it('blocks list serialised correctly', () => {
    const section = buildSection({ ...BASE_OPTS, blocks: ['dep-a', 'dep-b'] });
    expect(section).toContain('blocks: [dep-a, dep-b]');
  });

  it('uses (no description) placeholder when description is empty', () => {
    const section = buildSection({ ...BASE_OPTS, description: '' });
    expect(section).toContain('(no description)');
  });

  it('includes the description text', () => {
    const section = buildSection({ ...BASE_OPTS, description: 'My description line.' });
    expect(section).toContain('My description line.');
  });

  it('yaml block is fenced in ```yaml', () => {
    const section = buildSection(BASE_OPTS);
    expect(section).toContain('```yaml\n');
    expect(section).toMatch(/```yaml[\s\S]*?```/);
  });
});

// ── insertEntry ───────────────────────────────────────────────────────────

describe('insertEntry', () => {
  const existingBacklog = [
    '## Ready',
    '',
    '### `entry-a`',
    '',
    '```yaml',
    'id: entry-a',
    'tier: T1',
    'status: ready',
    'blocks: []',
    'role: programmer',
    '```',
    '',
    'Description of entry a.',
  ].join('\n');

  it('appends new entry after existing last ### entry', () => {
    const section = buildSection(BASE_OPTS);
    const result = insertEntry(existingBacklog, section);
    expect(result).toContain('### `entry-a`');
    expect(result).toContain('### `my-new-feature`');
    const aIdx = result.indexOf('### `entry-a`');
    const bIdx = result.indexOf('### `my-new-feature`');
    expect(bIdx).toBeGreaterThan(aIdx);
  });

  it('separates entries with --- divider', () => {
    const section = buildSection(BASE_OPTS);
    const result = insertEntry(existingBacklog, section);
    expect(result).toContain('\n\n---\n\n');
  });

  it('preserves trailing ## section when present', () => {
    const withTrailing = `${existingBacklog}\n\n## Completed\n\n### \`old-entry\`\n\nDone.\n`;
    const section = buildSection(BASE_OPTS);
    const result = insertEntry(withTrailing, section);
    // New entry should appear before ## Completed
    const newIdx = result.indexOf('### `my-new-feature`');
    const completedIdx = result.indexOf('## Completed');
    expect(newIdx).toBeLessThan(completedIdx);
  });

  it('appends correctly to empty text', () => {
    const section = buildSection(BASE_OPTS);
    const result = insertEntry('', section);
    expect(result).toContain('### `my-new-feature`');
  });
});

// ── hasDuplicateId ────────────────────────────────────────────────────────

describe('hasDuplicateId', () => {
  it('returns false when id is not present', () => {
    const entries = parseBacklog(
      writeTempBacklog([
        '## Ready',
        '',
        '### `existing-entry`',
        '',
        '```yaml',
        'id: existing-entry',
        'tier: T1',
        'status: ready',
        'blocks: []',
        'role: programmer',
        '```',
        '',
        'Some description.',
      ].join('\n')),
    );
    expect(hasDuplicateId(entries, 'brand-new-id')).toBe(false);
  });

  it('returns true when id already exists', () => {
    const entries = parseBacklog(
      writeTempBacklog([
        '## Ready',
        '',
        '### `existing-entry`',
        '',
        '```yaml',
        'id: existing-entry',
        'tier: T1',
        'status: ready',
        'blocks: []',
        'role: programmer',
        '```',
        '',
        'Some description.',
      ].join('\n')),
    );
    expect(hasDuplicateId(entries, 'existing-entry')).toBe(true);
  });
});

// ── round-trip integration ────────────────────────────────────────────────

describe('round-trip through parseBacklog', () => {
  it('generated entry is parseable and heading id matches frontmatter id', () => {
    const section = buildSection(BASE_OPTS);
    const backlogText = [
      '## Ready',
      '',
      '### `seed-entry`',
      '',
      '```yaml',
      'id: seed-entry',
      'tier: T0',
      'status: ready',
      'blocks: []',
      'role: programmer',
      '```',
      '',
      'Seed.',
    ].join('\n');
    const updated = insertEntry(backlogText, section);
    const path = writeTempBacklog(updated);

    const entries = parseBacklog(path);
    const entry = entries.find((e) => e.id === 'my-new-feature');
    expect(entry).toBeDefined();

    // Heading id must match frontmatter id
    const fmIdMatch = entry!.rawFrontmatter.match(/^id:\s*(\S+)/m);
    expect(fmIdMatch?.[1]).toBe(entry!.id);
  });

  it('refuses to insert a duplicate id (hasDuplicateId check)', () => {
    const backlogText = [
      '## Ready',
      '',
      '### `my-new-feature`',
      '',
      '```yaml',
      'id: my-new-feature',
      'tier: T1',
      'status: ready',
      'blocks: []',
      'role: programmer',
      '```',
      '',
      'Already exists.',
    ].join('\n');
    const path = writeTempBacklog(backlogText);
    const entries = parseBacklog(path);
    expect(hasDuplicateId(entries, 'my-new-feature')).toBe(true);
  });

  it('blocks field round-trips as an array', () => {
    const section = buildSection({ ...BASE_OPTS, blocks: ['dep-x', 'dep-y'] });
    const backlogText = '## Ready\n';
    const updated = insertEntry(backlogText, section);
    const path = writeTempBacklog(updated);

    const entries = parseBacklog(path);
    const entry = entries.find((e) => e.id === 'my-new-feature');
    expect(entry?.blocks).toEqual(['dep-x', 'dep-y']);
  });
});

// ── helpers ───────────────────────────────────────────────────────────────

const tmpFiles: string[] = [];

function writeTempBacklog(content: string): string {
  const path = join(tmpdir(), `test-backlog-${Date.now()}-${Math.random().toString(36).slice(2)}.md`);
  writeFileSync(path, content, 'utf8');
  tmpFiles.push(path);
  return path;
}

// Cleanup after all tests
import { afterAll } from 'vitest';
afterAll(() => {
  for (const f of tmpFiles) {
    try { unlinkSync(f); } catch { /* ignore */ }
  }
});
