package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runCLI invokes the built chitin-kernel binary with the given args, inside
// the given working directory. Returns stdout, stderr, exit code.
func runCLI(t *testing.T, wd string, args ...string) (string, string, int) {
	t.Helper()
	bin, err := filepath.Abs(filepath.Join("..", "..", "chitin-kernel"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		t.Skipf("binary not built at %s; run `go build -o chitin-kernel ./cmd/chitin-kernel` first", bin)
	}
	cmd := exec.Command(bin, args...)
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
	_, _, code := runCLI(t, t.TempDir(),
		"ingest-otel",
		"--from", fixturePathForCLI(t),
		"--dialect", "gen_ai",
	)
	if code == 0 {
		t.Fatal("want non-zero exit for unsupported dialect")
	}
}

func TestCLI_IngestOTEL_MalformedProtobuf(t *testing.T) {
	wd := t.TempDir()
	bad := filepath.Join(wd, "bad.pb")
	_ = os.WriteFile(bad, []byte("absolutely not a protobuf"), 0o644)
	_, _, code := runCLI(t, wd, "ingest-otel", "--from", bad, "--dialect", "openclaw")
	if code == 0 {
		t.Fatal("want non-zero exit for malformed input")
	}
}
