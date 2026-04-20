import {
  mkdtempSync,
  mkdirSync,
  existsSync,
  readFileSync,
  readdirSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect } from 'vitest';

const binEntry = join(__dirname, '..', 'bin', 'cli.ts');
const repoRoot = join(__dirname, '..', '..', '..', '..');
const kernelBin = join(repoRoot, 'dist/go/execution-kernel/chitin-kernel');
const tsxBin = join(repoRoot, 'node_modules/.bin/tsx');

describe('claude-code adapter bin/cli', () => {
  it('emits an event carrying the input session_id (PR #19 end-to-end guard)', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping: ${kernelBin} missing. Run: pnpm nx build execution-kernel`);
      return;
    }
    const workspace = mkdtempSync(join(tmpdir(), 'adp-cc-'));
    mkdirSync(join(workspace, '.chitin'));
    const cwd = join(workspace, 'a', 'b');
    mkdirSync(cwd, { recursive: true });

    const ccSessionID = '505c4216-bc0a-49d1-b512-55df4d6563c0';
    const hookInput = JSON.stringify({
      hook_event_name: 'SessionStart',
      session_id: ccSessionID,
      cwd,
    });

    const res = spawnSync(tsxBin, [binEntry], {
      input: hookInput,
      cwd,
      encoding: 'utf8',
      env: { ...process.env, CHITIN_KERNEL_BINARY: kernelBin },
    });
    expect(res.status).toBe(0);

    const chitinDir = join(workspace, '.chitin');
    const jsonlFiles = readdirSync(chitinDir).filter(
      (f) => f.startsWith('events-') && f.endsWith('.jsonl'),
    );
    expect(jsonlFiles.length).toBeGreaterThan(0);
    const jsonl = join(chitinDir, jsonlFiles[0]);
    const lines = readFileSync(jsonl, 'utf8').trim().split('\n').filter(Boolean);
    expect(lines.length).toBeGreaterThan(0);
    const evt = JSON.parse(lines[lines.length - 1]);
    expect(evt.session_id).toBe(ccSessionID);
    expect(evt.event_type).toBe('session_start');
    expect(evt.chain_id).toBe(ccSessionID);
  });

  it('exits 0 on empty stdin (never breaks the session)', () => {
    const res = spawnSync(tsxBin, [binEntry], {
      input: '',
      encoding: 'utf8',
      env: { ...process.env, CHITIN_KERNEL_BINARY: 'chitin-kernel' },
    });
    expect(res.status).toBe(0);
  });

  it('exits 0 on malformed JSON (never breaks the session)', () => {
    const res = spawnSync(tsxBin, [binEntry], {
      input: '{not json',
      encoding: 'utf8',
      env: { ...process.env, CHITIN_KERNEL_BINARY: 'chitin-kernel' },
    });
    expect(res.status).toBe(0);
  });

  it('exits 0 on input without session_id (logs and skips emit)', () => {
    const res = spawnSync(tsxBin, [binEntry], {
      input: JSON.stringify({ hook_event_name: 'SessionStart' }),
      encoding: 'utf8',
      env: { ...process.env, CHITIN_KERNEL_BINARY: 'chitin-kernel' },
    });
    expect(res.status).toBe(0);
  });
});
