import { describe, expect, it } from 'vitest';
import { hookToEventType } from '../src/hook-dispatch';

describe('hookToEventType', () => {
  const cases: Array<[string, Record<string, unknown> | null, string]> = [
    ['SessionStart', null, 'session_start'],
    ['UserPromptSubmit', null, 'user_prompt'],
    ['PreToolUse', null, 'intended'],
    ['PostToolUse', null, 'executed'],
    ['PostToolUse', { error: 'timeout' }, 'failed'],
    ['PreCompact', null, 'compaction'],
    ['SubagentStop', null, 'session_end'],
    ['SessionEnd', null, 'session_end'],
    ['Stop', null, ''],
    ['UnknownHook', null, ''],
  ];

  for (const [hook, payload, want] of cases) {
    it(`${hook} → ${want || 'no-op'}`, () => {
      expect(hookToEventType(hook, payload)).toBe(want);
    });
  }
});
