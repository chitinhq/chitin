package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKernelBin writes a tiny bash script that captures stdin to a file
// alongside it (sentinel.bin/sentinel.json) and exits 0. Used to verify
// the emit subcommand was actually invoked with the expected JSON.
func fakeKernelBin(t *testing.T, exitCode int) (binPath, sentinelPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "chitin-kernel")
	sentinelPath = filepath.Join(dir, "captured.json")
	script := "#!/usr/bin/env bash\n" +
		"set -e\n" +
		"# args expected: emit -event-json -\n" +
		"cat > " + sentinelPath + "\n" +
		"exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("setup fake kernel: %v", err)
	}
	return binPath, sentinelPath
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(buf[i:])
	if neg {
		s = "-" + s
	}
	return s
}

func TestEmitSchedulerStarted_WritesExpectedJSON(t *testing.T) {
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	var stderr bytes.Buffer
	emitSchedulerStarted(context.Background(), SchedulerStartedPayload{
		SpecRef:              "097-fixture",
		RunID:                "abc-123",
		NodeCount:            3,
		CapabilitiesRequired: []string{"code.implement"},
	}, &stderr)

	if stderr.Len() > 0 {
		t.Errorf("expected silent emit on success, got stderr: %s", stderr.String())
	}

	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("reading sentinel: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid JSON written to kernel emit: %v\n%s", err, body)
	}
	if got["event_type"] != "scheduler_started" {
		t.Errorf("event_type = %v, want scheduler_started", got["event_type"])
	}
	payload, ok := got["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload not an object: %T", got["payload"])
	}
	if payload["spec_ref"] != "097-fixture" {
		t.Errorf("payload.spec_ref = %v", payload["spec_ref"])
	}
	if payload["run_id"] != "abc-123" {
		t.Errorf("payload.run_id = %v", payload["run_id"])
	}
	if payload["node_count"] != float64(3) { // JSON numbers decode as float64
		t.Errorf("payload.node_count = %v (%T)", payload["node_count"], payload["node_count"])
	}
}

func TestEmitSchedulerCanceled_WritesExpectedJSON(t *testing.T) {
	bin, sentinel := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	var stderr bytes.Buffer
	emitSchedulerCanceled(context.Background(), SchedulerCanceledPayload{
		RunID:  "abc-123",
		Reason: "operator abort",
	}, &stderr)
	if stderr.Len() > 0 {
		t.Errorf("expected silent emit on success, got stderr: %s", stderr.String())
	}

	body, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("reading sentinel: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, body)
	}
	if got["event_type"] != "scheduler_canceled" {
		t.Errorf("event_type = %v, want scheduler_canceled", got["event_type"])
	}
	payload := got["payload"].(map[string]any)
	if payload["reason"] != "operator abort" {
		t.Errorf("payload.reason = %v", payload["reason"])
	}
}

func TestEmit_KernelBinaryMissing_LogsWarnReturnsNothing(t *testing.T) {
	// Point at a definitely-nonexistent binary.
	t.Setenv("CHITIN_KERNEL_BIN", filepath.Join(t.TempDir(), "nope-not-here"))

	var stderr bytes.Buffer
	emitSchedulerStarted(context.Background(), SchedulerStartedPayload{
		SpecRef: "fixture", RunID: "x", NodeCount: 1, CapabilitiesRequired: []string{"code.implement"},
	}, &stderr)

	got := stderr.String()
	if !strings.Contains(got, "warning: chain emit failed") {
		t.Errorf("expected warning on missing kernel binary, got: %q", got)
	}
	if !strings.Contains(got, "scheduler_started") {
		t.Errorf("warning should mention the event type, got: %q", got)
	}
	// Critical: the function returned (didn't panic, didn't exit).
}

func TestEmit_KernelExitsNonZero_LogsWarn(t *testing.T) {
	bin, _ := fakeKernelBin(t, 1) // exit 1
	t.Setenv("CHITIN_KERNEL_BIN", bin)

	var stderr bytes.Buffer
	emitSchedulerStarted(context.Background(), SchedulerStartedPayload{
		SpecRef: "fixture", RunID: "x", NodeCount: 1, CapabilitiesRequired: []string{"code.implement"},
	}, &stderr)
	if !strings.Contains(stderr.String(), "warning: chain emit failed") {
		t.Errorf("expected warning on non-zero exit, got: %q", stderr.String())
	}
}
