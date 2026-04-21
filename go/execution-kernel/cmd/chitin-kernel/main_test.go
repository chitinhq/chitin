package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testBinary is the path to the chitin-kernel binary built by TestMain.
// Populated before any test runs; accessed from runCLI.
var testBinary string

// TestMain builds the chitin-kernel binary into a temp path and points
// every CLI test at it. This removes the implicit `go build` ordering
// dependency that would otherwise cause `go test ./...` to silently
// skip these tests on a fresh checkout.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "chitin-kernel-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: mkdir temp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	testBinary = filepath.Join(tmp, "chitin-kernel")
	build := exec.Command("go", "build", "-o", testBinary, ".")
	build.Stderr = os.Stderr
	build.Stdout = os.Stdout
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: go build failed:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// runCLI invokes the built chitin-kernel binary with the given args, inside
// the given working directory. Returns stdout, stderr, exit code.
func runCLI(t *testing.T, wd string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(testBinary, args...)
	cmd.Dir = wd
	stdout, err := cmd.Output()
	var stderr []byte
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	return string(stdout), string(stderr), cmd.ProcessState.ExitCode()
}

// fixturePathForCLI returns the SP-1 synthesized fixture absolute path.
func fixturePathForCLI(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "..",
		"docs", "observations", "fixtures",
		"2026-04-20-openclaw-otel-capture", "sp1",
		"synthesized-model-usage.pb"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// writeTemplate writes a minimal valid envelope template to the given path
// and returns the path.
func writeTemplate(t *testing.T, dir string) string {
	t.Helper()
	tmpl := map[string]any{
		"schema_version":    "2",
		"run_id":            "550e8400-e29b-41d4-a716-446655441000",
		"session_id":        "550e8400-e29b-41d4-a716-446655441001",
		"surface":           "openclaw-gateway",
		"agent_instance_id": "550e8400-e29b-41d4-a716-446655441002",
		"agent_fingerprint": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"chain_id":          "placeholder",
		"chain_type":        "session",
		"driver_identity": map[string]any{
			"user":                "red",
			"machine_id":          "chimera-ant",
			"machine_fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	b, _ := json.Marshal(tmpl)
	p := filepath.Join(dir, "template.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLI_IngestOTEL_ParseOnly(t *testing.T) {
	wd := t.TempDir()
	stdout, _, code := runCLI(t, wd,
		"ingest-otel",
		"--from", fixturePathForCLI(t),
		"--dialect", "openclaw",
	)
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, stdout)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("unmarshal stdout: %v", err)
	}
	if out["ok"] != true {
		t.Errorf("ok: %v", out["ok"])
	}
	if _, ok := out["turns"]; !ok {
		t.Error("turns absent from parse-only output")
	}
}

func TestCLI_IngestOTEL_EmitMode(t *testing.T) {
	wd := t.TempDir()
	tmplPath := writeTemplate(t, wd)
	stdout, _, code := runCLI(t, wd,
		"ingest-otel",
		"--from", fixturePathForCLI(t),
		"--dialect", "openclaw",
		"--envelope-template", tmplPath,
		"--dir", filepath.Join(wd, ".chitin"),
	)
	if code != 0 {
		t.Fatalf("exit %d, stdout=%s", code, stdout)
	}
	entries, err := os.ReadDir(filepath.Join(wd, ".chitin"))
	if err != nil {
		t.Fatalf("read .chitin: %v", err)
	}
	var jsonl string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			jsonl = filepath.Join(wd, ".chitin", e.Name())
		}
	}
	if jsonl == "" {
		t.Fatalf("no events JSONL produced under %s: %+v", filepath.Join(wd, ".chitin"), entries)
	}
	data, _ := os.ReadFile(jsonl)
	if len(data) == 0 {
		t.Fatal("events JSONL empty")
	}
}

func TestCLI_IngestOTEL_UnsupportedDialect(t *testing.T) {
	_, stderr, code := runCLI(t, t.TempDir(),
		"ingest-otel",
		"--from", fixturePathForCLI(t),
		"--dialect", "gen_ai",
	)
	if code == 0 {
		t.Fatal("want non-zero exit for unsupported dialect")
	}
	if !strings.Contains(stderr, `"error":"unsupported_dialect"`) {
		t.Errorf(`want stderr to contain "error":"unsupported_dialect", got %q`, stderr)
	}
}

func TestCLI_IngestOTEL_MalformedProtobuf(t *testing.T) {
	wd := t.TempDir()
	bad := filepath.Join(wd, "bad.pb")
	_ = os.WriteFile(bad, []byte("absolutely not a protobuf"), 0o644)
	_, stderr, code := runCLI(t, wd, "ingest-otel", "--from", bad, "--dialect", "openclaw")
	if code == 0 {
		t.Fatal("want non-zero exit for malformed input")
	}
	if !strings.Contains(stderr, `"error":"otlp_decode_failed"`) {
		t.Errorf(`want stderr to contain "error":"otlp_decode_failed", got %q`, stderr)
	}
}
