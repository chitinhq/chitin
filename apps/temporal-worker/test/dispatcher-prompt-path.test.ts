import { buildPrompt } from '../src/dispatcher';
import { describe, it, expect } from 'vitest';

describe('buildPrompt', () => {
  it('prepends ./ to relative target file paths', () => {
    const entry = {
      id: 'test-entry',
      file: 'apps/foo/bar.ts',
      description: 'desc',
    };
    const prompt = buildPrompt(entry as any);
    expect(prompt).toContain('TARGET FILE: ./apps/foo/bar.ts');
    expect(prompt).toContain('dispatch the `read` tool on ./apps/foo/bar.ts');
    expect(prompt).toContain('Start by reading ./apps/foo/bar.ts now.');
  });

  it('does not prepend ./ if already present', () => {
    const entry = {
      id: 'test-entry',
      file: './apps/foo/bar.ts',
      description: 'desc',
    };
    const prompt = buildPrompt(entry as any);
    expect(prompt).toContain('TARGET FILE: ./apps/foo/bar.ts');
  });

  it('does not prepend ./ if absolute path', () => {
    const entry = {
      id: 'test-entry',
      file: '/apps/foo/bar.ts',
      description: 'desc',
    };
    const prompt = buildPrompt(entry as any);
    expect(prompt).toContain('TARGET FILE: /apps/foo/bar.ts');
  });
});
