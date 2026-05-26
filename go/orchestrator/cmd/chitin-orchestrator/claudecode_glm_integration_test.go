//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCodeGLMWholeSpecDispatchIntegration(t *testing.T) {
	if os.Getenv("CHITIN_SPEC120_INTEGRATION") != "1" {
		t.Skip("set CHITIN_SPEC120_INTEGRATION=1 with Temporal, ollama v0.21+, claude CLI, and glm-5.1 pulled")
	}
	t.Setenv("CHITIN_DRIVER_ALLOW", "claudecode-glm")

	var stdout, stderr bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--whole-spec",
		"--repo-root", repoRootForIntegration(t),
		"120-claudecode-glm-driver",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("schedule exit=%d\nstdout=%s\nstderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "scheduled spec 120-claudecode-glm-driver") {
		t.Fatalf("unexpected schedule output: %s", stdout.String())
	}
}

func repoRootForIntegration(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}
