package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCommand(t *testing.T) {
	cases := []struct {
		runtime  string
		module   string
		wantCmd  string
		wantArg0 string
	}{
		{"python3", "/p.py", "python3", "-u"},
		{"python", "/p.py", "python3", "-u"},
		{"node", "/p.ts", "node", "--experimental-strip-types"},
		{"bun", "/p.ts", "bun", "run"},
		{"bash", "/p.sh", "/p.sh", ""},
	}
	for _, c := range cases {
		cmd, args, err := runtimeCommand(c.runtime, c.module)
		if err != nil {
			t.Errorf("runtimeCommand(%q): %v", c.runtime, err)
			continue
		}
		if cmd != c.wantCmd {
			t.Errorf("runtimeCommand(%q) cmd=%q want %q", c.runtime, cmd, c.wantCmd)
		}
		if c.wantArg0 != "" && (len(args) == 0 || args[0] != c.wantArg0) {
			t.Errorf("runtimeCommand(%q) args[0]=%v want %q", c.runtime, args, c.wantArg0)
		}
	}
}

func TestRuntimeCommand_Unsupported(t *testing.T) {
	_, _, err := runtimeCommand("rust", "/p.rs")
	if err == nil {
		t.Error("expected error for unsupported runtime")
	}
}

func TestRun_BashEcho(t *testing.T) {
	// Write a tiny shell script that emits a fixed plugin output
	dir := t.TempDir()
	script := filepath.Join(dir, "echo-plugin.sh")
	body := `#!/usr/bin/env bash
read -r line  # consume stdin
echo '{"score":0.42,"fired":false,"reason":"smoke-bash-ok"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{
		Name: "smoke-bash", Type: "heuristic",
		Runtime: "bash", Module: script, TimeoutMs: 2000,
	}
	out, err := Run(context.Background(), manifest, map[string]interface{}{
		"tool_name":  "Read",
		"tool_input": map[string]interface{}{"file_path": "/tmp/x"},
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Score != 0.42 {
		t.Errorf("score=%v want 0.42", out.Score)
	}
	if out.Reason != "smoke-bash-ok" {
		t.Errorf("reason=%q want smoke-bash-ok", out.Reason)
	}
}

func TestRun_BashTimeout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "slow-plugin.sh")
	body := `#!/usr/bin/env bash
sleep 10
echo '{"score":0,"fired":false,"reason":"too-late"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{
		Name: "slow", Type: "heuristic",
		Runtime: "bash", Module: script, TimeoutMs: 200,
	}
	_, err := Run(context.Background(), manifest, map[string]interface{}{}, nil)
	if err == nil {
		t.Error("expected timeout error; got nil")
	}
}

func TestRun_BashMalformedOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "bad-plugin.sh")
	body := `#!/usr/bin/env bash
echo "not-json-at-all"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{
		Name: "bad", Type: "heuristic",
		Runtime: "bash", Module: script, TimeoutMs: 2000,
	}
	_, err := Run(context.Background(), manifest, map[string]interface{}{}, nil)
	if err == nil {
		t.Error("expected parse error; got nil")
	}
}
