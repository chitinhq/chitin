package hermes

import (
	"testing"
)

func TestFirstStringInArray(t *testing.T) {
	t.Run("missing key returns empty", func(t *testing.T) {
		m := map[string]any{"other": "val"}
		if got := firstStringInArray(m, "key"); got != "" {
			t.Errorf("expected empty for missing key, got %q", got)
		}
	})

	t.Run("empty list returns empty", func(t *testing.T) {
		m := map[string]any{"key": []any{}}
		if got := firstStringInArray(m, "key"); got != "" {
			t.Errorf("expected empty for empty list, got %q", got)
		}
	})

	t.Run("list with string returns first", func(t *testing.T) {
		m := map[string]any{"key": []any{"first", "second"}}
		if got := firstStringInArray(m, "key"); got != "first" {
			t.Errorf("expected first, got %q", got)
		}
	})

	t.Run("list with non-string first returns empty", func(t *testing.T) {
		m := map[string]any{"key": []any{42, "second"}}
		if got := firstStringInArray(m, "key"); got != "" {
			t.Errorf("expected empty for non-string first, got %q", got)
		}
	})

	t.Run("non-list value returns empty", func(t *testing.T) {
		m := map[string]any{"key": "not a list"}
		if got := firstStringInArray(m, "key"); got != "" {
			t.Errorf("expected empty for non-list, got %q", got)
		}
	})
}

func TestParseMCPToolName(t *testing.T) {
	t.Run("no mcp prefix returns false", func(t *testing.T) {
		_, _, ok := parseMCPToolName("shell.exec")
		if ok {
			t.Error("expected false for non-mcp name")
		}
	})

	t.Run("mcp prefix with server only", func(t *testing.T) {
		server, tool, ok := parseMCPToolName("mcp__myserver")
		if !ok {
			t.Error("expected ok for server-only mcp name")
		}
		if server != "myserver" {
			t.Errorf("expected server=myserver, got %q", server)
		}
		if tool != "" {
			t.Errorf("expected empty tool, got %q", tool)
		}
	})

	t.Run("mcp prefix with server and tool", func(t *testing.T) {
		server, tool, ok := parseMCPToolName("mcp__myserver__mytool")
		if !ok {
			t.Error("expected ok for full mcp name")
		}
		if server != "myserver" {
			t.Errorf("expected server=myserver, got %q", server)
		}
		if tool != "mytool" {
			t.Errorf("expected tool=mytool, got %q", tool)
		}
	})

	t.Run("mcp prefix with empty rest returns false", func(t *testing.T) {
		_, _, ok := parseMCPToolName("mcp__")
		if ok {
			t.Error("expected false for empty rest after prefix")
		}
	})
}