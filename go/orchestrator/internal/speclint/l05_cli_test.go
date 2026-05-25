package speclint

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestParseCLIAllowlist covers the allowlist file format: tolerant of
// blank lines and `#` comments, requires two whitespace-separated fields
// per data line, skips malformed lines silently.
func TestParseCLIAllowlist(t *testing.T) {
	in := `# operator-curated allowlist
chitin-orchestrator spec-lint
chitin-kernel  log-tail

# blank above is ignored
malformed-single-field
chitin-orchestrator   schedule   extra-tokens-ignored
`
	got := ParseCLIAllowlist(in)
	want := []AllowlistEntry{
		{Binary: "chitin-orchestrator", Subcommand: "spec-lint"},
		{Binary: "chitin-kernel", Subcommand: "log-tail"},
		{Binary: "chitin-orchestrator", Subcommand: "schedule"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCLIAllowlist mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

// TestExtractGhAPIPath exercises the tokenizer that resolves the
// endpoint path inside a `gh api ...` invocation, covering the flag
// forms gh actually accepts.
func TestExtractGhAPIPath(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"bare path", "repos/foo/bar/issues", "repos/foo/bar/issues"},
		{"-X POST then path", "-X POST repos/foo/bar/issues", "repos/foo/bar/issues"},
		{"--method POST then path", "--method POST repos/foo/bar/issues", "repos/foo/bar/issues"},
		{"--method=POST then path", "--method=POST repos/foo/bar/issues", "repos/foo/bar/issues"},
		{"-f field then path", "-f body=hi repos/foo/bar/issues/1/comments", "repos/foo/bar/issues/1/comments"},
		{"-F raw-field then path", "-F draft=true repos/foo/bar/pulls", "repos/foo/bar/pulls"},
		{"-H header then path", "-H Accept:application/vnd.github+json repos/foo/bar", "repos/foo/bar"},
		{"path then trailing flag", "repos/foo/bar -f body=hi", "repos/foo/bar"},
		{"flags only", "-X POST -f body=hi", ""},
		{"empty", "", ""},
		{"leading slash path", "/repos/foo/bar", "/repos/foo/bar"},
		{"non-repos path", "meta", "meta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractGhAPIPath(tc.args)
			if got != tc.want {
				t.Errorf("extractGhAPIPath(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestCheckGhAPIPaths covers the most likely false-positive shapes
// (flagged invocations) and the violations the rule must still catch
// (leading slash, non-repos endpoints, multiple per file with their
// own line numbers).
func TestCheckGhAPIPaths(t *testing.T) {
	text := strings.Join([]string{
		"line 1: ok bare `gh api repos/foo/bar/issues`",
		"line 2: ok flagged `gh api -X POST repos/foo/bar/issues/1/comments -f body=hi`",
		"line 3: ok long flag `gh api --method=PATCH repos/foo/bar/pulls/1`",
		"line 4: bad leading slash `gh api /repos/foo/bar`",
		"line 5: bad non-repos `gh api meta`",
		"line 6: ignored prose (no backticks) gh api repos/anything",
	}, "\n")

	findings := checkGhAPIPaths("spec.md", text)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })

	if findings[0].Line != 4 || !strings.Contains(findings[0].Message, "/repos/foo/bar") {
		t.Errorf("finding[0] mismatch: %+v", findings[0])
	}
	if findings[1].Line != 5 || !strings.Contains(findings[1].Message, "meta") {
		t.Errorf("finding[1] mismatch: %+v", findings[1])
	}
	for _, f := range findings {
		if f.Rule != "L05" || f.Severity != SeverityError || f.File != "spec.md" {
			t.Errorf("finding metadata mismatch: %+v", f)
		}
	}
}

// TestExtractIntroducedSubcommands covers the "FR body mentions
// 'subcommand'" heuristic: subcommands cited inside a qualifying FR
// body are introduced; those in non-FR sections or FR bodies that
// don't say "subcommand" are not.
func TestExtractIntroducedSubcommands(t *testing.T) {
	spec := strings.Join([]string{
		"## Functional Requirements",
		"",
		"**FR-001** The orchestrator MUST add a new `chitin-orchestrator spec-lint` subcommand that ...",
		"",
		"**FR-002** Unrelated FR that mentions `chitin-orchestrator schedule` only in passing prose.",
		"",
		"## Out of scope",
		"",
		"This section mentions `chitin-kernel log-tail` but is outside any FR body.",
	}, "\n")

	got := extractIntroducedSubcommands(spec)
	want := map[string]bool{
		"chitin-orchestrator spec-lint": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractIntroducedSubcommands mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

// TestL05CLISurface_EndToEnd ties the rule together: allowlisted
// subcommands pass, FR-introduced subcommands pass, unknown
// subcommands fail, and findings carry the rule/file/line shape the
// JSON contract promises.
func TestL05CLISurface_EndToEnd(t *testing.T) {
	specMD := strings.Join([]string{
		"# Spec X",
		"",
		"## Functional Requirements",
		"",
		"**FR-001** Introduce a new `chitin-orchestrator spec-lint` subcommand.",
		"",
		"**FR-002** Use `gh api repos/foo/bar/issues` for issue lookup.",
		"",
		"**FR-003** Use `gh api -X POST repos/foo/bar/pulls/1/comments -f body=hi` to reply.",
		"",
		"## Notes",
		"",
		"Operators may invoke `chitin-orchestrator unknown-sub` here, which is NOT allowlisted.",
	}, "\n")

	tasksMD := strings.Join([]string{
		"- [ ] T001 Run `chitin-kernel log-tail` to verify",
		"- [ ] T002 Wrong endpoint `gh api /repos/foo/bar`",
	}, "\n")

	allow := []AllowlistEntry{
		{Binary: "chitin-kernel", Subcommand: "log-tail"},
	}

	findings := L05CLISurface("spec.md", specMD, "tasks.md", tasksMD, allow)

	type sig struct {
		File   string
		Line   int
		Substr string
	}

	// Build a quick index by (file, line) for assertions.
	byKey := map[string]Violation{}
	for _, f := range findings {
		if f.Rule != "L05" {
			t.Errorf("non-L05 rule in findings: %+v", f)
		}
		if f.Severity != SeverityError {
			t.Errorf("non-error severity in findings: %+v", f)
		}
		byKey[f.File+":"+strconv.Itoa(f.Line)] = f
	}

	// Expected violations.
	checks := []sig{
		{File: "spec.md", Line: lineOfFirst(specMD, "unknown-sub"), Substr: "unknown-sub"},
		{File: "tasks.md", Line: lineOfFirst(tasksMD, "/repos/foo/bar"), Substr: "/repos/foo/bar"},
	}
	for _, c := range checks {
		f, ok := byKey[c.File+":"+strconv.Itoa(c.Line)]
		if !ok {
			t.Errorf("missing expected finding for %s:%d (%q); got: %+v", c.File, c.Line, c.Substr, findings)
			continue
		}
		if !strings.Contains(f.Message, c.Substr) {
			t.Errorf("finding message %q does not contain %q", f.Message, c.Substr)
		}
	}

	// Allowlisted + FR-introduced + flagged-but-valid endpoints must NOT appear.
	for _, f := range findings {
		if strings.Contains(f.Message, "spec-lint") {
			t.Errorf("FR-introduced spec-lint should be allowed: %+v", f)
		}
		if strings.Contains(f.Message, "log-tail") {
			t.Errorf("allowlisted log-tail should be allowed: %+v", f)
		}
		if strings.Contains(f.Message, "pulls/1/comments") {
			t.Errorf("flagged repos/ endpoint should be allowed: %+v", f)
		}
	}

	if len(findings) != len(checks) {
		t.Fatalf("expected %d findings, got %d: %+v", len(checks), len(findings), findings)
	}
}

// TestL05CLISurface_EmptyInputs proves L05 is safe on empty inputs.
func TestL05CLISurface_EmptyInputs(t *testing.T) {
	if got := L05CLISurface("spec.md", "", "tasks.md", "", nil); got != nil {
		t.Errorf("expected nil findings for empty inputs, got %+v", got)
	}
}

// lineOfFirst returns the 1-based line number of the first occurrence
// of needle in text, or -1 if not found.
func lineOfFirst(text, needle string) int {
	idx := strings.Index(text, needle)
	if idx < 0 {
		return -1
	}
	return strings.Count(text[:idx], "\n") + 1
}
