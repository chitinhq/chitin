package canon

import (
	"testing"
)

// TestParseOrOperator covers Parse() splitting "||" chains with OpOr.
func TestParseOrOperator(t *testing.T) {
	p := Parse("git diff || echo 'no changes'")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(p.Segments))
	}
	if p.Segments[0].Op != OpNone {
		t.Errorf("first segment op=%q, want empty", p.Segments[0].Op)
	}
	if p.Segments[1].Op != OpOr {
		t.Errorf("second segment op=%q, want ||", p.Segments[1].Op)
	}
	if p.Segments[0].Command.Tool != "git" || p.Segments[0].Command.Action != "diff" {
		t.Errorf("first cmd: tool=%q action=%q", p.Segments[0].Command.Tool, p.Segments[0].Command.Action)
	}
}

// TestParseSeqOperator covers Parse() splitting ";" chains with OpSeq.
func TestParseSeqOperator(t *testing.T) {
	p := Parse("echo hello; ls -l")
	if len(p.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(p.Segments))
	}
	if p.Segments[0].Op != OpNone {
		t.Errorf("first segment op=%q, want empty", p.Segments[0].Op)
	}
	if p.Segments[1].Op != OpSeq {
		t.Errorf("second segment op=%q, want ;", p.Segments[1].Op)
	}
	if p.Segments[0].Command.Tool != "echo" {
		t.Errorf("first cmd tool=%q, want 'echo'", p.Segments[0].Command.Tool)
	}
	if p.Segments[1].Command.Tool != "ls" {
		t.Errorf("second cmd tool=%q, want 'ls'", p.Segments[1].Command.Tool)
	}
}

// TestParseAllOperators exercises all four chain operators in a single pipeline.
func TestParseAllOperators(t *testing.T) {
	// OpNone ; OpSeq && OpAnd || OpOr | OpPipe
	p := Parse("echo a; echo b && echo c || echo d | wc -l")
	if len(p.Segments) != 5 {
		t.Fatalf("expected 5 segments, got %d: %v", len(p.Segments), p.Segments)
	}
	wantOps := []ChainOp{OpNone, OpSeq, OpAnd, OpOr, OpPipe}
	for i, want := range wantOps {
		if p.Segments[i].Op != want {
			t.Errorf("segment[%d].Op=%q, want %q", i, p.Segments[i].Op, want)
		}
	}
}

// TestSplitChainQuotedOperators verifies operators inside quotes are not treated as split points.
func TestSplitChainQuotedOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSegs int
	}{
		{"double-quoted &&", `echo "a && b"`, 1},
		{"single-quoted ||", `echo 'a || b'`, 1},
		{"double-quoted ;", `echo "a; b"`, 1},
		{"double-quoted |", `echo "a | b"`, 1},
		{"unquoted && splits", "echo a && echo b", 2},
		{"unquoted || splits", "echo a || echo b", 2},
		{"unquoted ; splits", "echo a; echo b", 2},
		{"unquoted | splits", "echo a | wc -l", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			segs := splitChain(tc.input)
			if len(segs) != tc.wantSegs {
				t.Errorf("splitChain(%q): got %d segments, want %d", tc.input, len(segs), tc.wantSegs)
			}
		})
	}
}

// TestSplitChainEscapedChars verifies backslash-escaped chars are written verbatim (not split).
func TestSplitChainEscapedChars(t *testing.T) {
	// Backslash before each & prevents the && from being treated as an operator.
	segs := splitChain(`echo a\&\& b`)
	if len(segs) != 1 {
		t.Errorf("escaped &&: expected 1 segment, got %d", len(segs))
	}
	if len(segs) == 1 && segs[0].text == "" {
		t.Error("escaped segment text should not be empty")
	}
}

// TestTokenize covers tokenize() for quoting and backslash escape handling.
func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple words",
			input: "git status",
			want:  []string{"git", "status"},
		},
		{
			name:  "double-quoted space preserved",
			input: `git commit -m "fix the bug"`,
			want:  []string{"git", "commit", "-m", "fix the bug"},
		},
		{
			name:  "single-quoted space preserved",
			input: `echo 'hello world'`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "backslash-escaped space",
			input: `echo hello\ world`,
			want:  []string{"echo", "hello world"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   \t  ",
			want:  nil,
		},
		{
			name:  "single-quoted file with spaces",
			input: `cat 'my file.txt'`,
			want:  []string{"cat", "my file.txt"},
		},
		{
			name:  "tabs between tokens",
			input: "ls\t-la",
			want:  []string{"ls", "-la"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenize(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("tokenize(%q) = %v (%d tokens), want %v (%d tokens)",
					tc.input, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("token[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseFlag covers parseFlag() directly with a variety of input forms.
func TestParseFlag(t *testing.T) {
	tests := []struct {
		tok      string
		wantName string
		wantVal  string
	}{
		{"--count=5", "count", "5"},
		{"-n", "n", ""},
		{"--verbose", "verbose", ""},
		{"-C5", "C5", ""},
		{"--output=/tmp/out.txt", "output", "/tmp/out.txt"},
		{"--format=oneline", "format", "oneline"},
		{"-v", "v", ""},
		{"--dry-run", "dry-run", ""},
	}
	for _, tc := range tests {
		t.Run(tc.tok, func(t *testing.T) {
			name, val := parseFlag(tc.tok)
			if name != tc.wantName {
				t.Errorf("parseFlag(%q) name=%q, want %q", tc.tok, name, tc.wantName)
			}
			if val != tc.wantVal {
				t.Errorf("parseFlag(%q) val=%q, want %q", tc.tok, val, tc.wantVal)
			}
		})
	}
}

// TestExpandShortFlags covers expandShortFlags() directly.
func TestExpandShortFlags(t *testing.T) {
	tests := []struct {
		tok  string
		want []string
	}{
		{"-rn", []string{"-r", "-n"}},
		{"--verbose", []string{"--verbose"}},
		{"-C5", []string{"-C5"}},
		{"-n", []string{"-n"}},
		{"-la", []string{"-l", "-a"}},
		{"--format=oneline", []string{"--format=oneline"}},
		{"-n20", []string{"-n20"}},
		{"-abc", []string{"-a", "-b", "-c"}},
	}
	for _, tc := range tests {
		t.Run(tc.tok, func(t *testing.T) {
			got := expandShortFlags(tc.tok)
			if len(got) != len(tc.want) {
				t.Fatalf("expandShortFlags(%q) = %v, want %v", tc.tok, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("expandShortFlags(%q)[%d] = %q, want %q", tc.tok, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseOneWhitespace verifies whitespace-only input yields "unknown" tool.
func TestParseOneWhitespace(t *testing.T) {
	for _, input := range []string{"   ", "\t", "  \t  "} {
		cmd := ParseOne(input)
		if cmd.Tool != "unknown" {
			t.Errorf("ParseOne(%q) tool=%q, want 'unknown'", input, cmd.Tool)
		}
	}
}
