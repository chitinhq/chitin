// Package hookdispatch maps Claude Code hook event names (+ payload hints) to v2 event_type.
package hookdispatch

// HookToEventType returns the v2 event_type for a Claude Code hook event, or "" if not subscribed.
// For PostToolUse, payload is inspected to choose between "executed" (success) and "failed".
func HookToEventType(hook string, payload map[string]any) string {
	switch hook {
	case "SessionStart":
		return "session_start"
	case "UserPromptSubmit":
		return "user_prompt"
	case "PreToolUse":
		return "intended"
	case "PostToolUse":
		if payload != nil {
			if errVal, ok := payload["error"]; ok && errVal != nil {
				return "failed"
			}
		}
		return "executed"
	case "PreCompact":
		return "compaction"
	case "SubagentStop":
		return "session_end"
	case "SessionEnd":
		return "session_end"
	default:
		// Stop and any others: not subscribed.
		return ""
	}
}
