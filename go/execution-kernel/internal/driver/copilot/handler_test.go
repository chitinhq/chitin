package copilot

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// The Copilot CLI's embedded JS permission consumer uses user-intent kinds.
// These constants are the wire values the handler must emit.
const (
	wireApprove   = copilotsdk.PermissionRequestResultKind("approve-once")
	wireRetryable = copilotsdk.PermissionRequestResultKind("user-not-available") // regular deny — invites model retry
	wireReject    = copilotsdk.PermissionRequestResultKind("reject")             // hard refusal — lockdown only
)

// captureStderr redirects os.Stderr for the duration of fn and returns the bytes written.
// Used to assert Verbose decision logging without coupling tests to log framing.
//
// Restores os.Stderr and closes the read-end via defer so a panic in fn or
// io.Copy doesn't leak fds across the test suite (issue #56).
func captureStderr(t *testing.T, fn func()) []byte {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	defer r.Close()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()
	fn()
	_ = w.Close()
	return <-done
}

// mockGate lets tests inject specific Decisions.
type mockGate struct {
	decision gov.Decision
	locked   bool
}

func (m *mockGate) Evaluate(a gov.Action, agent string, _ *gov.BudgetEnvelope) gov.Decision {
	if m.locked {
		return gov.Decision{
			Allowed:    false,
			Mode:       "enforce",
			RuleID:     "lockdown",
			Reason:     "agent in lockdown — contact operator",
			Escalation: "lockdown",
			Agent:      agent,
		}
	}
	return m.decision
}

func strPtr(s string) *string { return &s }

func TestHandler_Allow(t *testing.T) {
	h := &Handler{
		Gate:  &mockGate{decision: gov.Decision{Allowed: true, RuleID: "default-allow-shell"}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("ls /tmp"),
	}
	res, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected no error on allow, got %v", err)
	}
	if res.Kind != wireApprove {
		t.Errorf("Result kind: got %v, want %v", res.Kind, wireApprove)
	}
}

func TestHandler_GuideDenyReturnsRetryableAndLogsDecision(t *testing.T) {
	// Wire kind is "user-not-available" (retryable: invites the model to try
	// a variation, which drives the escalation counter). Guide text travels
	// via the Verbose decision log on stderr, not via the handler's error
	// return — the Go SDK drops handler errors before they reach the wire.
	h := &Handler{
		Gate: &mockGate{decision: gov.Decision{
			Allowed:          false,
			Mode:             "guide",
			RuleID:           "no-destructive-rm",
			Reason:           "Recursive delete is blocked",
			Suggestion:       "Use git rm for specific files",
			CorrectedCommand: "git rm <file>",
		}},
		Agent:   "copilot-cli",
		Cwd:     "/work",
		Verbose: true,
	}
	req := copilotsdk.PermissionRequest{
		Kind:            copilotsdk.PermissionRequestKindShell,
		FullCommandText: strPtr("rm -rf /"),
	}

	var res copilotsdk.PermissionRequestResult
	var err error
	stderr := captureStderr(t, func() {
		res, err = h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	})

	if err != nil {
		t.Fatalf("expected no error on deny (wire-path uses nil error), got %v", err)
	}
	if res.Kind != wireRetryable {
		t.Errorf("Result kind: got %v, want %v", res.Kind, wireRetryable)
	}

	// Decision JSON must carry the full guide-text payload for the decision log.
	var logged gov.Decision
	if err := json.Unmarshal(bytes.TrimSpace(stderr), &logged); err != nil {
		t.Fatalf("stderr not valid decision JSON: %v — raw=%q", err, stderr)
	}
	if logged.Reason != "Recursive delete is blocked" {
		t.Errorf("logged Reason: got %q", logged.Reason)
	}
	if logged.Suggestion != "Use git rm for specific files" {
		t.Errorf("logged Suggestion: got %q", logged.Suggestion)
	}
	if logged.CorrectedCommand != "git rm <file>" {
		t.Errorf("logged CorrectedCommand: got %q", logged.CorrectedCommand)
	}
}

func TestHandler_FormatGuideError_OmitsEmptySegments(t *testing.T) {
	// formatGuideError remains as a pure helper — used by demo scenario tests
	// and (potentially) future surfaces that render a guide string directly.
	// With empty Suggestion/CorrectedCommand, the optional segments are absent.
	d := gov.Decision{
		Allowed: false,
		Mode:    "guide",
		RuleID:  "generic-deny",
		Reason:  "policy violation",
	}
	msg := formatGuideError(d)
	if !strings.HasPrefix(msg, "chitin: ") {
		t.Errorf("missing chitin prefix, got %q", msg)
	}
	if !strings.Contains(msg, "policy violation") {
		t.Errorf("missing reason, got %q", msg)
	}
	if strings.Contains(msg, "suggest:") {
		t.Errorf("empty Suggestion should NOT produce a suggest segment, got %q", msg)
	}
	if strings.Contains(msg, "try:") {
		t.Errorf("empty CorrectedCommand should NOT produce a try segment, got %q", msg)
	}
}

