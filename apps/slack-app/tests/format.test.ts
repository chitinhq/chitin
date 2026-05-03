import { describe, it, expect } from 'vitest';
import {
  formatEnvelopeList,
  formatGrantResult,
  formatGateReset,
  formatChainInfo,
  formatError,
  formatGateStatus,
} from '../src/format.ts';
import type { EnvelopeState, ChainInfo, GateStatus } from '../src/chitin.ts';

const makeEnv = (overrides: Partial<EnvelopeState> = {}): EnvelopeState => ({
  id: 'ENV01',
  status: 'open',
  max_tool_calls: 1000,
  used_tool_calls: 100,
  max_input_bytes: 0,
  used_input_bytes: 0,
  budget_usd: 0,
  created_at: '2026-05-03T00:00:00Z',
  ...overrides,
});

describe('formatEnvelopeList', () => {
  it('empty list returns no-envelopes message', () => {
    const r = formatEnvelopeList([]);
    expect(r.text).toContain('No envelopes');
  });

  it('shows count in text', () => {
    const r = formatEnvelopeList([makeEnv(), makeEnv({ id: 'ENV02' })]);
    expect(r.text).toContain('2 envelopes');
  });

  it('open envelope has Grant button in blocks', () => {
    const r = formatEnvelopeList([makeEnv()]);
    const block = r.blocks?.find((b) => b.accessory?.action_id === 'grant_500_calls');
    expect(block).toBeDefined();
    expect(block?.accessory?.value).toBe('ENV01');
  });

  it('closed envelope has no Grant button', () => {
    const r = formatEnvelopeList([makeEnv({ status: 'closed' })]);
    const block = r.blocks?.find((b) => b.accessory?.action_id === 'grant_500_calls');
    expect(block).toBeUndefined();
  });

  it('uncapped envelope shows uncapped label', () => {
    const r = formatEnvelopeList([makeEnv({ max_tool_calls: 0 })]);
    const text = JSON.stringify(r);
    expect(text).toContain('uncapped');
  });
});

describe('formatGrantResult', () => {
  it('includes id and call count', () => {
    const r = formatGrantResult('ENV01', 500);
    expect(r.text).toContain('ENV01');
    expect(r.text).toContain('500');
  });
});

describe('formatGateReset', () => {
  it('includes agent name', () => {
    const r = formatGateReset('hermes');
    expect(r.text).toContain('hermes');
    expect(r.text).toContain('reset');
  });
});

describe('formatChainInfo', () => {
  it('not found returns not-found message', () => {
    const info: ChainInfo = { exists: false };
    const r = formatChainInfo('chain-abc', info);
    expect(r.text).toContain('not found');
  });

  it('found chain shows seq and truncated hash', () => {
    const info: ChainInfo = { exists: true, last_seq: 7, last_hash: 'aabbccddeeff1122' };
    const r = formatChainInfo('chain-abc', info);
    expect(r.text).toContain('chain-abc');
    const json = JSON.stringify(r);
    expect(json).toContain('last_seq');
    expect(json).toContain('aabbccddee');
  });
});

describe('formatError', () => {
  it('includes error message', () => {
    const r = formatError('something went wrong');
    expect(r.text).toContain('something went wrong');
    expect(r.text).toContain('❌');
  });
});

describe('formatGateStatus', () => {
  it('locked agent shows reset button', () => {
    const st: GateStatus = { agent: 'hermes', level: 'lockdown', locked: true, policy_id: 'p1', mode: 'enforce' };
    const r = formatGateStatus(st);
    const block = r.blocks?.find((b) => b.accessory?.action_id === 'gate_reset');
    expect(block).toBeDefined();
    expect(block?.accessory?.value).toBe('hermes');
  });

  it('unlocked agent shows no reset button', () => {
    const st: GateStatus = { agent: 'hermes', level: 'normal', locked: false, policy_id: 'p1', mode: 'enforce' };
    const r = formatGateStatus(st);
    expect(r.blocks).toBeUndefined();
  });
});
