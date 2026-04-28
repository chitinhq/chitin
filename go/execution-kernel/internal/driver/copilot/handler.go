package copilot

import (
	"encoding/json"
	"fmt"
	"os"
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
	Gate    Gate
	Agent   string // "copilot-cli" for this driver
	Cwd     string
	Verbose bool // when true, log every Decision as JSON to stderr

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
// Wire kinds are the Copilot CLI's user-intent vocabulary ("approve-once",
// "reject") rather than the Go SDK's internal enum ("approved", "denied-*").
// The JS CLI's permission consumer (Ice() in sdk/index.js) switches on
// user-intent only; sending the Go enum hits its default branch and
// produces "unexpected user permission response", breaking every tool
// call — allow and deny alike. The Go SDK's handlePermissionRequestV2
// also overrides the kind to "denied-no-approval-rule-and-could-not-
// request-from-user" whenever the handler returns a non-nil error, so
// we return nil error on deny too and rely on LockdownCh for the
// lockdown signal path (the SDK discards handler errors before they
// reach SendAndWait anyway).
//
// Returns:
//   - (approve-once,      nil) when the gate allows
//   - (user-not-available, nil) when the gate denies in guide or enforce mode —
//     maps to "denied-no-approval-rule-and-could-not-request-from-user" on the
//     model side, which the model treats as a try-something-else signal
//     (required for the Demo 5 escalation narrative where the model must
//     retry variants until the lockdown threshold fires)
//   - (reject,             nil) when gate returns lockdown — maps to
//     "denied-interactively-by-user", a hard refusal that stops the session;
//     paired with a *LockdownError send on LockdownCh for clean termination
//
// Side effects:
//   - Verbose: decision JSON written to stderr
//   - Lockdown: *LockdownError sent on LockdownCh (non-blocking, capacity-1 buffered)
func (h *Handler) OnPermissionRequest(
	req copilotsdk.PermissionRequest,
	inv copilotsdk.PermissionInvocation,
) (copilotsdk.PermissionRequestResult, error) {
	action := Normalize(req, h.Cwd)
	decision := h.Gate.Evaluate(action, h.Agent)

	if h.Verbose {
		_ = json.NewEncoder(os.Stderr).Encode(decision)
	}

	if decision.Allowed {
		return copilotsdk.PermissionRequestResult{
			Kind: copilotsdk.PermissionRequestResultKind("approve-once"),
		}, nil
	}

	if decision.RuleID == "lockdown" {
		lde := &LockdownError{Agent: h.Agent}
		if h.LockdownCh != nil {
			select {
			case h.LockdownCh <- lde:
			default:
				// channel full (already signalled); drop — Run already knows
			}
		}
		// Hard refusal for lockdown — session must terminate now.
		return copilotsdk.PermissionRequestResult{
			Kind: copilotsdk.PermissionRequestResultKind("reject"),
		}, nil
	}

	// Regular deny — "user-not-available" invites the model to attempt a
	// variation, which drives the escalation counter toward lockdown when
	// the model keeps firing at the same rule.
	return copilotsdk.PermissionRequestResult{
		Kind: copilotsdk.PermissionRequestResultKind("user-not-available"),
	}, nil
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
