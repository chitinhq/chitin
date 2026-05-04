package codex

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestNormalize_BashReclassifies(t *testing.T) {
	a, err := Normalize(HookInput{
		ToolName:  "Bash",
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

func TestNormalize_ApplyPatchExtractsFirstPath(t *testing.T) {
	patchInput := `*** Begin Patch
*** Update File: src/main.go
@@ -1 +1 @@
-old
+new
*** End Patch`
	a, _ := Normalize(HookInput{
		ToolName:  "apply_patch",
		ToolInput: map[string]any{"input": patchInput},
		Cwd:       "/work",
	})
	if a.Type != gov.ActFileWrite {
		t.Errorf("Type=%s want file.write", a.Type)
	}
	if a.Target != "src/main.go" {
		t.Errorf("Target=%q want src/main.go", a.Target)
	}
}

func TestNormalize_ApplyPatchAddFile(t *testing.T) {
	patchInput := `*** Begin Patch
*** Add File: docs/new.md
+content
*** End Patch`
	a, _ := Normalize(HookInput{
		ToolName:  "apply_patch",
		ToolInput: map[string]any{"input": patchInput},
	})
	if a.Target != "docs/new.md" {
		t.Errorf("Target=%q want docs/new.md", a.Target)
	}
}

func TestNormalize_ApplyPatchEmptyInputYieldsEmptyTarget(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "apply_patch",
		ToolInput: map[string]any{},
	})
	// Type is still file.write so policy can match action_type
	// alone; target empty when not parseable.
	if a.Type != gov.ActFileWrite {
		t.Errorf("Type=%s want file.write", a.Type)
	}
	if a.Target != "" {
		t.Errorf("Target=%q want empty (no parseable file path)", a.Target)
	}
}

func TestNormalize_ReadFile(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "read_file",
		ToolInput: map[string]any{"file_path": "/tmp/x"},
	})
	if a.Type != gov.ActFileRead {
		t.Errorf("Type=%s want file.read", a.Type)
	}
	if a.Target != "/tmp/x" {
		t.Errorf("Target=%q want /tmp/x", a.Target)
	}
}

func TestNormalize_MCPToolRoutesToMCPCall(t *testing.T) {
	cases := []struct {
		name       string
		toolName   string
		wantTarget string
	}{
		{"server+tool", "mcp__github__create_pull_request", "github/create_pull_request"},
		{"underscore_in_tool", "mcp__filesystem__read_file__binary", "filesystem/read_file__binary"},
		{"server_only", "mcp__some-server", "some-server"},
	}
	for _, tc := range cases {
		a, _ := Normalize(HookInput{
			ToolName:  tc.toolName,
			ToolInput: map[string]any{"x": 1},
		})
		if a.Type != gov.ActMCPCall {
			t.Errorf("%s: Type=%s want mcp.call", tc.name, a.Type)
		}
		if a.Target != tc.wantTarget {
			t.Errorf("%s: Target=%q want %q", tc.name, a.Target, tc.wantTarget)
		}
		if a.Params == nil {
			t.Errorf("%s: Params should preserve raw input", tc.name)
		}
	}
}

func TestNormalize_NonMCPNotMisclassified(t *testing.T) {
	// Future tool starting with "mcp" but missing the "__" wire
	// prefix must NOT route to MCP — guard against accidental
	// matches.
	a, _ := Normalize(HookInput{ToolName: "mcpDebug", ToolInput: map[string]any{}})
	if a.Type == gov.ActMCPCall {
		t.Fatalf("Type=%s; bare 'mcp' prefix without '__' should NOT route to MCP", a.Type)
	}
}

func TestNormalize_UnknownToolFailsClosed(t *testing.T) {
	a, _ := Normalize(HookInput{ToolName: "future_codex_tool", ToolInput: map[string]any{"x": 1}})
	if a.Type != gov.ActUnknown {
		t.Fatalf("Type=%s want ActUnknown", a.Type)
	}
	if a.Params == nil {
		t.Errorf("Params should preserve raw input")
	}
}

func TestFirstFilePathFromPatch_NoPatch(t *testing.T) {
	if got := firstFilePathFromPatch(""); got != "" {
		t.Errorf("empty input → %q want empty", got)
	}
	if got := firstFilePathFromPatch("not a patch at all"); got != "" {
		t.Errorf("non-patch input → %q want empty", got)
	}
}

func TestFirstFilePathFromPatch_HandlesAllVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"update", "*** Update File: a/b.go", "a/b.go"},
		{"add", "*** Add File: c/d.md", "c/d.md"},
		{"delete", "*** Delete File: e/f.txt", "e/f.txt"},
	}
	for _, tc := range cases {
		got := firstFilePathFromPatch(tc.in)
		if got != tc.want {
			t.Errorf("%s: got=%q want=%q", tc.name, got, tc.want)
		}
	}
	// Strips embedded leading whitespace
	got := firstFilePathFromPatch("   *** Update File: leading-space.go")
	if got != "leading-space.go" {
		t.Errorf("strips outer whitespace: got=%q", got)
	}
	// Doesn't grab from a similar but not-matching prefix
	if got := firstFilePathFromPatch("*** Updates: not-a-real-header"); got != "" {
		t.Errorf("non-matching prefix should not match, got=%q", got)
	}
	_ = strings.TrimSpace // silence unused-import lint if I move helpers
}
