import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// @ts-expect-error — sibling .mjs without types
import plugin from '../src/index.mjs';

function makeFakeKernel(scriptBody: string): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-plugin-test-'));
  const path = join(dir, 'fake-kernel');
  writeFileSync(path, `#!/usr/bin/env bash\n${scriptBody}\n`);
  chmodSync(path, 0o755);
  return { path, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

describe('plugin.observe mode', () => {
  type BeforeHandler = (event: { toolName: string; params?: unknown }, ctx: { agentId?: string }) => Promise<unknown>;

  function registerPlugin(fakeKernelPath: string, mode: string): { beforeHandler: BeforeHandler; capturedLogs: string[]; cleanup: () => void } {
    const fake = makeFakeKernel(`echo '{"decision":"block","reason":"policy deny"}'; exit 2`);
    const capturedLogs: string[] = [];
    const api = {
      pluginConfig: { kernelPath: fake.path, timeoutMs: 2000, mode },
      logger: {
        info: (...args: unknown[]) => capturedLogs.push(['info', ...args].join(' ')),
        warn: (...args: unknown[]) => capturedLogs.push(['warn', ...args].join(' ')),
        error: (...args: unknown[]) => capturedLogs.push(['error', ...args].join(' ')),
      },
      on: (event: string, fn: BeforeHandler) => {
        if (event === 'before_tool_call') return fn;
        return undefined;
      },
    };

    const handlers: Record<string, unknown> = {};
    const pluginApi = {
      ...api,
      on: (event: string, fn: unknown) => { handlers[event] = fn; },
    };

    plugin.register(pluginApi);
    const beforeHandler = handlers['before_tool_call'] as BeforeHandler;
    return { beforeHandler, capturedLogs, cleanup: fake.cleanup };
  }

  it('observe mode does not block even when router denies', async () => {
    const { beforeHandler, capturedLogs, cleanup } = registerPlugin('/tmp/fake', 'observe');
    try {
      const result = await beforeHandler(
        { toolName: 'shell.exec', params: { cmd: 'rm -rf /' } },
        { agentId: 'test-agent' },
      );
      // In observe mode, the tool call proceeds — handler returns undefined
      expect(result).toBeUndefined();
      // But a warning should be logged
      expect(capturedLogs.some((log) => log.includes('would-deny'))).toBe(true);
    } finally {
      cleanup();
    }
  });

  it('enforce mode blocks when router denies', async () => {
    const fake = makeFakeKernel(`echo '{"decision":"block","reason":"no-rm-rf"}'; exit 2`);
    const capturedLogs: string[] = [];
    const handlers: Record<string, unknown> = {};
    const pluginApi = {
      pluginConfig: { kernelPath: fake.path, timeoutMs: 2000, mode: 'enforce' },
      logger: {
        info: (...args: unknown[]) => capturedLogs.push(['info', ...args].join(' ')),
        warn: (...args: unknown[]) => capturedLogs.push(['warn', ...args].join(' ')),
        error: (...args: unknown[]) => capturedLogs.push(['error', ...args].join(' ')),
      },
      on: (event: string, fn: unknown) => { handlers[event] = fn; },
    };

    plugin.register(pluginApi);
    const handler = handlers['before_tool_call'] as (event: { toolName: string; params?: unknown }, ctx: { agentId?: string }) => Promise<unknown>;

    try {
      const result = (await handler(
        { toolName: 'shell.exec', params: { cmd: 'rm -rf /' } },
        { agentId: 'test-agent' },
      )) as { block?: boolean; blockReason?: string };
      expect(result?.block).toBe(true);
    } finally {
      fake.cleanup();
    }
  });
});

describe('plugin.before_install handler', () => {
  type InstallHandler = (event: { request?: { kind?: string } }, ctx: unknown) => Promise<unknown>;

  function captureInstallHandler(workerMode: boolean): { handler: InstallHandler; cleanup: () => void } {
    const fake = makeFakeKernel(`exit 0`);
    const handlers: Record<string, unknown> = {};
    const pluginApi = {
      pluginConfig: { kernelPath: fake.path, timeoutMs: 2000, mode: 'enforce', workerMode },
      logger: { info: () => {}, warn: () => {}, error: () => {} },
      on: (event: string, fn: unknown) => { handlers[event] = fn; },
    };

    plugin.register(pluginApi);
    return { handler: handlers['before_install'] as InstallHandler, cleanup: fake.cleanup };
  }

  it('blocks plugin-git installs when workerMode is true', async () => {
    const { handler, cleanup } = captureInstallHandler(true);
    try {
      const result = (await handler(
        { request: { kind: 'plugin-git' } },
        {},
      )) as { block?: boolean; blockReason?: string };
      expect(result?.block).toBe(true);
      expect(result?.blockReason).toMatch(/git-kind/i);
    } finally {
      cleanup();
    }
  });

  it('allows plugin-git installs when workerMode is false', async () => {
    const { handler, cleanup } = captureInstallHandler(false);
    try {
      const result = await handler(
        { request: { kind: 'plugin-git' } },
        {},
      );
      // Not in worker mode — install is allowed, handler returns undefined
      expect(result).toBeUndefined();
    } finally {
      cleanup();
    }
  });

  it('allows non-plugin-git installs in worker mode', async () => {
    const { handler, cleanup } = captureInstallHandler(true);
    try {
      const result = await handler(
        { request: { kind: 'plugin-npm' } },
        {},
      );
      expect(result).toBeUndefined();
    } finally {
      cleanup();
    }
  });

  it('allows installs with no kind in worker mode', async () => {
    const { handler, cleanup } = captureInstallHandler(true);
    try {
      const result = await handler({ request: {} }, {});
      expect(result).toBeUndefined();
    } finally {
      cleanup();
    }
  });

  it('returns undefined when not in worker mode regardless of install kind', async () => {
    const { handler, cleanup } = captureInstallHandler(false);
    try {
      const result = await handler({ request: { kind: 'plugin-git' } }, {});
      expect(result).toBeUndefined();
    } finally {
      cleanup();
    }
  });
});