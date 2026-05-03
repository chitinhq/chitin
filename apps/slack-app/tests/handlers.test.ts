import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../src/chitin.ts', () => ({
  envelopeList: vi.fn(),
  envelopeGrant: vi.fn(),
  gateReset: vi.fn(),
  chainInfo: vi.fn(),
  gateStatus: vi.fn(),
}));

import { handleSlashCommand, handleBlockAction, isDestructiveAction } from '../src/handlers.ts';
import * as chitin from '../src/chitin.ts';

const envelopeList = vi.mocked(chitin.envelopeList);
const envelopeGrant = vi.mocked(chitin.envelopeGrant);
const gateReset = vi.mocked(chitin.gateReset);
const chainInfo = vi.mocked(chitin.chainInfo);
// gateStatus is imported via the `chitin` namespace below so the mock
// covers all named exports; not directly referenced here.
void chitin.gateStatus;

beforeEach(() => vi.clearAllMocks());

describe('handleSlashCommand', () => {
  it('envelope-status returns formatted list', () => {
    envelopeList.mockReturnValue([
      {
        id: '01HZ',
        status: 'open',
        max_tool_calls: 1000,
        used_tool_calls: 42,
        max_input_bytes: 0,
        used_input_bytes: 0,
        budget_usd: 0,
        created_at: '2026-05-03T00:00:00Z',
      },
    ]);
    const r = handleSlashCommand('envelope-status');
    expect(r.text).toContain('1 envelopes');
    expect(envelopeList).toHaveBeenCalledOnce();
  });

  it('envelope-status with no envelopes returns empty message', () => {
    envelopeList.mockReturnValue([]);
    const r = handleSlashCommand('envelope-status');
    expect(r.text).toContain('No envelopes');
  });

  it('envelope-grant calls envelopeGrant with correct args', () => {
    envelopeGrant.mockReturnValue(undefined);
    const r = handleSlashCommand('envelope-grant 01HZ 200');
    expect(envelopeGrant).toHaveBeenCalledWith('01HZ', 200);
    expect(r.text).toContain('Granted');
    expect(r.text).toContain('+200');
  });

  it('envelope-grant missing args returns error', () => {
    const r = handleSlashCommand('envelope-grant');
    expect(r.text).toMatch(/Usage/);
    expect(envelopeGrant).not.toHaveBeenCalled();
  });

  it('envelope-grant with non-numeric calls returns error', () => {
    const r = handleSlashCommand('envelope-grant 01HZ abc');
    expect(r.text).toMatch(/Usage/);
  });

  it('gate-reset calls gateReset', () => {
    gateReset.mockReturnValue(undefined);
    const r = handleSlashCommand('gate-reset claude-code');
    expect(gateReset).toHaveBeenCalledWith('claude-code');
    expect(r.text).toContain('reset');
  });

  it('gate-reset missing agent returns error', () => {
    const r = handleSlashCommand('gate-reset');
    expect(r.text).toMatch(/Usage/);
  });

  it('chain-info returns chain data', () => {
    chainInfo.mockReturnValue({ exists: true, last_seq: 5, last_hash: 'abc123' });
    const r = handleSlashCommand('chain-info some-session-id');
    expect(chainInfo).toHaveBeenCalledWith('some-session-id');
    expect(r.text).toContain('some-session-id');
  });

  it('chain-info not found shows not-found message', () => {
    chainInfo.mockReturnValue({ exists: false });
    const r = handleSlashCommand('chain-info missing-id');
    expect(r.text).toContain('not found');
  });

  it('unknown subcommand returns error', () => {
    const r = handleSlashCommand('wat');
    expect(r.text).toMatch(/Unknown subcommand/);
  });

  it('empty text returns error', () => {
    const r = handleSlashCommand('');
    expect(r.text).toMatch(/Unknown subcommand/);
  });

  it('chitin client error is surfaced as error response', () => {
    envelopeList.mockImplementation(() => { throw new Error('kernel not found'); });
    const r = handleSlashCommand('envelope-status');
    expect(r.text).toContain('kernel not found');
  });
});

describe('handleBlockAction', () => {
  it('gate_reset action calls gateReset', () => {
    gateReset.mockReturnValue(undefined);
    const r = handleBlockAction('gate_reset', 'hermes');
    expect(gateReset).toHaveBeenCalledWith('hermes');
    expect(r.text).toContain('reset');
  });

  it('gate_reset with empty value returns error', () => {
    const r = handleBlockAction('gate_reset', '');
    expect(r.text).toContain('missing agent');
  });

  it('grant_500_calls action grants 500 calls', () => {
    envelopeGrant.mockReturnValue(undefined);
    const r = handleBlockAction('grant_500_calls', '01HZ');
    expect(envelopeGrant).toHaveBeenCalledWith('01HZ', 500);
    expect(r.text).toContain('+500');
  });

  it('grant_500_calls with empty value returns error', () => {
    const r = handleBlockAction('grant_500_calls', '');
    expect(r.text).toContain('missing envelope');
  });

  it('unknown action returns error', () => {
    const r = handleBlockAction('unknown_action', 'val');
    expect(r.text).toContain('Unknown action');
  });

  it('approve_pr with invalid PR number returns error', () => {
    const r = handleBlockAction('approve_pr', 'not-a-number');
    expect(r.text).toContain('invalid PR number');
  });

  it('approve_pr with non-positive PR number returns error', () => {
    const r = handleBlockAction('approve_pr', '0');
    expect(r.text).toContain('invalid PR number');
  });

  it('approve_pr surfaces gh spawn failure (binary missing)', () => {
    // Force gh to a path that can't exist; spawnSync should populate
    // result.error and the handler should surface it via formatError.
    const prev = process.env['PATH'];
    process.env['PATH'] = '/no-such-dir';
    try {
      const r = handleBlockAction('approve_pr', '999');
      expect(r.text).toMatch(/spawn failed|exited|gh pr merge/);
    } finally {
      if (prev === undefined) delete process.env['PATH'];
      else process.env['PATH'] = prev;
    }
  });
});

describe('isDestructiveAction', () => {
  it('classifies envelope-grant as destructive', () => {
    expect(isDestructiveAction('envelope-grant')).toBe(true);
  });

  it('classifies gate-reset as destructive', () => {
    expect(isDestructiveAction('gate-reset')).toBe(true);
  });

  it('classifies envelope-status as read-only', () => {
    expect(isDestructiveAction('envelope-status')).toBe(false);
  });

  it('classifies gate-status as read-only', () => {
    expect(isDestructiveAction('gate-status')).toBe(false);
  });

  it('classifies chain-info as read-only', () => {
    expect(isDestructiveAction('chain-info')).toBe(false);
  });

  it('classifies unknown subcommands as read-only (server falls through to handler error)', () => {
    expect(isDestructiveAction('wat')).toBe(false);
  });
});
