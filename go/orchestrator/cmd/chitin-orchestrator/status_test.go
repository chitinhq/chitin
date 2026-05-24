package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunStatus_TemporalUnreachable(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runStatus(context.Background(), []string{"--temporal-host", "127.0.0.1:1"}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("exit = %d, want %d; stderr=%q", code, exitRuntimeError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("stderr should report Temporal unreachable; got: %q", errBuf.String())
	}
}

func TestRunStatus_PositionalArgsRejected(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runStatus(context.Background(), []string{"hello"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "no positional") {
		t.Errorf("stderr should reject positional args; got: %q", errBuf.String())
	}
}

func TestRunStatus_UnknownFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runStatus(context.Background(), []string{"--bogus"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit = %d, want %d", code, exitUserError)
	}
}

func TestTrunc(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 10, "abc"},
		{"abcdef", 4, "abc…"},
		{"x", 1, "x"},
		{"xyz", 1, "…"},
	}
	for _, tc := range cases {
		got := trunc(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("trunc(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
