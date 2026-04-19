// Code generated from libs/contracts/src/event.schema.ts — DO NOT EDIT.
// Regenerate with: pnpm exec nx run @chitin/contracts:generate-go-types
package event

import "time"

// ActionType is one of 6 canonical categories.
type ActionType string

const (
	ActionRead      ActionType = "read"
	ActionWrite     ActionType = "write"
	ActionExec      ActionType = "exec"
	ActionGit       ActionType = "git"
	ActionNet       ActionType = "net"
	ActionDangerous ActionType = "dangerous"
)

// Result is the decision outcome for an event.
type Result string

const (
	ResultSuccess Result = "success"
	ResultError   Result = "error"
	ResultDenied  Result = "denied"
)

// Event matches the zod EventSchema in libs/contracts/src/event.schema.ts.
type Event struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	Surface       string         `json:"surface"`
	Driver        string         `json:"driver"`
	AgentID       string         `json:"agent_id"`
	ToolName      string         `json:"tool_name"`
	RawInput      map[string]any `json:"raw_input"`
	CanonicalForm map[string]any `json:"canonical_form"`
	ActionType    ActionType     `json:"action_type"`
	Result        Result         `json:"result"`
	DurationMs    int64          `json:"duration_ms"`
	Error         *string        `json:"error"`
	TS            time.Time      `json:"ts"`
	Metadata      map[string]any `json:"metadata"`
}
