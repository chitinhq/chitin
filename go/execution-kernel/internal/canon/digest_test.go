package canon

import (
	"regexp"
	"testing"
)

// hexPattern validates that a digest is exactly 16 lowercase hex characters.
var hexPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// --- Digest() tests ---

// TestDigest_Length verifies Digest always returns exactly 16 lowercase hex characters.
func TestDigest_Length(t *testing.T) {
	cases := []struct {
		name string
		cmd  Command
	}{
		{"empty", Command{}},
		{"tool only", Command{Tool: "git"}},
		{"full", Command{
			Tool:   "grep",
			Flags:  map[string]string{"recursive": "", "line-number": ""},
			Args:   []string{"pattern", "."},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Digest(tc.cmd)
			if !hexPattern.MatchString(got) {
				t.Errorf("Digest = %q, want 16 lowercase hex chars", got)
			}
		})
	}
}

// TestDigest_EmptyCommand verifies Digest on a zero-value Command does not panic
// and returns a consistent result across calls.
func TestDigest_EmptyCommand(t *testing.T) {
	d1 := Digest(Command{})
	d2 := Digest(Command{})
	if d1 != d2 {
		t.Errorf("empty command digest not deterministic: %s vs %s", d1, d2)
	}
}

// TestDigest_FlagOrderIndependence verifies that flag insertion order does not affect
// the digest — flags are sorted before hashing.
func TestDigest_FlagOrderIndependence(t *testing.T) {
	a := Command{Tool: "grep", Flags: map[string]string{
		"recursive":   "",
		"line-number": "",
		"ignore-case": "",
	}}
	b := Command{Tool: "grep", Flags: map[string]string{
		"ignore-case": "",
		"line-number": "",
		"recursive":   "",
	}}
	if Digest(a) != Digest(b) {
		t.Errorf("flag insertion order should not affect digest: %s vs %s", Digest(a), Digest(b))
	}
}

// TestDigest_EquivalentCommands verifies identical Command structs produce the same digest.
func TestDigest_EquivalentCommands(t *testing.T) {
	a := Command{Tool: "docker", Action: "ps", Flags: map[string]string{"all": ""}, Args: []string{"mycontainer"}}
	b := Command{Tool: "docker", Action: "ps", Flags: map[string]string{"all": ""}, Args: []string{"mycontainer"}}
	if Digest(a) != Digest(b) {
		t.Errorf("identical commands produced different digests: %s vs %s", Digest(a), Digest(b))
	}
}

