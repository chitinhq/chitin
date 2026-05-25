// spec_pr_classify_test.go — spec 115 T001 unit tests for isSpecPR.
// Tests inject fetchPRFiles to avoid shelling out to a real gh binary.

package main

import (
	"context"
	"errors"
	"testing"
)

func TestIsSpecPR(t *testing.T) {
	cases := []struct {
		name   string
		files  []prFileEntry
		fetchErr error
		want   bool
	}{
		{
			name: "pure_spec_pr_single_file",
			files: []prFileEntry{
				{Filename: ".specify/specs/115-spec-review-gate/spec.md"},
			},
			want: true,
		},
		{
			name: "pure_spec_pr_multiple_files_same_spec",
			files: []prFileEntry{
				{Filename: ".specify/specs/115-spec-review-gate/spec.md"},
				{Filename: ".specify/specs/115-spec-review-gate/tasks.md"},
			},
			want: true,
		},
		{
			name: "pure_spec_pr_multiple_specs",
			files: []prFileEntry{
				{Filename: ".specify/specs/114-operator-escalation-surface/spec.md"},
				{Filename: ".specify/specs/115-spec-review-gate/spec.md"},
			},
			want: true,
		},
		{
			name: "mixed_pr_spec_and_code",
			files: []prFileEntry{
				{Filename: ".specify/specs/115-spec-review-gate/spec.md"},
				{Filename: "go/orchestrator/cmd/chitin-orchestrator/main.go"},
			},
			want: false,
		},
		{
			name: "pure_code_pr",
			files: []prFileEntry{
				{Filename: "go/orchestrator/cmd/chitin-orchestrator/main.go"},
			},
			want: false,
		},
		{
			name: "non_spec_under_specify_dir",
			files: []prFileEntry{
				{Filename: ".specify/known-cli-surfaces.txt"},
			},
			want: false,
		},
		{
			name: "spec_dir_without_numeric_prefix",
			files: []prFileEntry{
				{Filename: ".specify/specs/foo/spec.md"},
			},
			want: false,
		},
		{
			name: "empty_file_list",
			files: []prFileEntry{},
			want: false,
		},
		{
			name:     "gh_api_failure",
			fetchErr: errors.New("gh: 502 Bad Gateway"),
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := fetchPRFiles
			defer func() { fetchPRFiles = prev }()
			fetchPRFiles = func(_ context.Context, _ string, _ int) ([]prFileEntry, error) {
				if tc.fetchErr != nil {
					return nil, tc.fetchErr
				}
				return tc.files, nil
			}
			got := isSpecPR(context.Background(), "chitinhq/chitin", 1234)
			if got != tc.want {
				t.Fatalf("isSpecPR() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSpecPR_InvalidInputs(t *testing.T) {
	called := false
	prev := fetchPRFiles
	defer func() { fetchPRFiles = prev }()
	fetchPRFiles = func(_ context.Context, _ string, _ int) ([]prFileEntry, error) {
		called = true
		return nil, nil
	}

	if isSpecPR(context.Background(), "", 1234) {
		t.Error("expected false for empty repo")
	}
	if isSpecPR(context.Background(), "chitinhq/chitin", 0) {
		t.Error("expected false for zero PR number")
	}
	if isSpecPR(context.Background(), "chitinhq/chitin", -1) {
		t.Error("expected false for negative PR number")
	}
	if called {
		t.Error("fetchPRFiles should not be called on invalid inputs")
	}
}
