package speclint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunL05_GhAPI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Violation
	}{
		{
			name: "valid repos endpoint on one line",
			in:   "Run `gh api repos/owner/repo/pulls/1` to fetch.\n",
		},
		{
			name: "invalid endpoint on one line",
			in:   "Try `gh api /pulls/1/comments/2/replies` to reply.\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 1, Severity: SeverityError,
				Message: `gh api path "/pulls/1/comments/2/replies" must start with 'repos/<owner>/<repo>/...'`,
			}},
		},
		{
			name: "flag before path: -X POST",
			in:   "Run `gh api -X POST /pulls/1/comments` here.\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 1, Severity: SeverityError,
				Message: `gh api path "/pulls/1/comments" must start with 'repos/<owner>/<repo>/...'`,
			}},
		},
		{
			name: "flag and -H header before path",
			in:   "Run `gh api -H 'Accept: x' --method GET repos/o/r/issues/1`.\n",
		},
		{
			name: "long-form flag before path",
			in:   "Run `gh api --method POST /pulls/1/comments`.\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 1, Severity: SeverityError,
				Message: `gh api path "/pulls/1/comments" must start with 'repos/<owner>/<repo>/...'`,
			}},
		},
		{
			// The motivating #1050 case: gh api on one line, endpoint on the next.
			name: "multiline markdown-wrapped gh api with bad path on next line",
			in:   "Call `gh api\n  /pulls/1/comments/2/replies` to reply.\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 2, Severity: SeverityError,
				Message: `gh api path "/pulls/1/comments/2/replies" must start with 'repos/<owner>/<repo>/...'`,
			}},
		},
		{
			name: "multiline shell `\\` continuation with bad path",
			in:   "gh api \\\n  -X POST \\\n  /pulls/1/comments\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 3, Severity: SeverityError,
				Message: `gh api path "/pulls/1/comments" must start with 'repos/<owner>/<repo>/...'`,
			}},
		},
		{
			name: "multiline shell `\\` continuation with valid path",
			in:   "gh api \\\n  -X POST \\\n  repos/o/r/pulls/1/comments\n",
		},
		{
			name: "blank line terminates multiline lookahead",
			in:   "gh api\n\n/pulls/1/comments\n",
		},
		{
			name: "subsequent gh invocation terminates lookahead",
			in:   "gh api\ngh pr list\n",
		},
		{
			name: "markdown header terminates lookahead",
			in:   "gh api\n\n## Next section\n\n/pulls/1\n",
		},
		{
			name: "template prose `gh api ...` is ignored",
			in:   "Use `gh api ...` to talk to GitHub.\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RunL05(L05Input{SpecFile: "spec.md", SpecContent: []byte(tc.in)})
			if err != nil {
				t.Fatalf("RunL05 returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("violations mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

func TestRunL05_ChitinCLI(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		allowlist string
		want      []Violation
	}{
		{
			name: "subcommand introduced inside FR body passes",
			in:   "**FR-001** Introduces `chitin-orchestrator spec-lint` for spec validation.\n",
		},
		{
			name: "subcommand mentioned in prose only is flagged",
			in:   "Spec body talks about chitin-orchestrator spec-lint without an FR.\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 1, Severity: SeverityError,
				Message: `CLI "chitin-orchestrator spec-lint" is not in .specify/known-cli-surfaces.txt and is not introduced by an FR-NNN in this spec`,
			}},
		},
		{
			name: "FR body ends at next FR header",
			in:   "**FR-001** Adds first thing.\n**FR-002** Adds chitin-orchestrator new-cmd.\n",
		},
		{
			name: "FR body ends at section header — mention after header is flagged",
			in:   "**FR-001** Adds first thing.\n\n## Next section\n\nchitin-orchestrator orphan\n",
			want: []Violation{{
				Rule: RuleL05, File: "spec.md", Line: 5, Severity: SeverityError,
				Message: `CLI "chitin-orchestrator orphan" is not in .specify/known-cli-surfaces.txt and is not introduced by an FR-NNN in this spec`,
			}},
		},
		{
			name:      "allowlist permits subcommand outside FR",
			in:        "Run chitin-orchestrator queue to inspect.\n",
			allowlist: "chitin-orchestrator queue\n",
		},
		{
			name:      "allowlist supports comments and blank lines",
			in:        "Run chitin-kernel scan now.\n",
			allowlist: "# preface\n\nchitin-kernel scan\n",
		},
		{
			name: "chitin-kernel subcommand is recognized",
			in:   "**FR-001** Adds chitin-kernel scan tooling.\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := L05Input{SpecFile: "spec.md", SpecContent: []byte(tc.in)}
			if tc.allowlist != "" {
				p := filepath.Join(t.TempDir(), "known-cli-surfaces.txt")
				if err := os.WriteFile(p, []byte(tc.allowlist), 0o644); err != nil {
					t.Fatalf("write allowlist: %v", err)
				}
				in.AllowlistPath = p
			}
			got, err := RunL05(in)
			if err != nil {
				t.Fatalf("RunL05 returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("violations mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

func TestRunL05_MissingAllowlistIsNotAnError(t *testing.T) {
	got, err := RunL05(L05Input{
		SpecFile:      "spec.md",
		SpecContent:   []byte("nothing of interest here\n"),
		AllowlistPath: "/definitely/does/not/exist/known-cli-surfaces.txt",
	})
	if err != nil {
		t.Fatalf("expected nil error for missing allowlist, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no violations, got %#v", got)
	}
}

func TestRunL05_ViolationsAreLineOrdered(t *testing.T) {
	// chitin violation on line 1, gh api violation on line 3 — sort should
	// produce them in line order regardless of internal processing order.
	in := "chitin-orchestrator orphan\n\ngh api /pulls/1\n"
	got, err := RunL05(L05Input{SpecFile: "spec.md", SpecContent: []byte(in)})
	if err != nil {
		t.Fatalf("RunL05 error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 violations, got %d: %#v", len(got), got)
	}
	if got[0].Line != 1 || got[1].Line != 3 {
		t.Errorf("expected lines [1, 3], got [%d, %d]", got[0].Line, got[1].Line)
	}
}
