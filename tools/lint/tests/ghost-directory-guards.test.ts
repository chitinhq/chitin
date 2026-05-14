import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const repoRoot = resolve(import.meta.dirname, '../../..');

describe('historical ghost directory guards', () => {
  const removedDirs = [
    'apps/mcp-server',
    'apps/runner',
    'apps/slack-app',
    'libs/governance',
    'libs/scheduler',
  ];

  for (const relPath of removedDirs) {
    it(`${relPath} stays deleted`, () => {
      expect(existsSync(resolve(repoRoot, relPath))).toBe(false);
    });
  }
});
