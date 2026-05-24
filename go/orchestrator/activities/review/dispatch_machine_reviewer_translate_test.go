package review

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestTranslateInvokeResult_SuccessVerdict covers the dominant happy
// path: driver returns StatusSucceeded with a valid StructuredVerdict
// JSON in Explanation. Outcome.Verdict is populated; Failure is nil.
func TestTranslateInvokeResult_SuccessVerdict(t *testing.T) {
	res := driver.Result{
		Status:      driver.StatusSucceeded,
		Explanation: `{"verdict":"approve","concerns":[],"recommendations":[],"blockers":[]}`,
	}
	out := translateInvokeResult("inv-1", "claudecode", verdict.RolePrimary, time.Now(), res, nil, nil)
	if out.Failure != nil {
		t.Fatalf("Failure = %+v, want nil", out.Failure)
	}
	if out.Verdict == nil || out.Verdict.Verdict != verdict.Approve {
		t.Errorf("Verdict mismatch: %+v", out.Verdict)
	}
	if out.InvocationID != "inv-1" || out.DriverID != "claudecode" {
		t.Errorf("metadata mismatch: %+v", out)
	}
}

// TestTranslateInvokeResult_MalformedJSON covers the parse-stage failure:
// driver returned StatusSucceeded but Explanation isn't valid JSON.
// Outcome.Failure.Kind = FailureMalformedJSON.
func TestTranslateInvokeResult_MalformedJSON(t *testing.T) {
	res := driver.Result{
		Status:      driver.StatusSucceeded,
		Explanation: `{verdict:"approve"`, // missing quotes
	}
	out := translateInvokeResult("inv-2", "codex", verdict.RolePrimary, time.Now(), res, nil, nil)
	if out.Failure == nil {
		t.Fatalf("Failure = nil, want set")
	}
	if out.Failure.Kind != verdict.FailureMalformedJSON {
		t.Errorf("Failure.Kind = %q, want %q", out.Failure.Kind, verdict.FailureMalformedJSON)
	}
	if out.Verdict != nil {
		t.Errorf("Verdict = %+v, want nil on failure", out.Verdict)
	}
}

// TestTranslateInvokeResult_MalformedShape covers the validate-stage
// failure: driver returned valid JSON but the verdict violates FR-014
// (approve with non-empty blockers).
func TestTranslateInvokeResult_MalformedShape(t *testing.T) {
	res := driver.Result{
		Status:      driver.StatusSucceeded,
		Explanation: `{"verdict":"approve","blockers":["should not exist"]}`,
	}
	out := translateInvokeResult("inv-3", "openclaw", verdict.RolePrimary, time.Now(), res, nil, nil)
	if out.Failure == nil {
		t.Fatalf("Failure = nil, want set")
	}
	if out.Failure.Kind != verdict.FailureMalformedShape {
		t.Errorf("Failure.Kind = %q, want %q", out.Failure.Kind, verdict.FailureMalformedShape)
	}
}

// TestTranslateInvokeResult_Timeout_ContextDeadline covers the timeout
// path triggered by ctx.DeadlineExceeded. Wins over invokeErr.
func TestTranslateInvokeResult_Timeout_ContextDeadline(t *testing.T) {
	res := driver.Result{Status: driver.StatusFailed, Explanation: "ctx canceled"}
	out := translateInvokeResult("inv-4", "claudecode", verdict.RoleArbiter, time.Now(),
		res, errors.New("ignored: ctx err takes priority"), context.DeadlineExceeded)
	if out.Failure == nil || out.Failure.Kind != verdict.FailureTimeout {
		t.Errorf("Failure = %+v, want FailureTimeout", out.Failure)
	}
}

// TestTranslateInvokeResult_Timeout_StatusTimeout covers the timeout path
// triggered by driver.StatusTimeout (driver self-reports timeout without
// ctx deadline firing — e.g., driver's internal tool-timeout is shorter).
func TestTranslateInvokeResult_Timeout_StatusTimeout(t *testing.T) {
	res := driver.Result{Status: driver.StatusTimeout, Explanation: "tool wait 60s"}
	out := translateInvokeResult("inv-5", "codex", verdict.RolePrimary, time.Now(), res, nil, nil)
	if out.Failure == nil || out.Failure.Kind != verdict.FailureTimeout {
		t.Errorf("Failure = %+v, want FailureTimeout", out.Failure)
	}
	if !strings.Contains(out.Failure.Detail, "tool wait") {
		t.Errorf("Detail = %q, want to preserve driver's explanation", out.Failure.Detail)
	}
}

// TestTranslateInvokeResult_InvokeError covers the FailureError path when
// Invoke returned a non-nil Go error (e.g., kernel gate rejected).
func TestTranslateInvokeResult_InvokeError(t *testing.T) {
	res := driver.Result{Status: driver.StatusUnknown} // typically zero on err
	out := translateInvokeResult("inv-6", "openclaw", verdict.RolePrimary, time.Now(),
		res, errors.New("kernel: review tool not in registry"), nil)
	if out.Failure == nil || out.Failure.Kind != verdict.FailureError {
		t.Errorf("Failure = %+v, want FailureError", out.Failure)
	}
	if !strings.Contains(out.Failure.Detail, "kernel:") {
		t.Errorf("Detail = %q, want to surface invoke error", out.Failure.Detail)
	}
}

// TestTranslateInvokeResult_StatusFailed covers the explicit-failure
// path: driver returned StatusFailed without a Go-level error.
func TestTranslateInvokeResult_StatusFailed(t *testing.T) {
	res := driver.Result{Status: driver.StatusFailed, Explanation: "prompt returned non-JSON"}
	out := translateInvokeResult("inv-7", "codex", verdict.RolePrimary, time.Now(), res, nil, nil)
	if out.Failure == nil || out.Failure.Kind != verdict.FailureError {
		t.Errorf("Failure = %+v, want FailureError", out.Failure)
	}
	if !strings.Contains(out.Failure.Detail, "prompt returned") {
		t.Errorf("Detail = %q, want explanation preserved", out.Failure.Detail)
	}
}

// TestTranslateInvokeResult_ElapsedMS confirms elapsed time is measured
// from the supplied startedAt, not from translate-time.
func TestTranslateInvokeResult_ElapsedMS(t *testing.T) {
	res := driver.Result{
		Status:      driver.StatusSucceeded,
		Explanation: `{"verdict":"approve"}`,
	}
	startedAt := time.Now().Add(-250 * time.Millisecond)
	out := translateInvokeResult("inv-8", "x", verdict.RolePrimary, startedAt, res, nil, nil)
	if out.ElapsedMS < 200 || out.ElapsedMS > 10_000 {
		t.Errorf("ElapsedMS = %d, want ~250 (>200, <10000)", out.ElapsedMS)
	}
}

// TestNonEmpty covers the small helper used to compose failure-detail
// fallbacks.
func TestNonEmpty(t *testing.T) {
	if got := nonEmpty("set", "default"); got != "set" {
		t.Errorf("non-empty input: got %q, want set", got)
	}
	if got := nonEmpty("", "default"); got != "default" {
		t.Errorf("empty input: got %q, want default", got)
	}
}
