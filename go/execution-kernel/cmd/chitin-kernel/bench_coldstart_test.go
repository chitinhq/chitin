package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// TestColdStart_HookStdinLatency is the spec acceptance gate: build the
// chitin-kernel binary, exec it 100 times against a fixture PreToolUse
// payload via `gate evaluate --hook-stdin`, measure wall-clock latency,
// report p50/p95/p99 to stderr, and decide:
//
//	p95 ≤ 100ms  → ship cold-start; daemon mode not needed.
//	p95 >  100ms → daemon mode is required for the hook driver to be
//	               usable in interactive sessions; design that next.
//
// This test runs full subprocess cold-starts — sqlite open, policy
// load, action normalize, gate evaluate, decision log write — exactly
// what Claude Code triggers on each tool call. It is intentionally
// gated behind COLDSTART=1 so unit-test runs aren't slow; CI and the
// operator's "did we earn the cold-start ship decision" check enable it.
//
// 100 iterations is enough to get stable p95/p99 on the operator's box
// without making test runs interminable. Adjust COLDSTART_ITERS if more
// precision is needed for the daemon-mode call.
func TestColdStart_HookStdinLatency(t *testing.T) {
	if os.Getenv("COLDSTART") != "1" {
		t.Skip("COLDSTART=1 not set; skipping cold-start benchmark")
	}
	iters := 100
	if v := os.Getenv("COLDSTART_ITERS"); v != "" {
		fmt.Sscanf(v, "%d", &iters)
	}

	binary := buildKernelBinary(t)
	cwd := stagePolicyDir(t)
	chitinHome := t.TempDir()

	payload := map[string]any{
		"tool_name":  "Read",
		"tool_input": map[string]any{"file_path": "/etc/hosts"},
		"cwd":        cwd,
	}
	body, _ := json.Marshal(payload)

	// Warm-up: 3 ignored runs so OS file cache + sqlite WAL setup don't
	// inflate the first measurements. The acceptance gate is steady-
	// state cold-start, not first-run cold-start.
	for i := 0; i < 3; i++ {
		_ = runOnce(t, binary, chitinHome, body)
	}

	durations := make([]time.Duration, iters)
	for i := 0; i < iters; i++ {
		durations[i] = runOnce(t, binary, chitinHome, body)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[iters*50/100]
	p95 := durations[iters*95/100]
	p99 := durations[iters*99/100]

	verdict := "ship cold-start (p95 ≤ 100ms)"
	if p95 > 100*time.Millisecond {
		verdict = "BUILD DAEMON MODE (p95 > 100ms)"
	}

	report := map[string]any{
		"iters":   iters,
		"p50_ms":  p50.Milliseconds(),
		"p95_ms":  p95.Milliseconds(),
		"p99_ms":  p99.Milliseconds(),
		"min_ms":  durations[0].Milliseconds(),
		"max_ms":  durations[iters-1].Milliseconds(),
		"verdict": verdict,
	}
	rb, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("cold-start latency report:\n%s", rb)

	if p95 > 100*time.Millisecond {
		t.Logf("DECISION: cold-start p95=%v exceeds 100ms gate; design daemon mode (gate.sock listener) before shipping the hook driver.", p95)
	}
}

func runOnce(t *testing.T, binary, chitinHome string, payload []byte) time.Duration {
	t.Helper()
	cmd := exec.Command(binary, "gate", "evaluate", "--hook-stdin", "--agent=claude-code")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "CHITIN_HOME="+chitinHome)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	d := time.Since(start)
	if err != nil {
		// Exit 2 (block) is "successful failure" for some scenarios but
		// the fixture is deliberately Read on /etc/hosts under an
		// allow-read policy — the run should always be exit 0.
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 0 {
			t.Fatalf("hook run failed: %v\noutput=%s", err, out)
		}
	}
	return d
}

func buildKernelBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "chitin-kernel")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/chitin-kernel")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build chitin-kernel: %v\n%s", err, out)
	}
	return bin
}

func stagePolicyDir(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	policy := `id: bench
mode: enforce
rules:
  - id: allow-read
    action: file.read
    effect: allow
    reason: read ok
`
	if err := os.WriteFile(filepath.Join(cwd, "chitin.yaml"), []byte(policy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return cwd
}

// repoRoot finds the go-module root by walking up from cwd until a
// go.mod is seen. Lets the test build the binary regardless of where
// `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			t.Fatalf("no go.mod up from %s", cwd)
		}
		cwd = parent
	}
}
