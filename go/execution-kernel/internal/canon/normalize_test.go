package canon

import (
	"testing"
)

// --- normalizeFlag ---

func TestNormalizeFlag(t *testing.T) {
	tests := []struct {
		tool, action, flag string
		want               string
	}{
		// grep short flags resolve to long form
		{"grep", "", "r", "recursive"},
		{"grep", "", "i", "ignore-case"},
		{"grep", "", "n", "line-number"},
		{"grep", "", "A", "after-context"},
		{"grep", "", "B", "before-context"},
		{"grep", "", "C", "context"},
		// git log action-specific flags
		{"git", "log", "n", "max-count"},
		{"git", "log", "oneline", "format=oneline"},
		// ls short flags
		{"ls", "", "l", "long"},
		{"ls", "", "a", "all"},
		// read short flag
		{"read", "", "n", "lines"},
		// Unknown flag passes through unchanged
		{"unknown", "", "z", "z"},
		{"git", "commit", "xyz", "xyz"},
		// Already-canonical long flag is returned unchanged
		{"grep", "", "recursive", "recursive"},
	}

	for _, tc := range tests {
		got := normalizeFlag(tc.tool, tc.action, tc.flag)
		if got != tc.want {
			t.Errorf("normalizeFlag(%q, %q, %q) = %q, want %q",
				tc.tool, tc.action, tc.flag, got, tc.want)
		}
	}
}

// --- flagTakesValue ---

func TestFlagTakesValue(t *testing.T) {
	tests := []struct {
		tool, action, flag string
		want               bool
	}{
		// grep valued flags
		{"grep", "", "A", true},
		{"grep", "", "after-context", true},
		{"grep", "", "B", true},
		{"grep", "", "before-context", true},
		{"grep", "", "C", true},
		{"grep", "", "context", true},
		{"grep", "", "e", true},
		{"grep", "", "regexp", true},
		// git commit valued flags
		{"git", "commit", "m", true},
		{"git", "commit", "message", true},
		// git log valued flags
		{"git", "log", "n", true},
		{"git", "log", "max-count", true},
		// curl valued flags
		{"curl", "", "H", true},
		{"curl", "", "header", true},
		{"curl", "", "X", true},
		{"curl", "", "request", true},
		{"curl", "", "d", true},
		{"curl", "", "data", true},
		// read valued flags
		{"read", "", "lines", true},
		{"read", "", "n", true},
		// Boolean flag — explicitly false in map
		{"git", "diff", "stat", false},
		// Unknown flags return false
		{"git", "", "verbose", false},
		{"grep", "", "unknown-flag", false},
		{"unknown", "", "x", false},
	}

	for _, tc := range tests {
		got := flagTakesValue(tc.tool, tc.action, tc.flag)
		if got != tc.want {
			t.Errorf("flagTakesValue(%q, %q, %q) = %v, want %v",
				tc.tool, tc.action, tc.flag, got, tc.want)
		}
	}
}

// --- normalizeRead ---

func TestNormalizeRead(t *testing.T) {
	t.Run("head with lines flag converts to head-lines", func(t *testing.T) {
		flags := map[string]string{"lines": "10"}
		args := []string{"file.txt"}
		normalizeRead("head", flags, &args)
		if flags["head-lines"] != "10" {
			t.Errorf("expected head-lines=10, got %v", flags)
		}
		if _, ok := flags["lines"]; ok {
			t.Error("expected lines key to be deleted")
		}
	})

	t.Run("head with n flag converts to head-lines", func(t *testing.T) {
		flags := map[string]string{"n": "5"}
		args := []string{"file.txt"}
		normalizeRead("head", flags, &args)
		if flags["head-lines"] != "5" {
			t.Errorf("expected head-lines=5, got %v", flags)
		}
		if _, ok := flags["n"]; ok {
			t.Error("expected n key to be deleted")
		}
	})

	t.Run("tail with lines flag converts to tail-lines", func(t *testing.T) {
		flags := map[string]string{"lines": "20"}
		args := []string{"file.txt"}
		normalizeRead("tail", flags, &args)
		if flags["tail-lines"] != "20" {
			t.Errorf("expected tail-lines=20, got %v", flags)
		}
		if _, ok := flags["lines"]; ok {
			t.Error("expected lines key to be deleted")
		}
	})

	t.Run("tail with n flag converts to tail-lines", func(t *testing.T) {
		flags := map[string]string{"n": "3"}
		args := []string{"file.txt"}
		normalizeRead("tail", flags, &args)
		if flags["tail-lines"] != "3" {
			t.Errorf("expected tail-lines=3, got %v", flags)
		}
		if _, ok := flags["n"]; ok {
			t.Error("expected n key to be deleted")
		}
	})

	t.Run("cat leaves flags unchanged", func(t *testing.T) {
		flags := map[string]string{"number": ""}
		args := []string{"file.txt"}
		normalizeRead("cat", flags, &args)
		if _, ok := flags["number"]; !ok {
			t.Error("expected number key to remain for cat")
		}
	})

	t.Run("head with no line flag leaves flags unchanged", func(t *testing.T) {
		flags := map[string]string{"silent": ""}
		args := []string{"file.txt"}
		normalizeRead("head", flags, &args)
		if _, ok := flags["head-lines"]; ok {
			t.Error("expected head-lines not to be set when no -n/-lines flag present")
		}
	})
}