// TestDigest_Differences uses a table-driven test to verify that changing any single
// field of a Command produces a different digest.
func TestDigest_Differences(t *testing.T) {
	base := Command{
		Tool:   "git",
		Action: "log",
		Flags:  map[string]string{"max-count": "10"},
		Args:   []string{"main"},
	}
	cases := []struct {
		name  string
		other Command
	}{
		{
			"different tool",
			Command{Tool: "gh", Action: "log", Flags: map[string]string{"max-count": "10"}, Args: []string{"main"}},
		},
		{
			"different action",
			Command{Tool: "git", Action: "diff", Flags: map[string]string{"max-count": "10"}, Args: []string{"main"}},
		},
		{
			"different flag value",
			Command{Tool: "git", Action: "log", Flags: map[string]string{"max-count": "20"}, Args: []string{"main"}},
		},
		{
			"different args",
			Command{Tool: "git", Action: "log", Flags: map[string]string{"max-count": "10"}, Args: []string{"feature"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Digest(base) == Digest(tc.other) {
				t.Errorf("expected different digests for %q, both got %s", tc.name, Digest(base))
			}
		})
	}
}

// --- PipelineDigest() tests ---

// TestPipelineDigest_Length verifies PipelineDigest always returns exactly 16 lowercase hex characters.
func TestPipelineDigest_Length(t *testing.T) {
	cases := []struct {
		name string
		p    Pipeline
	}{
		{"empty", Pipeline{}},
		{"one segment", Pipeline{Segments: []Segment{
			{Op: OpNone, Command: Command{Digest: "abc123"}},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PipelineDigest(tc.p)
			if !hexPattern.MatchString(got) {
				t.Errorf("PipelineDigest = %q, want 16 lowercase hex chars", got)
			}
		})
	}
}

// TestPipelineDigest_EmptyPipeline verifies PipelineDigest on an empty Pipeline does not
// panic and returns a consistent result across calls.
func TestPipelineDigest_EmptyPipeline(t *testing.T) {
	d1 := PipelineDigest(Pipeline{})
	d2 := PipelineDigest(Pipeline{})
	if d1 != d2 {
		t.Errorf("empty pipeline digest not deterministic: %s vs %s", d1, d2)
	}
}

// TestPipelineDigest_Equivalent verifies identical pipelines produce the same digest.
func TestPipelineDigest_Equivalent(t *testing.T) {
	mkPipeline := func() Pipeline {
		return Pipeline{Segments: []Segment{
			{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
			{Op: OpAnd, Command: Command{Digest: "cccc3333dddd4444"}},
		}}
	}
	if PipelineDigest(mkPipeline()) != PipelineDigest(mkPipeline()) {
		t.Errorf("identical pipelines should produce same digest")
	}
}

// TestPipelineDigest_DifferentOp verifies that changing the chain operator produces a
// different pipeline digest.
func TestPipelineDigest_DifferentOp(t *testing.T) {
	cases := []struct {
		name string
		op   ChainOp
	}{
		{"OpAnd", OpAnd},
		{"OpOr", OpOr},
		{"OpPipe", OpPipe},
		{"OpSeq", OpSeq},
	}
	// Verify each pair of operators produces a different digest.
	for i := 0; i < len(cases); i++ {
		for j := i + 1; j < len(cases); j++ {
			a := Pipeline{Segments: []Segment{
				{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
				{Op: cases[i].op, Command: Command{Digest: "cccc3333dddd4444"}},
			}}
			b := Pipeline{Segments: []Segment{
				{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
				{Op: cases[j].op, Command: Command{Digest: "cccc3333dddd4444"}},
			}}
			if PipelineDigest(a) == PipelineDigest(b) {
				t.Errorf("%s vs %s should produce different pipeline digests, both got %s",
					cases[i].name, cases[j].name, PipelineDigest(a))
			}
		}
	}
}

// TestPipelineDigest_DifferentSegmentDigest verifies that changing one segment's
// Command.Digest field changes the pipeline digest.
func TestPipelineDigest_DifferentSegmentDigest(t *testing.T) {
	a := Pipeline{Segments: []Segment{
		{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
		{Op: OpPipe, Command: Command{Digest: "cccc3333dddd4444"}},
	}}
	b := Pipeline{Segments: []Segment{
		{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
		{Op: OpPipe, Command: Command{Digest: "eeee5555ffff6666"}},
	}}
	if PipelineDigest(a) == PipelineDigest(b) {
		t.Errorf("different segment digests should produce different pipeline digests: %s vs %s",
			PipelineDigest(a), PipelineDigest(b))
	}
}

// TestPipelineDigest_SegmentOrderMatters verifies that reversing segment order produces
// a different pipeline digest.
func TestPipelineDigest_SegmentOrderMatters(t *testing.T) {
	a := Pipeline{Segments: []Segment{
		{Op: OpNone, Command: Command{Digest: "aaaa1111bbbb2222"}},
		{Op: OpPipe, Command: Command{Digest: "cccc3333dddd4444"}},
	}}
	b := Pipeline{Segments: []Segment{
		{Op: OpNone, Command: Command{Digest: "cccc3333dddd4444"}},
		{Op: OpPipe, Command: Command{Digest: "aaaa1111bbbb2222"}},
	}}
	if PipelineDigest(a) == PipelineDigest(b) {
		t.Errorf("reversed segment order should produce different pipeline digest: %s vs %s",
			PipelineDigest(a), PipelineDigest(b))
	}
}
