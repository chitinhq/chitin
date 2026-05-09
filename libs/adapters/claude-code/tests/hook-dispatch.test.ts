import { describe, expect, it } from 'vitest';
import { hookToEventType } from '../src/hook-dispatch';

describe('hookToEventType', () => {
  it.each([
    ['SessionStart', null, 'session_start'],
    ['UserPromptSubmit', null, 'user_prompt'],
    ['PreToolUse', null, 'intended'],
    ['PreCompact', null, 'compaction'],
    ['SubagentStop', null, 'session_end'],
    ['SessionEnd', null, 'session_end'],
    ['UnknownHook', null, ''],
  ])('maps %s to %s', (hook, payload, expected) => {
    expect(hookToEventType(hook, payload as Record<string, unknown> | null)).toBe(expected);
  });

  it('maps PostToolUse without error to executed', () => {
    expect(hookToEventType('PostToolUse', {})).toBe('executed');
  });

  it('maps PostToolUse with error to failed', () => {
    expect(hookToEventType('PostToolUse', { error: 'something went wrong' })).toBe('failed');
  });

  it('maps PostToolUse with null error field to executed', () => {
    expect(hookToEventType('PostToolUse', { error: null })).toBe('executed');
  });

  it('maps PostToolUse with null payload to executed', () => {
    expect(hookToEventType('PostToolUse', null)).toBe('executed');
  });

  it('returns empty string for unknown hooks', () => {
    expect(hookToEventType('NotARealHook', null)).toBe('');
  });
});