// --- normalizeGrep ---

func TestNormalizeGrep(t *testing.T) {
	t.Run("strips recursive flag", func(t *testing.T) {
		flags := map[string]string{"recursive": "", "ignore-case": ""}
		normalizeGrep("grep", flags)
		if _, ok := flags["recursive"]; ok {
			t.Error("expected recursive to be removed")
		}
		if _, ok := flags["ignore-case"]; !ok {
			t.Error("expected ignore-case to remain")
		}
	})

	t.Run("strips short r flag", func(t *testing.T) {
		flags := map[string]string{"r": "", "line-number": ""}
		normalizeGrep("rg", flags)
		if _, ok := flags["r"]; ok {
			t.Error("expected r to be removed")
		}
		if _, ok := flags["line-number"]; !ok {
			t.Error("expected line-number to remain")
		}
	})

	t.Run("strips both recursive and r when both present", func(t *testing.T) {
		flags := map[string]string{"recursive": "", "r": "", "count": ""}
		normalizeGrep("ag", flags)
		if _, ok := flags["recursive"]; ok {
			t.Error("expected recursive to be removed")
		}
		if _, ok := flags["r"]; ok {
			t.Error("expected r to be removed")
		}
		if _, ok := flags["count"]; !ok {
			t.Error("expected count to remain")
		}
	})

	t.Run("no-op when no recursive flags present", func(t *testing.T) {
		flags := map[string]string{"line-number": "", "ignore-case": ""}
		normalizeGrep("grep", flags)
		if len(flags) != 2 {
			t.Errorf("expected 2 flags unchanged, got %d: %v", len(flags), flags)
		}
	})
}

// --- normalizeGit ---

func TestNormalizeGit(t *testing.T) {
	t.Run("oneline expands to format=oneline and removes abbrev-commit", func(t *testing.T) {
		flags := map[string]string{"oneline": "", "abbrev-commit": ""}
		normalizeGit("log", flags)
		if flags["format"] != "oneline" {
			t.Errorf("expected format=oneline, got %q", flags["format"])
		}
		if _, ok := flags["oneline"]; ok {
			t.Error("expected oneline to be removed")
		}
		if _, ok := flags["abbrev-commit"]; ok {
			t.Error("expected abbrev-commit to be removed")
		}
	})

	t.Run("oneline without abbrev-commit still sets format", func(t *testing.T) {
		flags := map[string]string{"oneline": ""}
		normalizeGit("log", flags)
		if flags["format"] != "oneline" {
			t.Errorf("expected format=oneline, got %q", flags["format"])
		}
		if _, ok := flags["oneline"]; ok {
			t.Error("expected oneline to be removed")
		}
	})

	t.Run("pretty is replaced by format with its value", func(t *testing.T) {
		flags := map[string]string{"pretty": "short"}
		normalizeGit("log", flags)
		if flags["format"] != "short" {
			t.Errorf("expected format=short, got %q", flags["format"])
		}
		if _, ok := flags["pretty"]; ok {
			t.Error("expected pretty to be removed")
		}
	})

	t.Run("non-log action leaves flags unchanged", func(t *testing.T) {
		flags := map[string]string{"oneline": "", "pretty": "short"}
		normalizeGit("status", flags)
		if _, ok := flags["format"]; ok {
			t.Error("expected format not to be set for non-log action")
		}
		if _, ok := flags["oneline"]; !ok {
			t.Error("expected oneline to remain for non-log action")
		}
		if _, ok := flags["pretty"]; !ok {
			t.Error("expected pretty to remain for non-log action")
		}
	})
}

