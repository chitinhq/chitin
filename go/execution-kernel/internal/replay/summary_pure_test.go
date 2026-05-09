package replay

import "testing"

func TestReverseToolName(t *testing.T) {
	tests := []struct {
		actionType string
		fallback   string
		want       string
	}{
		{"shell.exec", "Bash", "Bash"},
		{"file.write", "Edit", "Edit"},
		{"file.read", "Read", "Read"},
		{"http.request", "WebFetch", "WebFetch"},
		{"delegate.task", "Task", "Task"},
		{"git.worktree.add", "EnterWorktree", "EnterWorktree"},
		{"git.worktree.remove", "ExitWorktree", "ExitWorktree"},
		{"unknown.action", "FallbackTool", "FallbackTool"},
		{"unknown.action", "", ""},
	}
	for _, tt := range tests {
		got := reverseToolName(tt.actionType, tt.fallback)
		if got != tt.want {
			t.Errorf("reverseToolName(%q, %q) = %q, want %q", tt.actionType, tt.fallback, got, tt.want)
		}
	}
}

func TestIsLikelyFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"src/main.go", true},
		{"/etc/passwd", true},
		{"./README.md", true},
		{"config.yaml", true},
		{"app.ts", true},
		{"index.js", true},
		{"styles.css", false}, // .css not in extension list
		{"README", false},
		{"plainword", false},
		{"rm -rf /tmp", true}, // contains /
		{"file.py", true},
		{"script.sh", true},
		{"data.json", true},
		{"spec.yml", true},
		{"config.yaml", true},
		{"file.tsx", true},
	}
	for _, tt := range tests {
		got := isLikelyFilePath(tt.input)
		if got != tt.want {
			t.Errorf("isLikelyFilePath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	// Short paths pass through unchanged
	s := "short/path.go"
	if got := shortPath(s); got != s {
		t.Errorf("shortPath(%q) = %q, want %q", s, got, s)
	}
	// Long paths are truncated with "..." prefix
	long := "/very/long/path/that/exceeds/fifty/characters/easily/output.go"
	if len(long) <= 50 {
		t.Skip("long string not >50 chars")
	}
	got := shortPath(long)
	if got[:3] != "..." {
		t.Errorf("shortPath(long) should start with '...', got %q", got)
	}
	if len(got) > 50 {
		t.Errorf("shortPath result too long: %d chars", len(got))
	}
}

func TestShortID(t *testing.T) {
	// Short IDs pass through unchanged
	short := "abc123"
	if got := shortID(short); got != short {
		t.Errorf("shortID(%q) = %q, want %q", short, got, short)
	}
	// Long IDs are truncated to first 8 chars + "…"
	long := "505c4216-bc0a-49d1-b512-55df4d6563c0"
	got := shortID(long)
	want := long[:8] + "…"
	if got != want {
		t.Errorf("shortID(long) = %q, want %q", got, want)
	}
}

func TestBoolToStr(t *testing.T) {
	if boolToStr(true) != "allow" {
		t.Errorf("boolToStr(true) = %q, want %q", boolToStr(true), "allow")
	}
	if boolToStr(false) != "deny" {
		t.Errorf("boolToStr(false) = %q, want %q", boolToStr(false), "deny")
	}
}