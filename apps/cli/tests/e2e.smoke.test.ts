import { describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const kernelBinary = process.env.CHITIN_KERNEL_BINARY
  ?? join(process.cwd(), 'go/execution-kernel/chitin-kernel');

function runKernel(args: string[], opts: { cwd?: string } = {}): string {
  const res = spawnSync(kernelBinary, args, { encoding: 'utf8', cwd: opts.cwd });
  if (res.status !== 0) throw new Error(`kernel failed: ${res.stderr}`);
  return res.stdout;
}

describe('e2e: emit chain + verify hash linkage', () => {
  it('two events in one chain have correct seq and prev_hash linkage', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-e2e-'));
    const chitinDir = join(dir, '.chitin');
    runKernel(['init', '--dir', chitinDir]);

    const baseEnv = {
      schema_version: '2',
      run_id: '550e8400-e29b-41d4-a716-446655440000',
      session_id: '550e8400-e29b-41d4-a716-446655440001',
      surface: 'claude-code',
      driver_identity: { user: 'u', machine_id: 'm', machine_fingerprint: 'a'.repeat(64) },
      agent_instance_id: '550e8400-e29b-41d4-a716-446655440002',
      parent_agent_id: null,
      agent_fingerprint: 'b'.repeat(64),
      chain_id: 'chain-A',
      chain_type: 'session',
      parent_chain_id: null,
      seq: 0,
      prev_hash: null,
      this_hash: '',
      ts: '2026-04-19T12:00:00.000Z',
      labels: {},
    };

    const ev1 = {
      ...baseEnv,
      event_type: 'session_start',
      payload: {
        cwd: '/', client_info: { name: 'claude-code', version: '1' },
        model: { name: 'x', provider: 'y' },
        system_prompt_hash: '0'.repeat(64),
        tool_allowlist_hash: '0'.repeat(64),
        agent_version: '1',
      },
    };
    const ev2 = { ...baseEnv, event_type: 'user_prompt', payload: { text: 'hi' } };

    const p1 = join(dir, 'ev1.json');
    const p2 = join(dir, 'ev2.json');
    writeFileSync(p1, JSON.stringify(ev1));
    writeFileSync(p2, JSON.stringify(ev2));
    runKernel(['emit', '--dir', chitinDir, '--event-file', p1]);
    runKernel(['emit', '--dir', chitinDir, '--event-file', p2]);

    const jsonls = readdirSync(chitinDir).filter((f) => f.startsWith('events-') && f.endsWith('.jsonl'));
    expect(jsonls.length).toBe(1);
    const lines = readFileSync(join(chitinDir, jsonls[0]), 'utf8')
      .trim()
      .split('\n')
      .map((l) => JSON.parse(l));
    expect(lines.length).toBe(2);
    expect(lines[0].seq).toBe(0);
    expect(lines[0].prev_hash).toBeNull();
    expect(lines[1].seq).toBe(1);
    expect(lines[1].prev_hash).toBe(lines[0].this_hash);
    expect(lines[0].this_hash).toMatch(/^[a-f0-9]{64}$/);
    expect(lines[1].this_hash).toMatch(/^[a-f0-9]{64}$/);
  });
});
