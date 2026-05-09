package hermes

import "testing"

func TestStringField(t *testing.T) {
	m := map[string]any{
		"present":  "value",
		"nonstr":   42,
		"empty":    "",
	}
	if got := stringField(m, "present"); got != "value" {
		t.Errorf("stringField(present) = %q, want 'value'", got)
	}
	if got := stringField(m, "missing"); got != "" {
		t.Errorf("stringField(missing) = %q, want empty", got)
	}
	if got := stringField(m, "nonstr"); got != "" {
		t.Errorf("stringField(nonstr) = %q, want empty", got)
	}
	if got := stringField(m, "empty"); got != "" {
		t.Errorf("stringField(empty) = %q, want empty (empty string value)", got)
	}
}

func TestFirstStringInArray(t *testing.T) {
	m := map[string]any{
		"arr":     []any{"first", "second"},
		"empty":   []any{},
		"nonarr":  "not an array",
		"nested":  []any{42, "second"},
	}
	if got := firstStringInArray(m, "arr"); got != "first" {
		t.Errorf("firstStringInArray(arr) = %q, want 'first'", got)
	}
	if got := firstStringInArray(m, "empty"); got != "" {
		t.Errorf("firstStringInArray(empty) = %q, want empty", got)
	}
	if got := firstStringInArray(m, "nonarr"); got != "" {
		t.Errorf("firstStringInArray(nonarr) = %q, want empty", got)
	}
	if got := firstStringInArray(m, "missing"); got != "" {
		t.Errorf("firstStringInArray(missing) = %q, want empty", got)
	}
	if got := firstStringInArray(m, "nested"); got != "" {
		t.Errorf("firstStringInArray(nested) = %q, want empty (first is int)", got)
	}
}

func TestParseMCPToolName(t *testing.T) {
	tests := []struct {
		name      string
		wantSrv   string
		wantTool  string
		wantOK    bool
	}{
		{"mcp__server__tool", "server", "tool", true},
		{"mcp__server", "server", "", true},
		{"mcp__", "", "", false},
		{"notmcp__server__tool", "", "", false},
		{"mcp__srv__tl__extra", "srv", "tl__extra", true},
		{"", "", "", false},
	}
	for _, tc := range tests {
		srv, tool, ok := parseMCPToolName(tc.name)
		if ok != tc.wantOK || srv != tc.wantSrv || tool != tc.wantTool {
			t.Errorf("parseMCPToolName(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.name, srv, tool, ok, tc.wantSrv, tc.wantTool, tc.wantOK)
		}
	}
}