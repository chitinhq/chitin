package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarize_NoEvents(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".chitin"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Summarize("missing-session")
	if err == nil {
		t.Error("expected error for missing session")
	}
}

func TestSummarize_BasicShape(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sid := "test-session-001"
	jsonl := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"Read","action_target":"apps/foo/bar.ts","decision":"allow","rule_id":"default-allow-reads"}}
{"ts":"2026-05-03T10:00:30Z","event_type":"decision","payload":{"tool_name":"Edit","action_target":"apps/foo/bar.ts","decision":"allow","rule_id":"default-allow-file-write"}}
{"ts":"2026-05-03T10:01:00Z","event_type":"decision","payload":{"tool_name":"Bash","action_target":"rm -rf /tmp","decision":"deny","rule_id":"no-rm-recursive"}}
`
	path := filepath.Join(chitinDir, "events-"+sid+".jsonl")
	if err := os.WriteFile(path, []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Summarize(sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"3 tool calls",
		"1 denied",
		"apps/foo/bar.ts",
		"Last decision",
		"deny",
		"no-rm-recursive",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Summarize output missing %q\nout:\n%s", want, out)
		}
	}
}

func TestFindRelatedSessions_NoMatches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".chitin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRelatedSessions("nonexistent-entry", []string{"apps/x/"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %v; want empty (no chain files)", got)
	}
}

func TestFindRelatedSessions_EntryIDSubstring(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Sessions with entry_id pattern: swarm-<entry-id>-<ts>
	sessions := []string{
		"swarm-foo-bar-1777800000",
		"swarm-baz-1777800001",
		"swarm-foo-bar-1777800002",
	}
	for _, sid := range sessions {
		p := filepath.Join(chitinDir, "events-"+sid+".jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FindRelatedSessions("foo-bar", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d matches; want 2 (foo-bar substring; got %v)", len(got), got)
	}
}

func TestFindRelatedSessions_FilePathOverlap(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	chitinDir := filepath.Join(tmp, ".chitin")
	if err := os.MkdirAll(chitinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Session 1: touches apps/foo/bar.ts
	jsonl1 := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"Edit","action_target":"apps/foo/bar.ts","decision":"allow"}}`
	// Session 2: touches libs/baz/qux.ts (no match)
	jsonl2 := `{"ts":"2026-05-03T10:00:00Z","event_type":"decision","payload":{"tool_name":"Edit","action_target":"libs/baz/qux.ts","decision":"allow"}}`
	for sid, body := range map[string]string{"sess-aaa": jsonl1, "sess-bbb": jsonl2} {
		p := filepath.Join(chitinDir, "events-"+sid+".jsonl")
		if err := os.WriteFile(p, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FindRelatedSessions("", []string{"apps/foo/"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d matches; want 1 (path overlap; got %v)", len(got), got)
	}
	if len(got) == 1 && got[0] != "sess-aaa" {
		t.Errorf("got %v; want [sess-aaa]", got)
	}
}
