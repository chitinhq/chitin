// Tests for @chitin/router-plugin-api gateAction() — exercises the
// branches Copilot flagged as untested: deny parsing, timeout
// fail-open, missing-binary fail-open, non-JSON fallback,
// require_policy flag pass-through.
//
// We use small shell shims for `kernelBinary` so we can pin exit
// code + stdout + behavior deterministically without spinning up a
// real chitin-kernel.

import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { gateAction, GateBlocked } from './index.ts';

function makeShim(stdout: string, exitCode: number, extraScript = ''): string {
  const dir = mkdtempSync(join(tmpdir(), 'gate-shim-'));
  const path = join(dir, 'fake-kernel');
  writeFileSync(
    path,
    `#!/bin/bash
${extraScript}
echo -n '${stdout.replace(/'/g, "'\\''")}'
exit ${exitCode}
`,
  );
  chmodSync(path, 0o755);
  return path;
}

describe('gateAction', () => {
  it('parses an allow decision (exit 0, JSON allow)', async () => {
    const bin = makeShim(JSON.stringify({ decision: 'allow' }), 0);
    const d = await gateAction({
      toolName: 'Read',
      toolInput: { file_path: '/tmp/x' },
      kernelBinary: bin,
    });
    expect(d.allowed).toBe(true);
  });

  it('treats empty stdout + exit 0 as silent allow', async () => {
    const bin = makeShim('', 0);
    const d = await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: bin,
    });
    expect(d.allowed).toBe(true);
  });

  it('parses a block decision and throws GateBlocked by default', async () => {
    const bin = makeShim(JSON.stringify({ decision: 'block', reason: 'no rm' }), 2);
    await expect(
      gateAction({
        toolName: 'Bash',
        toolInput: { command: 'rm -rf /' },
        kernelBinary: bin,
      }),
    ).rejects.toThrow(GateBlocked);
  });

  it('returns deny instead of throwing when raiseOnDeny=false', async () => {
    const bin = makeShim(JSON.stringify({ decision: 'block', reason: 'nope' }), 2);
    const d = await gateAction({
      toolName: 'Bash',
      toolInput: { command: 'rm -rf /' },
      kernelBinary: bin,
      raiseOnDeny: false,
    });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe('nope');
  });

  it('falls open when kernel binary is missing (ENOENT)', async () => {
    const d = await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: '/nonexistent/path/never/exists',
    });
    expect(d.allowed).toBe(true);
    expect(d.reason).toBe('kernel-missing-fail-open');
  });

  it('falls open when kernel times out', async () => {
    // Shim sleeps longer than timeoutMs.
    const bin = makeShim('', 0, 'sleep 10');
    const d = await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: bin,
      timeoutMs: 100,
    });
    expect(d.allowed).toBe(true);
    expect(d.reason).toBe('kernel-timeout-fail-open');
  });

  it('falls open when stdout is malformed JSON', async () => {
    const bin = makeShim('{not json', 0);
    const d = await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: bin,
    });
    expect(d.allowed).toBe(true);
    expect(d.reason).toBe('kernel-non-json-fail-open');
  });

  it('passes --require-policy when requirePolicy=true', async () => {
    // Shim that echoes its argv to stderr so we can introspect.
    const dir = mkdtempSync(join(tmpdir(), 'gate-rp-'));
    const argLog = join(dir, 'argv.log');
    const bin = join(dir, 'fake-kernel');
    writeFileSync(
      bin,
      `#!/bin/bash
echo "$@" > ${argLog}
echo '{"decision":"allow"}'
exit 0
`,
    );
    chmodSync(bin, 0o755);
    await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: bin,
      requirePolicy: true,
    });
    const argv = (await import('node:fs')).readFileSync(argLog, 'utf8');
    expect(argv).toContain('--require-policy');
  });

  it('omits --require-policy by default', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'gate-rp-default-'));
    const argLog = join(dir, 'argv.log');
    const bin = join(dir, 'fake-kernel');
    writeFileSync(
      bin,
      `#!/bin/bash
echo "$@" > ${argLog}
echo '{"decision":"allow"}'
exit 0
`,
    );
    chmodSync(bin, 0o755);
    await gateAction({
      toolName: 'Read',
      toolInput: {},
      kernelBinary: bin,
    });
    const argv = (await import('node:fs')).readFileSync(argLog, 'utf8');
    expect(argv).not.toContain('--require-policy');
  });
});
