package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRun_BlockFlag verifies a pre-action analysis plugin's
// block:true flag round-trips through the loader.
func TestRun_BlockFlag(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "blocker.sh")
	body := `#!/usr/bin/env bash
read -r line  # consume stdin
echo '{"score":1.0,"fired":true,"block":true,"reason":"test-block-fired"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{
		Name: "blocker", Type: "heuristic",
		Runtime: "bash", Module: script, TimeoutMs: 2000,
	}
	out, err := Run(context.Background(), manifest, map[string]interface{}{
		"tool_name": "Bash",
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !out.Block {
		t.Errorf("Block=%v want true", out.Block)
	}
	if !out.Fired {
		t.Errorf("Fired=%v want true", out.Fired)
	}
	if out.Reason != "test-block-fired" {
		t.Errorf("Reason=%q want test-block-fired", out.Reason)
	}
}

// TestRun_NoBlockField — backward compat: plugins emitting the
// pre-Block schema still work; Block defaults to false.
func TestRun_NoBlockField(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "legacy.sh")
	body := `#!/usr/bin/env bash
read -r line
echo '{"score":0.7,"fired":true,"reason":"legacy-heuristic"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := PluginManifest{
		Name: "legacy", Type: "heuristic",
		Runtime: "bash", Module: script, TimeoutMs: 2000,
	}
	out, err := Run(context.Background(), manifest, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Block {
		t.Errorf("Block=%v want false (no field present)", out.Block)
	}
	if !out.Fired {
		t.Error("Fired=false; want true")
	}
}
