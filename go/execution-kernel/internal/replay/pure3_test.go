package replay

import (
	"sort"
	"testing"
)

func TestReverseToolName(t *testing.T) {
	cases := []struct {
		actionType string
		fallback   string
		want       string
	}{
		{"shell.exec", "Unknown", "Bash"},
		{"file.write", "Unknown", "Edit"},
		{"file.read", "Unknown", "Read"},
		{"http.request", "Unknown", "WebFetch"},
		{"delegate.task", "Unknown", "Task"},
		{"git.worktree.add", "Unknown", "EnterWorktree"},
		{"git.worktree.remove", "Unknown", "ExitWorktree"},
		{"unknown.action", "Fallback", "Fallback"},
		{"", "Default", "Default"},
	}
	for _, c := range cases {
		got := reverseToolName(c.actionType, c.fallback)
		if got != c.want {
			t.Errorf("reverseToolName(%q, %q) = %q, want %q", c.actionType, c.fallback, got, c.want)
		}
	}
}

func TestIsLikelyFilePath(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"/path/to/file.go", true},
		{"src/index.ts", true},
		{"README.md", true},
		{"config.yaml", true},
		{"script.sh", true},
		{"data.json", true},
		{"style.css", false},
		{"just-a-word", false},
		{"rm -rf /tmp", true}, // contains /
	}
	for _, c := range cases {
		got := isLikelyFilePath(c.input)
		if got != c.want {
			t.Errorf("isLikelyFilePath(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestShortPath(t *testing.T) {
	// Short path unchanged
	if got := shortPath("short/path"); got != "short/path" {
		t.Errorf("shortPath(short) = %q, want %q", got, "short/path")
	}
	// Long path truncated
	long := "this-is-a-very-long-path-that-should-get-truncated-by-the-short-path-func"
	if len(shortPath(long)) > 50 {
		t.Errorf("shortPath(long) = %q, len=%d, want <=50", shortPath(long), len(shortPath(long)))
	}
}

func TestShortID(t *testing.T) {
	// Short ID unchanged
	if got := shortID("abc"); got != "abc" {
		t.Errorf("shortID(short) = %q, want %q", got, "abc")
	}
	// Long ID truncated
	if got := shortID("0123456789abcdef0123456789"); got != "01234567…" {
		t.Errorf("shortID(long) = %q, want %q", got, "01234567…")
	}
}

func TestIsPolicyAbsentError(t *testing.T) {
	if isPolicyAbsentError(nil) {
		t.Error("nil should not be policy absent error")
	}
	if !isPolicyAbsentError(errFakePolicyAbsent) {
		t.Error("error containing 'no_policy_found' should match")
	}
	if isPolicyAbsentError(errFakeOther) {
		t.Error("unrelated error should not match")
	}
}

// fake errors for testing
var errFakePolicyAbsent = &policyErr{"no_policy_found: test"}
var errFakeOther = &policyErr{"something_else"}

type policyErr struct{ msg string }

func (e *policyErr) Error() string { return e.msg }

func TestBoolToStr(t *testing.T) {
	if got := boolToStr(true); got != "allow" {
		t.Errorf("boolToStr(true) = %q, want %q", got, "allow")
	}
	if got := boolToStr(false); got != "deny" {
		t.Errorf("boolToStr(false) = %q, want %q", got, "deny")
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"zebra": true, "apple": true, "mango": true}
	keys := sortedKeys(m)
	if !sort.StringsAreSorted(keys) {
		t.Errorf("sortedKeys not sorted: %v", keys)
	}
	if len(keys) != 3 {
		t.Errorf("sortedKeys returned %d keys, want 3", len(keys))
	}
}

func TestSortedKeys_Empty(t *testing.T) {
	keys := sortedKeys(map[string]bool{})
	if len(keys) != 0 {
		t.Errorf("sortedKeys on empty map should return empty, got %v", keys)
	}
}