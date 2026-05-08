package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCLI_DecisionsRecent_EmptyDir: a freshly initialized chitin home
// (no jsonl files yet) must yield "[]" — never "null" — so MCP-bridge
// readers can iterate without an empty-result branch.
func TestCLI_DecisionsRecent_EmptyDir(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, code := runCLIWithHome(t, home, "decisions", "recent")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("want '[]', got %q", stdout)
	}
}

// TestCLI_DecisionsRecent_HappyPath: write two in-window decisions to a
// jsonl file, run the CLI, expect them returned newest-first as JSON.
// This is the contract the deleted libs/mcp-chitin/decisionsRecentTool
// previously satisfied; replicating it at the kernel CLI surface lets
// substrate MCP servers register `chitin-kernel decisions recent` as a
// drop-in tool.
func TestCLI_DecisionsRecent_HappyPath(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC()
	older := now.Add(-30 * time.Minute).Format(time.RFC3339)
	newer := now.Add(-5 * time.Minute).Format(time.RFC3339)
	date := now.Format("2006-01-02")

	lines := []string{
		`{"allowed":true,"mode":"enforce","rule_id":"older","action_type":"test","action_target":"/tmp/o","ts":"` + older + `"}`,
		`{"allowed":true,"mode":"enforce","rule_id":"newer","action_type":"test","action_target":"/tmp/n","ts":"` + newer + `"}`,
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(home, "gov-decisions-"+date+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLIWithHome(t, home,
		"decisions", "recent", "--window-hours", "1", "--limit", "10")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse stdout: %v\nstdout=%s", err, stdout)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 decisions, got %d: %v", len(got), got)
	}
	if got[0]["rule_id"] != "newer" || got[1]["rule_id"] != "older" {
		t.Fatalf("wrong order: got %v", got)
	}
}

// TestCLI_DecisionsRecent_RejectsBadFlags: window=0 and limit=0 must
// fail loud (not silently pretend success with no results). Documented
// boundary — these flags are guarded explicitly in cmdDecisionsRecent.
func TestCLI_DecisionsRecent_RejectsBadFlags(t *testing.T) {
	home := t.TempDir()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "zero window-hours",
			args: []string{"decisions", "recent", "--window-hours", "0"},
			want: "decisions_invalid_window_hours",
		},
		{
			name: "zero limit",
			args: []string{"decisions", "recent", "--limit", "0"},
			want: "decisions_invalid_limit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLIWithHome(t, home, tc.args...)
			if code == 0 {
				t.Fatal("want non-zero exit")
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("want stderr to contain %q, got %q", tc.want, stderr)
			}
		})
	}
}

// TestCLI_DecisionsRecent_UnknownSubcommand: chitin-kernel surfaces a
// distinct error kind for unknown subcommands so callers can branch.
func TestCLI_DecisionsRecent_UnknownSubcommand(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runCLIWithHome(t, home, "decisions", "bogus")
	if code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(stderr, "decisions_unknown_subcommand") {
		t.Fatalf("want decisions_unknown_subcommand error, got %q", stderr)
	}
}
