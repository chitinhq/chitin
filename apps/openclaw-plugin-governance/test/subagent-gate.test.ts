import { describe, expect, it } from 'vitest';
// @ts-expect-error — sibling .mjs without types
import { isClaudeCodeAgent } from '../src/index.mjs';

describe('isClaudeCodeAgent (ToS denylist)', () => {
  it.each([
    'claude-code',
    'Claude-Code',
    'CLAUDE-CODE',
    'claude_code',
    'Claude Code',
    'claude-code-2',
    'claude-code-1.0',
    '@anthropic/claude-code',
    'org/claude-code',
    'claudecode', // no separator
  ])('matches Claude Code variant: %s', (id) => {
    expect(isClaudeCodeAgent(id)).toBe(true);
  });

  it.each([
    'copilot',
    'local-qwen',
    'local-glm',
    'local-deepseek',
    'main',
    'qwen-agent',
    'claudia', // partial substring of "claude" only — must not match
    'code-claude', // wrong order
    '',
  ])('does NOT match unrelated agent: %s', (id) => {
    expect(isClaudeCodeAgent(id)).toBe(false);
  });

  it('returns false for non-string inputs (defensive)', () => {
    expect(isClaudeCodeAgent(undefined)).toBe(false);
    expect(isClaudeCodeAgent(null)).toBe(false);
    expect(isClaudeCodeAgent(42)).toBe(false);
    expect(isClaudeCodeAgent({ agentId: 'claude-code' })).toBe(false);
  });
});
