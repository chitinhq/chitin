package gemini

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestNormalize_ShellExecReclassifies(t *testing.T) {
	a, err := Normalize(HookInput{
		ToolName:  "run_shell_command",
		ToolInput: map[string]any{"command": "rm -rf /tmp/foo"},
		Cwd:       "/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Type == gov.ActUnknown {
		t.Errorf("rm -rf should reclassify, got %s", a.Type)
	}
	if a.Path != "/work" {
		t.Errorf("Path=%q want /work", a.Path)
	}
}

func TestNormalize_FileRead(t *testing.T) {
	cases := []struct {
		name       string
		toolName   string
		input      map[string]any
		wantTarget string
	}{
		{"read_file", "read_file", map[string]any{"file_path": "/tmp/x"}, "/tmp/x"},
		{"list_directory", "list_directory", map[string]any{"path": "/tmp"}, "/tmp"},
		{"glob", "glob", map[string]any{"pattern": "**/*.go"}, "**/*.go"},
		{"search_file_content", "search_file_content", map[string]any{"pattern": "TODO"}, "TODO"},
		{"list_background_processes", "list_background_processes", map[string]any{}, "list_background_processes"},
		{"read_background_output", "read_background_output", map[string]any{}, "read_background_output"},
	}
	for _, tc := range cases {
		a, err := Normalize(HookInput{ToolName: tc.toolName, ToolInput: tc.input})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if a.Type != gov.ActFileRead {
			t.Errorf("%s: type=%s want file.read", tc.name, a.Type)
		}
		if a.Target != tc.wantTarget {
			t.Errorf("%s: target=%q want %q", tc.name, a.Target, tc.wantTarget)
		}
	}
}

func TestNormalize_ReadManyFilesPicksFirst(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "read_many_files",
		ToolInput: map[string]any{"paths": []any{"/tmp/a", "/tmp/b", "/tmp/c"}},
	})
	if a.Type != gov.ActFileRead {
		t.Fatalf("type=%s want file.read", a.Type)
	}
	if a.Target != "/tmp/a" {
		t.Errorf("target=%q want first path /tmp/a", a.Target)
	}
}

func TestNormalize_FileWrite(t *testing.T) {
	cases := []struct {
		name       string
		toolName   string
		input      map[string]any
		wantTarget string
	}{
		{"edit", "edit", map[string]any{"file_path": "/tmp/x.go"}, "/tmp/x.go"},
		{"replace", "replace", map[string]any{"file_path": "/tmp/y"}, "/tmp/y"},
		{"write_file", "write_file", map[string]any{"file_path": "/tmp/z"}, "/tmp/z"},
	}
	for _, tc := range cases {
		a, _ := Normalize(HookInput{ToolName: tc.toolName, ToolInput: tc.input})
		if a.Type != gov.ActFileWrite {
			t.Errorf("%s: type=%s want file.write", tc.name, a.Type)
		}
		if a.Target != tc.wantTarget {
			t.Errorf("%s: target=%q want %q", tc.name, a.Target, tc.wantTarget)
		}
	}
}

func TestNormalize_HTTP(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "web_fetch",
		ToolInput: map[string]any{"url": "https://example.com"},
	})
	if a.Type != gov.ActHTTPRequest {
		t.Errorf("type=%s want http.request", a.Type)
	}
	if a.Target != "https://example.com" {
		t.Errorf("target=%q want url", a.Target)
	}
	b, _ := Normalize(HookInput{
		ToolName:  "google_web_search",
		ToolInput: map[string]any{"query": "chitin governance"},
	})
	if b.Type != gov.ActHTTPRequest {
		t.Errorf("type=%s want http.request", b.Type)
	}
	if b.Target != "google_web_search:chitin governance" {
		t.Errorf("target=%q does not include query", b.Target)
	}
}

func TestNormalize_SaveMemoryAndUpdateTopic(t *testing.T) {
	a, _ := Normalize(HookInput{ToolName: "save_memory", ToolInput: map[string]any{"content": "..."}})
	if a.Type != gov.ActFileWrite || a.Target != "memory" {
		t.Errorf("save_memory: %+v want type=file.write target=memory", a)
	}
	b, _ := Normalize(HookInput{ToolName: "update_topic", ToolInput: map[string]any{"summary": "..."}})
	if b.Type != gov.ActFileWrite || b.Target != "topic" {
		t.Errorf("update_topic: %+v want type=file.write target=topic", b)
	}
}

func TestNormalize_UnknownToolFailsClosed(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "future_unreleased_gemini_tool",
		ToolInput: map[string]any{"x": 1},
	})
	if a.Type != gov.ActUnknown {
		t.Fatalf("type=%s want ActUnknown", a.Type)
	}
	// Params preserved so the audit log captures what we couldn't classify.
	if a.Params == nil {
		t.Errorf("Params should preserve raw input")
	}
}

func TestNormalize_MissingFieldYieldsEmptyTarget(t *testing.T) {
	a, _ := Normalize(HookInput{ToolName: "read_file", ToolInput: map[string]any{}})
	if a.Type != gov.ActFileRead {
		t.Fatalf("type=%s want file.read", a.Type)
	}
	if a.Target != "" {
		t.Errorf("target=%q want empty", a.Target)
	}
}
