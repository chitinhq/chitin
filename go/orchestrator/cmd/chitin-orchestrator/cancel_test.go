package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
)

func TestRunCancel_RunIDRequired(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runCancel(context.Background(), nil, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "-run-id is required") {
		t.Errorf("stderr should require -run-id; got: %q", errBuf.String())
	}
}

func TestRunCancel_PositionalArgsRejected(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runCancel(context.Background(), []string{"hello"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit = %d, want %d", code, exitUserError)
	}
}

func TestRunCancel_TemporalUnreachable(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runCancel(context.Background(), []string{
		"--temporal-host", "127.0.0.1:1",
		"-run-id", "abc",
	}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("exit = %d, want %d; stderr=%q", code, exitRuntimeError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("stderr should report Temporal unreachable; got: %q", errBuf.String())
	}
}

func TestIsTerminalStatus(t *testing.T) {
	terminals := []enumspb.WorkflowExecutionStatus{
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
	}
	for _, s := range terminals {
		if !isTerminalStatus(s) {
			t.Errorf("isTerminalStatus(%v) = false, want true", s)
		}
	}
	nonTerminals := []enumspb.WorkflowExecutionStatus{
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
	}
	for _, s := range nonTerminals {
		if isTerminalStatus(s) {
			t.Errorf("isTerminalStatus(%v) = true, want false", s)
		}
	}
}

func TestTerminalStatusName(t *testing.T) {
	cases := []struct {
		in   enumspb.WorkflowExecutionStatus
		want string
	}{
		{enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, "Completed"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_FAILED, "Failed"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED, "Canceled"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, "Terminated"},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, "TimedOut"},
	}
	for _, tc := range cases {
		got := terminalStatusName(tc.in)
		if got != tc.want {
			t.Errorf("terminalStatusName(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
