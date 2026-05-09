import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// @ts-expect-error — sibling .mjs without types
import { evaluateRouter } from '../src/chitin-bridge.mjs';

function makeFakeKernel(scriptBody: string): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-router-test-'));
  const path = join(dir, 'fake-kernel');
  writeFileSync(path, `#!/usr/bin/env bash\n${scriptBody}\n`);
  chmodSync(path, 0o755);
  return { path, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

const baseInput = { agent: 'test-agent', tool: 'shell.exec', params: { cmd: 'ls' }, cwd: '/tmp' };
const baseOpts = { kernelPath: 'chitin-kernel', timeoutMs: 2000, denyOnError: true };

describe('evaluateRouter (bridge → chitin-kernel router evaluate)', () => {
  // evaluateRouter uses the Claude Code hook protocol:
  //   exit 0 + empty/non-block stdout → allow
  //   exit non-0 + first JSON line {"decision":"block",...} → deny

  it('allows when router exits 0 with no stdout', async () => {
    const fake = makeFakeKernel(`exit 0`);
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(true);
    } finally {
      fake.cleanup();
    }
  });

  it('allows when router exits 0 with empty JSON that is not a block', async () => {
    const fake = makeFakeKernel(`echo '{"decision":"allow","reason":""}'; exit 0`);
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(true);
    } finally {
      fake.cleanup();
    }
  });

  it('blocks when router exits non-0 with decision:block and reason', async () => {
    const fake = makeFakeKernel(
      `echo '{"decision":"block","reason":"no-rm-rf-root"}'; exit 2`,
    );
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.reason).toBe('no-rm-rf-root');
      expect(decision.ruleId).toBe('router_block');
    } finally {
      fake.cleanup();
    }
  });

  it('blocks when router exits non-0 with decision:block and no reason', async () => {
    const fake = makeFakeKernel(`echo '{"decision":"block"}'; exit 2`);
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      // Default reason when none provided
      expect(decision.reason).toBe('denied by chitin router');
      expect(decision.ruleId).toBe('router_block');
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed when router exits non-0 with no parseable verdict', async () => {
    const fake = makeFakeKernel(`echo 'ERROR: something went wrong'; exit 1`);
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('bridge_error');
      expect(decision.reason).toMatch(/no parseable verdict/i);
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed when router exits non-0 with non-block JSON decision', async () => {
    // Router returns exit 1 with a JSON that isn't {"decision":"block"}
    // This is an unexpected protocol violation — fail closed
    const fake = makeFakeKernel(`echo '{"decision":"error","message":"internal"}'; exit 1`);
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('bridge_error');
      expect(decision.reason).toMatch(/non-block deny verdict/i);
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed on router timeout (denyOnError=true)', async () => {
    const fake = makeFakeKernel(`exec sleep 5`);
    try {
      const decision = await evaluateRouter(baseInput, {
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

  it('fails open on router timeout (denyOnError=false)', async () => {
    const fake = makeFakeKernel(`exec sleep 5`);
    try {
      const decision = await evaluateRouter(baseInput, {
        ...baseOpts,
        kernelPath: fake.path,
        timeoutMs: 200,
        denyOnError: false,
      });
      expect(decision.allow).toBe(true);
    } finally {
      fake.cleanup();
    }
  });

  it('fails closed when kernel binary is missing (denyOnError=true)', async () => {
    const decision = await evaluateRouter(baseInput, {
      ...baseOpts,
      kernelPath: '/nonexistent/chitin-kernel-xyz',
      denyOnError: true,
    });
    expect(decision.allow).toBe(false);
    expect(decision.ruleId).toBe('bridge_error');
    expect(decision.reason).toMatch(/not found|invocation failed/i);
  });

  it('fails open when kernel binary is missing (denyOnError=false)', async () => {
    const decision = await evaluateRouter(baseInput, {
      ...baseOpts,
      kernelPath: '/nonexistent/chitin-kernel-xyz',
      denyOnError: false,
    });
    expect(decision.allow).toBe(true);
  });

  it('passes sessionId in the hook input when provided', async () => {
    // Verify that sessionId reaches the kernel via stdin
    const dir = mkdtempSync(join(tmpdir(), 'chitin-stdin-'));
    const stdinPath = join(dir, 'stdin.json');
    const fake = makeFakeKernel(
      `cat > ${stdinPath}; echo '{"decision":"block","reason":"test"}'; exit 2`,
    );
    try {
      await evaluateRouter(
        { ...baseInput, sessionId: 'my-session-42' },
        { ...baseOpts, kernelPath: fake.path },
      );
      const stdin = JSON.parse(await import('node:fs').then((fs) => fs.readFileSync(stdinPath, 'utf-8')));
      expect(stdin.session_id).toBe('my-session-42');
      expect(stdin.hook_event_name).toBe('PreToolUse');
      expect(stdin.tool_name).toBe('shell.exec');
    } finally {
      fake.cleanup();
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it('uses default sessionId when not provided', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-stdin-'));
    const stdinPath = join(dir, 'stdin.json');
    const fake = makeFakeKernel(
      `cat > ${stdinPath}; echo '{"decision":"block","reason":"test"}'; exit 2`,
    );
    try {
      await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      const stdin = JSON.parse(await import('node:fs').then((fs) => fs.readFileSync(stdinPath, 'utf-8')));
      expect(stdin.session_id).toBe('openclaw-test-agent');
    } finally {
      fake.cleanup();
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it('extracts first JSON line from multi-line output', async () => {
    // Router may emit log lines before the verdict
    const fake = makeFakeKernel(
      `echo 'some log line'; echo '{"decision":"block","reason":"blast-radius"}'; echo 'trailing'; exit 2`,
    );
    try {
      const decision = await evaluateRouter(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.reason).toBe('blast-radius');
    } finally {
      fake.cleanup();
    }
  });
});