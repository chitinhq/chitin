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
func captureStderr(t *testing.T, fn func()) []byte {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// mockGate lets tests inject specific Decisions.
type mockGate struct {
	decision gov.Decision
	locked   bool
}

func (m *mockGate) Evaluate(a gov.Action, agent string) gov.Decision {
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

func TestHandler_GuideDenyRejectsAndLogsDecision(t *testing.T) {
	// Wire kind flips to "reject" with nil error. Guide text travels via the
	// Verbose decision log on stderr, not via the handler's error return —
	// the Go SDK drops handler errors before they reach the wire.
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
