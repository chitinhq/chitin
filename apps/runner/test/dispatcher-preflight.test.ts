import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { __test__ } from '../src/dispatcher.ts';

const { findClaudeBackupArtifact } = __test__;

describe('findClaudeBackupArtifact', () => {
  let scratch: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'chitin-preflight-test-'));
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns null when there is no .claude directory', () => {
    expect(findClaudeBackupArtifact(scratch)).toBeNull();
  });

  it('returns null when .claude exists but has no backup files', () => {
    mkdirSync(join(scratch, '.claude'));
    writeFileSync(join(scratch, '.claude', 'settings.json'), '{}');
    expect(findClaudeBackupArtifact(scratch)).toBeNull();
  });

  it('returns the artifact path when a backup file is present', () => {
    mkdirSync(join(scratch, '.claude'));
    const artifactName = 'settings.json.chitin-backup-20260429T210349Z';
    writeFileSync(join(scratch, '.claude', artifactName), '{}');
    expect(findClaudeBackupArtifact(scratch)).toBe(join(scratch, '.claude', artifactName));
  });

  it('does not match settings.json itself or a similar but distinct name', () => {
    mkdirSync(join(scratch, '.claude'));
    writeFileSync(join(scratch, '.claude', 'settings.json'), '{}');
    writeFileSync(join(scratch, '.claude', 'settings.json.bak'), '{}');
    writeFileSync(join(scratch, '.claude', 'chitin-backup-20260429T210349Z'), '{}');
    expect(findClaudeBackupArtifact(scratch)).toBeNull();
  });

  it('requires a non-empty suffix after chitin-backup-', () => {
    // Defensive: the kernel's installer always appends a timestamp, but a
    // truncated `settings.json.chitin-backup-` (trailing dash, no payload)
    // is suspicious and should still trip the preflight.
    mkdirSync(join(scratch, '.claude'));
    writeFileSync(join(scratch, '.claude', 'settings.json.chitin-backup-'), '{}');
    expect(findClaudeBackupArtifact(scratch)).toBeNull();
    // A single trailing character DOES count as a backup.
    writeFileSync(join(scratch, '.claude', 'settings.json.chitin-backup-x'), '{}');
    expect(findClaudeBackupArtifact(scratch)).toBe(
      join(scratch, '.claude', 'settings.json.chitin-backup-x'),
    );
  });

  it('returns the first backup found if multiple are present', () => {
    mkdirSync(join(scratch, '.claude'));
    const a = 'settings.json.chitin-backup-20260101T000000Z';
    const b = 'settings.json.chitin-backup-20260102T000000Z';
    writeFileSync(join(scratch, '.claude', a), '{}');
    writeFileSync(join(scratch, '.claude', b), '{}');
    const found = findClaudeBackupArtifact(scratch);
    expect(found).not.toBeNull();
    expect([join(scratch, '.claude', a), join(scratch, '.claude', b)]).toContain(found);
  });
});
