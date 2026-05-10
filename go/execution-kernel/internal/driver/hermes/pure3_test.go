package hermes

import (
	"testing"
)

func TestHermesStringField(t *testing.T) {
	m := map[string]any{
		"name":    "alice",
		"age":     30,
		"missing": "",
	}
	if got := stringField(m, "name"); got != "alice" {
		t.Errorf("stringField(name) = %q, want %q", got, "alice")
	}
	if got := stringField(m, "age"); got != "" {
		t.Errorf("stringField(age) = %q, want empty", got)
	}
	if got := stringField(m, "nonexistent"); got != "" {
		t.Errorf("stringField(nonexistent) = %q, want empty", got)
	}
}

func TestFirstStringInArray(t *testing.T) {
	// key present, array with string
	m := map[string]any{
		"tools": []any{"bash", "read"},
	}
	if got := firstStringInArray(m, "tools"); got != "bash" {
		t.Errorf("firstStringInArray(tools) = %q, want %q", got, "bash")
	}

	// key missing
	if got := firstStringInArray(m, "missing"); got != "" {
		t.Errorf("firstStringInArray(missing) = %q, want empty", got)
	}

	// value not an array
	m2 := map[string]any{"tools": "not-array"}
	if got := firstStringInArray(m2, "tools"); got != "" {
		t.Errorf("firstStringInArray(string) = %q, want empty", got)
	}

	// empty array
	m3 := map[string]any{"tools": []any{}}
	if got := firstStringInArray(m3, "tools"); got != "" {
		t.Errorf("firstStringInArray(empty) = %q, want empty", got)
	}

	// array with non-string first element
	m4 := map[string]any{"tools": []any{42, "bash"}}
	if got := firstStringInArray(m4, "tools"); got != "" {
		t.Errorf("firstStringInArray(int-first) = %q, want empty", got)
	}
}

func TestParseMCPToolName(t *testing.T) {
	cases := []struct {
		input      string
		wantServer string
		wantTool   string
		wantOK     bool
	}{
		{"mcp__server__tool", "server", "tool", true},
		{"mcp__server", "server", "", true},
		{"mcp__", "", "", false},
		{"bash", "", "", false},
		{"mcp__s__t", "s", "t", true},
	}
	for _, c := range cases {
		server, tool, ok := parseMCPToolName(c.input)
		if server != c.wantServer || tool != c.wantTool || ok != c.wantOK {
			t.Errorf("parseMCPToolName(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.input, server, tool, ok, c.wantServer, c.wantTool, c.wantOK)
		}
	}
}