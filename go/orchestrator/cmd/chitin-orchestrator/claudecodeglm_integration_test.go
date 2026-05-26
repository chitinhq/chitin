//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver/claudecodeglm"
)

func TestIntegrationScheduleWholeSpecClaudeCodeGLMEmitsDriverID(t *testing.T) {
	requireOnPath(t, "ollama")
	requireOnPath(t, "claude")
	if ready, reason := claudecodeglm.New().Ready(context.Background()); !ready {
		t.Skipf("claudecode-glm not ready: %s", reason)
	}

	repo := fixtureRepo(t, "120-glm-integration", "- [ ] T001 [US1] Implement a tiny fixture change")
	t.Setenv("CHITIN_DRIVER_ALLOW", "claudecode-glm")

	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--temporal-host", getenvDefault("CHITIN_INTEGRATION_TEMPORAL_HOST", "127.0.0.1:7233"),
		"120",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("schedule exit=%d; stdout=%q stderr=%q", code, out.String(), errBuf.String())
	}

	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read emitted scheduler_started event: %v", err)
	}
	var event struct {
		EventType string `json:"event_type"`
		Payload   struct {
			DriverID string `json:"driver_id"`
			Mode     string `json:"mode"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("unmarshal event: %v\n%s", err, body)
	}
	if event.EventType != "scheduler_started" {
		t.Fatalf("event_type=%q, want scheduler_started", event.EventType)
	}
	if event.Payload.Mode != "whole-spec" {
		t.Fatalf("mode=%q, want whole-spec", event.Payload.Mode)
	}
	if event.Payload.DriverID != "claudecode-glm" {
		t.Fatalf("driver_id=%q, want claudecode-glm", event.Payload.DriverID)
	}
}

func requireOnPath(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(filepath.Base(name)); err != nil {
		t.Skipf("%s not available on PATH: %v", name, err)
	}
}

func getenvDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
