// Spec 091 — Honor `continue:false` from the chitin governance gate.
// Tests the parser changes (FR-007 surface): both the legacy gate-evaluate
// path (parseDecision via evaluateGate) and the router/hook-gate path
// (parseRouterDecision via evaluateHookGate) must now extract `continue` and
// `stopReason` from the kernel JSON. The kernel emits `continue:false` ONLY
// on lockdown / hard-stop denies; regular denies omit the field and the
// parser must surface `undefined` (NOT `true` — see research.md R4).
//
// Higher-level state tests (stopHookActive sticky, FORCED_CONTINUATION_CAP
// triggers stop_signal_ignored) require importing the plugin's
// before_tool_call handler with a mocked openclaw `api`, which is
// non-trivial scaffolding deferred to a follow-up commit. The parser-level
// tests below exercise the load-bearing code path: when the kernel emits
// `continue:false`, does the GateDecision carry it through?

import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
// @ts-expect-error — sibling .mjs without types
import { evaluateGate, evaluateHookGate } from '../src/chitin-bridge.mjs';

function makeFakeKernel(scriptBody: string): { path: string; cleanup: () => void } {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-091-test-'));
  const path = join(dir, 'fake-kernel');
  writeFileSync(path, `#!/usr/bin/env bash\n${scriptBody}\n`);
  chmodSync(path, 0o755);
  return { path, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

const baseInput = { agent: 'clawta', tool: 'shell.exec', params: { cmd: 'ls' }, cwd: '/tmp' };
const baseOpts = { kernelPath: 'chitin-kernel', timeoutMs: 2000, denyOnError: true };

describe('spec 091 — parseDecision (evaluateGate path) extracts continue/stopReason', () => {
  it('extracts continue:false from a lockdown deny', async () => {
    const fake = makeFakeKernel(
      `echo '{"allowed":false,"reason":"chitin: lockdown","rule_id":"lockdown","continue":false,"stopReason":"chitin: agent in lockdown — session terminated"}'; exit 1`,
    );
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('lockdown');
      // The load-bearing assertion: continue:false survives to the GateDecision.
      expect(decision.continue).toBe(false);
      expect(decision.stopReason).toBe('chitin: agent in lockdown — session terminated');
    } finally {
      fake.cleanup();
    }
  });

  it('returns continue:undefined for regular (non-lockdown) denies', async () => {
    const fake = makeFakeKernel(
      `echo '{"allowed":false,"reason":"blocked","rule_id":"no-destructive-rm"}'; exit 1`,
    );
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.ruleId).toBe('no-destructive-rm');
      // Critical regression guard (research.md R4): absent `continue` field
      // must surface as undefined, NOT as true. Treating absent as true would
      // make every soft deny look like a hard stop.
      expect(decision.continue).toBeUndefined();
      expect(decision.stopReason).toBeUndefined();
    } finally {
      fake.cleanup();
    }
  });

  it('returns continue:undefined for allow decisions', async () => {
    const fake = makeFakeKernel(`echo '{"allowed":true}'; exit 0`);
    try {
      const decision = await evaluateGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(true);
      expect(decision.continue).toBeUndefined();
    } finally {
      fake.cleanup();
    }
  });
});

describe('spec 091 — parseRouterDecision (evaluateHookGate path) extracts continue/stopReason', () => {
  it('extracts continue:false from a router lockdown deny', async () => {
    const fake = makeFakeKernel(
      `echo '{"decision":"block","reason":"chitin: lockdown","rule_id":"lockdown","continue":false,"stopReason":"chitin: agent in lockdown — session terminated"}'; exit 2`,
    );
    try {
      const decision = await evaluateHookGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      // Load-bearing assertions for FR-007:
      expect(decision.continue).toBe(false);
      expect(decision.stopReason).toBe('chitin: agent in lockdown — session terminated');
      // FR-007 also requires the parser to read j.rule_id (was hardcoded 'router_block'):
      expect(decision.ruleId).toBe('lockdown');
    } finally {
      fake.cleanup();
    }
  });

  it('returns continue:undefined for a router regular deny', async () => {
    const fake = makeFakeKernel(
      `echo '{"decision":"block","reason":"blocked","rule_id":"some-rule"}'; exit 2`,
    );
    try {
      const decision = await evaluateHookGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      expect(decision.continue).toBeUndefined();
      expect(decision.stopReason).toBeUndefined();
      // Rule id flows through, not hardcoded:
      expect(decision.ruleId).toBe('some-rule');
    } finally {
      fake.cleanup();
    }
  });

  it('falls back to router_block when j.rule_id is missing (defensive)', async () => {
    const fake = makeFakeKernel(
      `echo '{"decision":"block","reason":"blocked"}'; exit 2`,
    );
    try {
      const decision = await evaluateHookGate(baseInput, { ...baseOpts, kernelPath: fake.path });
      expect(decision.allow).toBe(false);
      // When the kernel omits rule_id (shouldn't happen, but defensive), the
      // parser falls back to 'router_block' rather than dropping the rule
      // identity entirely.
      expect(decision.ruleId).toBe('router_block');
    } finally {
      fake.cleanup();
    }
  });
});
