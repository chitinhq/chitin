/**
 * Maps Claude Code hook event names (+ optional payload hints) to v2 event_type.
 * Returns "" for hooks we do not subscribe to.
 */
export function hookToEventType(
  hook: string,
  payload: Record<string, unknown> | null,
): string {
  switch (hook) {
    case 'SessionStart':
      return 'session_start';
    case 'UserPromptSubmit':
      return 'user_prompt';
    case 'PreToolUse':
      return 'intended';
    case 'PostToolUse':
      if (payload && payload.error != null) return 'failed';
      return 'executed';
    case 'PreCompact':
      return 'compaction';
    case 'SubagentStop':
      return 'session_end';
    case 'SessionEnd':
      return 'session_end';
    default:
      return '';
  }
}
