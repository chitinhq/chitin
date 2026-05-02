import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// @ts-expect-error — sibling .mjs without types
import { evaluateGate } from '../src/chitin-bridge.mjs';
// @ts-expect-error — sibling .mjs without types
import plugin from '../src/index.mjs';

function makeFakeKernel(scriptBody: string): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-bridge-test-'));
  const path = join(dir, 'fake-kernel');
  writeFileSync(path, `#!/usr/bin/env bash\n${scriptBody}\n`);
  chmodSync(path, 0o755);
  return { path, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

const baseInput = { agent: 'test', tool: 'shell.exec', params: { cmd: 'ls' }, cwd: '/tmp' };
const baseOpts = { kernelPath: 'chitin-kernel', timeoutMs: 2000, denyOnError: true };

describe('evaluateGate (bridge → chitin-kernel subprocess)', () => {
  it('parses a JSON allow decision from kernel stdout', async () => {
    const fake = makeFakeKernel(`echo '{"allowed":true}'; exit 0`);
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(true);
    } finally {
      fake.cleanup();
    }
  });

  it('parses a JSON deny decision with reason and rule_id', async () => {
    const fake = makeFakeKernel(
      `echo '{"allowed":false,"reason":"blocked","rule_id":"no-rm-rf"}'; exit 1`,
    );
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.reason).toBe('blocked');
      expect(decision.ruleId).toBe('no-rm-rf');
    } finally {
      fake.cleanup();
    }
  });

  it('parses params rewrite from kernel decision', async () => {
    const fake = makeFakeKernel(
      `echo '{"allowed":true,"params":{"cmd":"ls -la"}}'; exit 0`,
    );
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(true);
      expect(decision.params).toEqual({ cmd: 'ls -la' });
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed when kernel binary is missing (denyOnError=true)', async () => {
    const decision = await evaluateGate(baseInput, {
      ...baseOpts,
      kernelPath: '/nonexistent/chitin-kernel-xyz',
      denyOnError: true,
    });
    expect(decision.allow).toBe(false);
    expect(decision.ruleId).toBe('bridge_error');
    expect(decision.reason).toMatch(/not found|invocation failed/i);
  });

  it('fails open when kernel binary is missing (denyOnError=false)', async () => {
    const decision = await evaluateGate(baseInput, {
      ...baseOpts,
      kernelPath: '/nonexistent/chitin-kernel-xyz',
      denyOnError: false,
    });
    expect(decision.allow).toBe(true);
  });

  it('fails closed on unparseable kernel stdout', async () => {
    const fake = makeFakeKernel(`echo 'not json'; exit 0`);
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('bridge_error');
      expect(decision.reason).toMatch(/unparseable/i);
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed on kernel timeout (denyOnError=true)', async () => {
    // exec so SIGKILL hits sleep directly (no bash-stdout-pipe inheritance race).
    const fake = makeFakeKernel(`exec sleep 5`);
    try {
      const decision = await evaluateGate(baseInput, {
        ...baseOpts,
        kernelPath: fake.path,
        timeoutMs: 200,
        denyOnError: true,
      });
      expect(decision.allow).toBe(false);
      expect(decision.reason).toMatch(/timed out/i);
    } finally {
      fake.cleanup();
    }
  });

  it('rejects decisions where allowed is not a boolean', async () => {
    const fake = makeFakeKernel(`echo '{"allowed":"yes"}'; exit 0`);
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('bridge_error');
    } finally {
      fake.cleanup();
    }
  });
});

describe('plugin.before_tool_call params handling', () => {
  type Handler = (event: { toolName: string; params?: unknown }, ctx: { agentId?: string }) => Promise<unknown>;

  function captureBeforeToolCall(kernelStdoutJson: string): { handler: Handler; cleanup: () => void } {
    const fake = makeFakeKernel(`echo '${kernelStdoutJson}'; exit 0`);
    let handler: Handler | undefined;
    const api = {
      pluginConfig: { kernelPath: fake.path, timeoutMs: 2000, mode: 'enforce' },
      logger: { info: () => {}, warn: () => {}, error: () => {} },
      on: (event: string, fn: Handler) => {
        if (event === 'before_tool_call') handler = fn;
      },
    };
    plugin.register(api);
    if (!handler) throw new Error('before_tool_call handler not registered');
    return { handler, cleanup: fake.cleanup };
  }

  it('returns undefined when kernel allows with empty params object', async () => {
    // Empty `{}` is truthy in JS — the pre-fix `decision.params ? ...` branch
    // would return `{ params: {} }` and clobber the agent's actual args. The
    // fix uses `Object.keys(...).length > 0` so {} now resolves to undefined.
    const { handler, cleanup } = captureBeforeToolCall('{"allowed":true,"params":{}}');
    try {
      const result = await handler(
        { toolName: 'shell.exec', params: { cmd: 'ls' } },
        { agentId: 'test-agent' },
      );
      expect(result).toBeUndefined();
    } finally {
      cleanup();
    }
  });

  it('returns the rewrite when kernel allows with non-empty params', async () => {
    const { handler, cleanup } = captureBeforeToolCall('{"allowed":true,"params":{"cmd":"ls -la"}}');
    try {
      const result = await handler(
        { toolName: 'shell.exec', params: { cmd: 'ls' } },
        { agentId: 'test-agent' },
      );
      expect(result).toEqual({ params: { cmd: 'ls -la' } });
    } finally {
      cleanup();
    }
  });
});
