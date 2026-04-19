import { describe, expect, it } from 'vitest';
import { buildTree, renderTree } from '../src/commands/events-tree';

const fixture = [
  { chain_id: 'S1', chain_type: 'session', parent_chain_id: null, seq: 0, event_type: 'session_start', ts: '2026-04-19T12:00:00Z' },
  { chain_id: 'S1', chain_type: 'session', parent_chain_id: null, seq: 1, event_type: 'user_prompt', ts: '2026-04-19T12:00:01Z' },
  { chain_id: 'T1', chain_type: 'tool_call', parent_chain_id: 'S1', seq: 0, event_type: 'intended', ts: '2026-04-19T12:00:02Z' },
  { chain_id: 'T1', chain_type: 'tool_call', parent_chain_id: 'S1', seq: 1, event_type: 'executed', ts: '2026-04-19T12:00:03Z' },
  { chain_id: 'S1', chain_type: 'session', parent_chain_id: null, seq: 2, event_type: 'assistant_turn', ts: '2026-04-19T12:00:04Z' },
  { chain_id: 'S1', chain_type: 'session', parent_chain_id: null, seq: 3, event_type: 'session_end', ts: '2026-04-19T12:00:05Z' },
];

describe('buildTree', () => {
  it('groups events by chain and links parent-child', () => {
    const tree = buildTree(fixture);
    expect(tree.chains.length).toBe(1);
    const root = tree.chains[0];
    expect(root.chainID).toBe('S1');
    expect(root.children.length).toBe(1);
    expect(root.children[0].chainID).toBe('T1');
  });
});

describe('renderTree', () => {
  it('produces a readable indented string', () => {
    const tree = buildTree(fixture);
    const out = renderTree(tree);
    expect(out).toContain('S1');
    expect(out).toContain('T1');
    expect(out).toContain('intended');
    expect(out).toContain('executed');
  });
});
