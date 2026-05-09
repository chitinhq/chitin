package copilot

import (
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	copilotsdk "github.com/github/copilot-sdk/go"
)

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// wireApprove and wireReject are the result Kind strings that OnPermissionRequest
// actually returns. The handler uses plain string kinds ("approve-once", "reject")
// rather than the SDK's PermissionRequestResultKindApproved/Denied* constants.
const (
	wireAllow  = copilotsdk.PermissionRequestResultKind("approve-once")
	wireReject = copilotsdk.PermissionRequestResultKind("reject")
)

// mockGate is a test double for the copilot handler's Gate interface
// (Evaluate + RecordDenial).
type mockGate struct {
	decision   gov.Decision
	err        error
	callCount  int
}

func (m *mockGate) Evaluate(a gov.Action, agent string, envelope *gov.BudgetEnvelope) gov.Decision {
	m.callCount++
	return m.decision
}

func (m *mockGate) RecordDenial(action gov.Action, agent string, args map[string]any) error {
	m.callCount++
	return m.err
}