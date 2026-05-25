package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunQueue_DefaultsAndRepoFromEnv(t *testing.T) {
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")
	var out, errBuf bytes.Buffer

	code := runQueue(context.Background(), nil, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "spec 114 T002-T008") {
		t.Errorf("stderr missing stub marker; got %q", errBuf.String())
	}
}

func TestRunQueue_MissingRepoErrors(t *testing.T) {
	t.Setenv("CHITIN_REPO", "")
	var out, errBuf bytes.Buffer

	code := runQueue(context.Background(), nil, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "$CHITIN_REPO") {
		t.Errorf("stderr should name the env var; got %q", errBuf.String())
	}
}

func TestRunQueue_InvalidSinceErrors(t *testing.T) {
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")
	var out, errBuf bytes.Buffer

	code := runQueue(context.Background(), []string{"--since", "not-a-duration"}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "--since") {
		t.Errorf("stderr should name --since; got %q", errBuf.String())
	}
}

func TestRunQueue_FlagOverrideBeatsEnv(t *testing.T) {
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")
	var out, errBuf bytes.Buffer

	code := runQueue(context.Background(), []string{
		"--repo", "other/repo",
		"--since", "24h",
		"--format", "json",
		"--reason", "sibling_rebase_failed",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}
}

func TestRunQueue_RejectsPositionalArgs(t *testing.T) {
	t.Setenv("CHITIN_REPO", "chitinhq/chitin")
	var out, errBuf bytes.Buffer

	code := runQueue(context.Background(), []string{"unexpected"}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "unexpected") {
		t.Errorf("stderr should name the unexpected arg; got %q", errBuf.String())
	}
}