func TestHandler_LockdownSignalsChannelAndReturnsReject(t *testing.T) {
	// Lockdown no longer travels as an error (SDK drops it). LockdownCh is
	// the sole signal path; the wire result is "reject" like any other deny.
	lockdownCh := make(chan *LockdownError, 1)
	h := &Handler{
		Gate:       &mockGate{locked: true},
		Agent:      "copilot-cli",
		Cwd:        "/work",
		LockdownCh: lockdownCh,
	}
	req := copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKindShell, FullCommandText: strPtr("anything")}
	res, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected no error on lockdown (wire-path uses nil error), got %v", err)
	}
	if res.Kind != wireReject {
		t.Errorf("Result kind on lockdown: got %v, want %v", res.Kind, wireReject)
	}

	select {
	case lde := <-lockdownCh:
		if lde == nil {
			t.Fatal("LockdownCh delivered nil")
		}
		if lde.Agent != "copilot-cli" {
			t.Errorf("LockdownError.Agent: got %q, want copilot-cli", lde.Agent)
		}
	default:
		t.Fatal("expected *LockdownError on LockdownCh, channel was empty")
	}
}

func TestHandler_LockdownCountReflectsPriorDenials(t *testing.T) {
	// Issue #54: LockdownError.Count must carry the per-handler denial
	// total so printLockdownSummary's "denials=N" matches what the
	// operator just watched. Mock gate denies for the first 3 calls,
	// then locks down on the 4th — final Count must be 3.
	gate := &countingGate{denyCount: 3}
	lockdownCh := make(chan *LockdownError, 1)
	h := &Handler{
		Gate:       gate,
		Agent:      "copilot-cli",
		Cwd:        "/work",
		LockdownCh: lockdownCh,
	}
	req := copilotsdk.PermissionRequest{
		Kind: copilotsdk.PermissionRequestKindShell, FullCommandText: strPtr("rm -rf /"),
	}
	for i := 0; i < 4; i++ {
		_, _ = h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	}
	select {
	case lde := <-lockdownCh:
		if lde.Count != 3 {
			t.Errorf("LockdownError.Count: got %d, want 3 (3 prior denials before lockdown)", lde.Count)
		}
	default:
		t.Fatal("expected lockdown signal on channel")
	}
}

// countingGate denies for the first denyCount calls, then locks down.
type countingGate struct {
	denyCount int
	calls     int
}

func (g *countingGate) Evaluate(a gov.Action, agent string, _ *gov.BudgetEnvelope) gov.Decision {
	g.calls++
	if g.calls > g.denyCount {
		return gov.Decision{Allowed: false, Mode: "enforce", RuleID: "lockdown", Agent: agent}
	}
	return gov.Decision{Allowed: false, Mode: "guide", RuleID: "no-rm", Reason: "denied"}
}

func TestHandler_LockdownWithNilChannelDoesNotPanic(t *testing.T) {
	// LockdownCh is optional; nil must not cause a panic in the signal path.
	h := &Handler{Gate: &mockGate{locked: true}, Agent: "copilot-cli", Cwd: "/work"}
	req := copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKindShell, FullCommandText: strPtr("anything")}
	_, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestHandler_UnknownKindGoesThroughNormalizeAndIsRejected(t *testing.T) {
	// Unknown Kind → Action{Type: "unknown"} → gate returns deny → wire "reject".
	h := &Handler{
		Gate: &mockGate{decision: gov.Decision{
			Allowed: false,
			Mode:    "enforce",
			Reason:  "unknown action type not permitted",
		}},
		Agent: "copilot-cli",
		Cwd:   "/work",
	}
	req := copilotsdk.PermissionRequest{Kind: copilotsdk.PermissionRequestKind("nonexistent")}
	res, err := h.OnPermissionRequest(req, copilotsdk.PermissionInvocation{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if res.Kind != wireRetryable {
		t.Errorf("Result kind: got %v, want %v", res.Kind, wireRetryable)
	}
}

func TestLockdownError_Error(t *testing.T) {
	lde := &LockdownError{Agent: "glm-agent", Count: 3}
	msg := lde.Error()
	if !strings.Contains(msg, "glm-agent") {
		t.Errorf("expected agent name in message, got: %s", msg)
	}
	if !strings.Contains(msg, "3") {
		t.Errorf("expected denial count in message, got: %s", msg)
	}
	if !strings.Contains(msg, "chitin-lockdown") {
		t.Errorf("expected 'chitin-lockdown' prefix in message, got: %s", msg)
	}
}

func TestLockdownError_ZeroCount(t *testing.T) {
	lde := &LockdownError{Agent: "copilot", Count: 0}
	msg := lde.Error()
	if !strings.Contains(msg, "copilot") {
		t.Errorf("expected agent name in message, got: %s", msg)
	}
}

func TestPrintLockdownSummary(t *testing.T) {
	lde := &LockdownError{Agent: "test-agent", Count: 5}
	stderr := captureStderr(t, func() {
		printLockdownSummary(lde)
	})
	if !strings.Contains(string(stderr), "Session terminated") {
		t.Errorf("expected 'Session terminated' in stderr, got: %s", stderr)
	}
	if !strings.Contains(string(stderr), "test-agent") {
		t.Errorf("expected agent name in stderr, got: %s", stderr)
	}
}
