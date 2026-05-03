import { mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { KernelError, parseKernelJSON, runKernel } from '../src/kernel.js';
import { decisionsRecentTool } from '../src/tools/decisions-recent.js';
import { envelopeGrantTool } from '../src/tools/envelope-grant.js';

// ── kernel utilities ──────────────────────────────────────────────────────────

describe('runKernel', () => {
  it('returns nonzero status when binary is missing', () => {
    process.env['CHITIN_KERNEL_BINARY'] = '/nonexistent-binary-xyz';
    const result = runKernel(['anything']);
    expect(result.error).toBeDefined();
    delete process.env['CHITIN_KERNEL_BINARY'];
  });
});

describe('parseKernelJSON', () => {
  it('throws KernelError with kind=spawn_failed when binary is missing', () => {
    const result = runKernel(['anything']);
    // Simulate a missing binary result
    expect(() =>
      parseKernelJSON({ stdout: '', stderr: '', status: -1, error: new Error('ENOENT') }),
    ).toThrowError(KernelError);
  });

  it('throws KernelError on nonzero exit', () => {
    expect(() =>
      parseKernelJSON({ stdout: '{"error":"test_err","message":"boom"}', stderr: '', status: 1 }),
    ).toThrowError(KernelError);
  });

  it('throws KernelError when stdout is not JSON', () => {
    expect(() =>
      parseKernelJSON({ stdout: 'not json', stderr: '', status: 0 }),
    ).toThrowError(KernelError);
  });

  it('parses valid JSON output', () => {
    const result = parseKernelJSON<{ ok: boolean }>({ stdout: '{"ok":true}', stderr: '', status: 0 });
    expect(result.ok).toBe(true);
  });

  it('extracts error kind from stderr JSON', () => {
    let caught: KernelError | undefined;
    try {
      parseKernelJSON({ stdout: '', stderr: '{"error":"envelope_not_found","message":"x"}', status: 1 });
    } catch (e) {
      caught = e as KernelError;
    }
    expect(caught).toBeInstanceOf(KernelError);
    expect(caught?.kind).toBe('envelope_not_found');
  });
});

// ── envelopeGrantTool argument validation ────────────────────────────────────

describe('envelopeGrantTool', () => {
  it('throws KernelError when chitin-kernel is missing', () => {
    process.env['CHITIN_KERNEL_BINARY'] = '/nonexistent-binary-xyz';
    expect(() => envelopeGrantTool({ id: 'test-id', calls: 10 })).toThrowError(KernelError);
    delete process.env['CHITIN_KERNEL_BINARY'];
  });
});

// ── decisionsRecentTool file reading ─────────────────────────────────────────

describe('decisionsRecentTool', () => {
  let dir: string;

  beforeEach(() => {
    dir = join(tmpdir(), `chitin-test-${Date.now()}`);
    mkdirSync(dir, { recursive: true });
  });

  afterEach(() => {
    // no cleanup needed — tmp files age out
  });

  function makeDecision(ts: string, allowed = true) {
    return JSON.stringify({
      allowed,
      mode: 'enforce',
      rule_id: 'test',
      agent: 'claude-code',
      action_type: 'write_file',
      action_target: '/tmp/x',
      ts,
    });
  }

  it('returns empty array when dir has no decision files', () => {
    const result = decisionsRecentTool({ dir });
    expect(result).toEqual([]);
  });

  it('returns empty array when dir does not exist', () => {
    const result = decisionsRecentTool({ dir: '/nonexistent-chitin-dir-xyz' });
    expect(result).toEqual([]);
  });

  it('reads decisions from JSONL files within the window', () => {
    const recent = new Date(Date.now() - 60_000).toISOString();
    const content = makeDecision(recent) + '\n';
    writeFileSync(join(dir, `gov-decisions-${recent.slice(0, 10)}.jsonl`), content);

    const result = decisionsRecentTool({ dir, windowHours: 1 });
    expect(result).toHaveLength(1);
    expect(result[0]!.action_type).toBe('write_file');
  });

  it('excludes decisions outside the window', () => {
    const old = new Date(Date.now() - 48 * 3_600_000).toISOString();
    const content = makeDecision(old) + '\n';
    writeFileSync(join(dir, `gov-decisions-${old.slice(0, 10)}.jsonl`), content);

    const result = decisionsRecentTool({ dir, windowHours: 1 });
    expect(result).toHaveLength(0);
  });

  it('respects the limit parameter', () => {
    const lines = Array.from({ length: 10 }, (_, i) => {
      const ts = new Date(Date.now() - i * 60_000).toISOString();
      return makeDecision(ts);
    });
    const today = new Date().toISOString().slice(0, 10);
    writeFileSync(join(dir, `gov-decisions-${today}.jsonl`), lines.join('\n') + '\n');

    const result = decisionsRecentTool({ dir, limit: 3 });
    expect(result).toHaveLength(3);
  });

  it('skips malformed JSON lines without throwing', () => {
    const recent = new Date(Date.now() - 60_000).toISOString();
    const today = new Date().toISOString().slice(0, 10);
    const content = `{not valid json}\n${makeDecision(recent)}\n`;
    writeFileSync(join(dir, `gov-decisions-${today}.jsonl`), content);

    const result = decisionsRecentTool({ dir, windowHours: 1 });
    expect(result).toHaveLength(1);
  });
});