// --- normalizeToolSpecific ---

func TestNormalizeToolSpecific(t *testing.T) {
	t.Run("dispatches read tool to normalizeRead", func(t *testing.T) {
		flags := map[string]string{"n": "5"}
		args := []string{"file.txt"}
		normalizeToolSpecific("read", "head", "", flags, &args)
		if flags["head-lines"] != "5" {
			t.Errorf("expected head-lines=5 after read dispatch, got %v", flags)
		}
	})

	t.Run("dispatches grep tool to normalizeGrep", func(t *testing.T) {
		flags := map[string]string{"recursive": "", "ignore-case": ""}
		args := []string{"."}
		normalizeToolSpecific("grep", "grep", "", flags, &args)
		if _, ok := flags["recursive"]; ok {
			t.Error("expected recursive to be stripped after grep dispatch")
		}
		if _, ok := flags["ignore-case"]; !ok {
			t.Error("expected ignore-case to remain after grep dispatch")
		}
	})

	t.Run("dispatches git tool to normalizeGit", func(t *testing.T) {
		flags := map[string]string{"oneline": ""}
		args := []string{}
		normalizeToolSpecific("git", "git", "log", flags, &args)
		if flags["format"] != "oneline" {
			t.Errorf("expected format=oneline after git dispatch, got %v", flags)
		}
	})

	t.Run("unknown tool is a no-op", func(t *testing.T) {
		flags := map[string]string{"foo": "bar"}
		args := []string{"baz"}
		normalizeToolSpecific("docker", "docker", "", flags, &args)
		if flags["foo"] != "bar" {
			t.Errorf("expected flags unchanged for unknown tool, got %v", flags)
		}
		if len(args) != 1 || args[0] != "baz" {
			t.Errorf("expected args unchanged for unknown tool, got %v", args)
		}
	})
}

// --- maskSensitive ---

func TestMaskSensitive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "masks value containing API_KEY",
			input: "my_api_key_value",
			want:  "[MASKED]",
		},
		{
			name:  "masks value containing SECRET",
			input: "mysecretvalue",
			want:  "[MASKED]",
		},
		{
			name:  "masks value containing TOKEN",
			input: "oauth_token_xyz",
			want:  "[MASKED]",
		},
		{
			name:  "masks value containing PASSWORD",
			input: "password123",
			want:  "[MASKED]",
		},
		{
			name:  "masks value containing AUTH",
			input: "basic_auth_string",
			want:  "[MASKED]",
		},
		{
			name:  "masks value containing CREDENTIAL",
			input: "aws_credential_abc",
			want:  "[MASKED]",
		},
		{
			name:  "masks long alphanumeric string (API key style)",
			input: "sk-proj-abcdefghijklmnopqrstuvwxyz01234",
			want:  "[MASKED]",
		},
		{
			name:  "masks long hex-like token",
			input: "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef01",
			want:  "[MASKED]",
		},
		{
			name:  "passes through short normal value",
			input: "main",
			want:  "main",
		},
		{
			name:  "passes through normal git flag value",
			input: "oneline",
			want:  "oneline",
		},
		{
			name:  "passes through path with slashes (even if long)",
			input: "/a/very/long/path/that/is/longer/than/thirty/chars/here",
			want:  "/a/very/long/path/that/is/longer/than/thirty/chars/here",
		},
		{
			name:  "passes through string with spaces (even if long)",
			input: "this is a long sentence that exceeds thirty characters easily",
			want:  "this is a long sentence that exceeds thirty characters easily",
		},
		{
			name:  "masks case-insensitively (uppercase pattern in mixed-case input)",
			input: "Bearer_TOKEN_abcdef",
			want:  "[MASKED]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := maskSensitive(tc.input)
			if got != tc.want {
				t.Errorf("maskSensitive(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
