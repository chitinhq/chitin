package copilot

import (
	"fmt"
	"strings"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Gate is the minimal gov interface the Handler needs. Satisfied by
// *gov.Gate; defined here so tests can inject mocks without the SQLite
// Counter dependency.
//
// Invariant: Evaluate is the single entry point — it handles lockdown
// short-circuit internally (returns Decision{RuleID:"lockdown"} when
// the agent is in lockdown). The caller detects lockdown via RuleID,
// not a separate IsLocked call.
type Gate interface {
	Evaluate(a gov.Action, agent string) gov.Decision
}

// LockdownError is returned when the agent has hit the escalation lockdown
// threshold. The driver recognizes this sentinel type and terminates the
// session cleanly (exit 0 — lockdown is correct operation, not a crash).
type LockdownError struct {
	Agent string
	// Count is the number of denials that triggered lockdown, if known.
	// 0 when the lockdown was detected from a pre-existing persisted state.
	Count int
}

func (e *LockdownError) Error() string {
	return fmt.Sprintf(
		"chitin-lockdown: agent=%s denials=%d — session terminated. Reset with: chitin-kernel gate reset --agent=%s",
		e.Agent, e.Count, e.Agent,
	)
}

// Handler implements the SDK's PermissionHandlerFunc. It holds a reference
// to the gov.Gate (library-direct, no subprocess) and an agent identifier
// used for escalation tracking.
//
// Wire-up: assign Handler.OnPermissionRequest as the OnPermissionRequest
// field in copilotsdk.SessionConfig or copilotsdk.ClientOptions.
type Handler struct {
	Gate  Gate
	Agent string // "copilot-cli" for this driver
	Cwd   string

	// LockdownCh, if non-nil, receives a *LockdownError when OnPermissionRequest
	// detects a lockdown decision. The SDK's executePermissionAndRespond discards
	// handler errors before they can propagate to SendAndWait, so this channel
	// is the reliable signal path for Run() to detect lockdown and terminate
	// the session cleanly. A capacity-1 buffered channel is recommended so the
	// send never blocks (lockdown fires at most once per session).
	LockdownCh chan<- *LockdownError
}

// OnPermissionRequest is the SDK PermissionHandlerFunc callback. Called
// synchronously by the SDK before each tool execution.
//
// Returns:
//   - (Approved, nil)            when the gate allows the action
//   - (Denied, error)            with guide-mode encoding on gate deny:
//     "chitin: <Reason> [| suggest: <Suggestion>] [| try: <CorrectedCommand>]"
//     (Suggestion and CorrectedCommand segments are omitted when empty)
//   - (Denied, *LockdownError)   when the decision has RuleID "lockdown",
//     so the driver can detect via errors.As and terminate the session cleanly
func (h *Handler) OnPermissionRequest(
	req copilotsdk.PermissionRequest,
	inv copilotsdk.PermissionInvocation,
) (copilotsdk.PermissionRequestResult, error) {
	denied := copilotsdk.PermissionRequestResult{
		Kind: copilotsdk.PermissionRequestResultKindDeniedCouldNotRequestFromUser,
	}

	action := Normalize(req, h.Cwd)
	decision := h.Gate.Evaluate(action, h.Agent)

	if decision.Allowed {
		return copilotsdk.PermissionRequestResult{
			Kind: copilotsdk.PermissionRequestResultKindApproved,
		}, nil
	}

	// Lockdown short-circuit: gate.Evaluate returns RuleID="lockdown" when
	// the agent is in lockdown (either pre-existing or just triggered).
	// Return the sentinel type so the driver can detect via errors.As.
	// Also signal LockdownCh — the SDK discards handler errors before they
	// reach SendAndWait, so the channel is the reliable signal path.
	if decision.RuleID == "lockdown" {
		lde := &LockdownError{Agent: h.Agent}
		if h.LockdownCh != nil {
			select {
			case h.LockdownCh <- lde:
			default:
				// channel full (already signalled); drop — Run already knows
			}
		}
		return denied, lde
	}

	// Guide-mode (or enforce-mode) denial: encode into model-facing error
	// string so the model can self-correct.
	return denied, fmt.Errorf("%s", formatGuideError(decision))
}

// formatGuideError produces the model-facing refusal string.
//
// Format: "chitin: <Reason> [| suggest: <Suggestion>] [| try: <CorrectedCommand>]"
//
// Invariant: if Suggestion is empty, the "suggest:" segment is absent;
// if CorrectedCommand is empty, the "try:" segment is absent.
// The "chitin: " prefix is always present.
func formatGuideError(d gov.Decision) string {
	parts := []string{"chitin: " + d.Reason}
	if d.Suggestion != "" {
		parts = append(parts, "suggest: "+d.Suggestion)
	}
	if d.CorrectedCommand != "" {
		parts = append(parts, "try: "+d.CorrectedCommand)
	}
	return strings.Join(parts, " | ")
}
