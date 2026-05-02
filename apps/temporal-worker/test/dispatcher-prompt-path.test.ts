import { buildPrompt } from '../src/dispatcher.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';
import { describe, it, expect } from 'vitest';

function makeEntry(overrides: Partial<BacklogEntry>): BacklogEntry {
  return {
    id: 'test-entry',
    status: 'ready',
    description: 'desc',
    rawFrontmatter: '```yaml\nid: test-entry\nstatus: ready\n```',
    rawSection: '### `test-entry`\n\n```yaml\nid: test-entry\nstatus: ready\n```\n\ndesc\n',
    ...overrides,
  };
}

describe('buildPrompt', () => {
  it('prepends ./ to relative target file paths', () => {
    const prompt = buildPrompt(makeEntry({ file: 'apps/foo/bar.ts' }));
    expect(prompt).toContain('TARGET FILE: ./apps/foo/bar.ts');
    expect(prompt).toContain('dispatch the `read` tool on ./apps/foo/bar.ts');
    expect(prompt).toContain('Start by reading ./apps/foo/bar.ts now.');
  });

  it('does not prepend ./ if already present', () => {
    const prompt = buildPrompt(makeEntry({ file: './apps/foo/bar.ts' }));
    expect(prompt).toContain('TARGET FILE: ./apps/foo/bar.ts');
  });

  it('does not prepend ./ if absolute path', () => {
    const prompt = buildPrompt(makeEntry({ file: '/apps/foo/bar.ts' }));
    expect(prompt).toContain('TARGET FILE: /apps/foo/bar.ts');
  });

  it('does not prepend ./ to the placeholder when entry.file is missing', () => {
    const prompt = buildPrompt(makeEntry({ file: undefined }));
    expect(prompt).toContain('TARGET FILE: the file named in the entry');
    expect(prompt).not.toContain('TARGET FILE: ./the file named in the entry');
  });
});
