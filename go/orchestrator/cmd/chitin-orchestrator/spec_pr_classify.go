// spec_pr_classify.go — spec 115 T001 / US1 / FR-001.
//
// Discriminator that extends the factory-listen /webhook/pr eligibility
// surface (spec 113 FR-001): is this pull request "spec-class"? A PR is
// spec-class iff every changed file is contained under
// `.specify/specs/NNN-*/`. Spec-class PRs route to SpecIterationWorkflow
// (spec 115 T011/T015); everything else — including mixed PRs that
// touch both spec dirs and code — keeps flowing through spec 113's
// PRIterationWorkflow loop unchanged, per the "Spec PR also modifies
// code" edge case in spec 115 §Edge cases.
//
// FR-002: the discriminator is computed from the same gh-api endpoint
// the iteration loop already uses (`repos/<owner>/<repo>/pulls/<N>/files`),
// not a new endpoint.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

// specPathRegex matches a file path entirely contained under
// `.specify/specs/NNN-...` where NNN is one or more digits. The
// trailing `/` after the spec-id segment means a hypothetical entry
// named `.specify/specs/115-foo` (no trailing slash, no nested file)
// would NOT match — that case never arises from the pulls/files
// endpoint, which lists files not directories.
var specPathRegex = regexp.MustCompile(`^\.specify/specs/\d+-.*/`)

// prFileEntry is the subset of the GitHub
// `repos/{owner}/{repo}/pulls/{pull_number}/files` payload this
// discriminator consumes. The full response carries
// additions/deletions/patch fields that are irrelevant here — only the
// filename matters for classification.
type prFileEntry struct {
	Filename string `json:"filename"`
}

// fetchPRFiles is the package-level shell-out hook. Tests reassign it
// to return canned entries; production resolves to fetchPRFilesViaGH.
var fetchPRFiles = fetchPRFilesViaGH

// isSpecPR returns true iff every changed file in the PR matches
// specPathRegex (i.e. is contained under `.specify/specs/NNN-*/`).
//
// Returns false on:
//   - any file outside the spec prefix (mixed or pure-code PRs)
//   - empty file list (no changes — treat as non-spec; spec PRs always
//     touch at least one file)
//   - any `gh api` failure (conservative fallback so the PR continues
//     through spec 113's existing loop rather than silently stalling)
//
// Fail-closed for spec routing is the right default: false on error
// keeps existing factory behavior intact.
func isSpecPR(ctx context.Context, repo string, prNumber int) bool {
	if repo == "" || prNumber <= 0 {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	files, err := fetchPRFiles(cctx, repo, prNumber)
	if err != nil {
		return false
	}
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !specPathRegex.MatchString(f.Filename) {
			return false
		}
	}
	return true
}

// fetchPRFilesViaGH shells out to
// `gh api --paginate repos/<repo>/pulls/<N>/files` and decodes the JSON
// array into prFileEntry values. `--paginate` ensures PRs with >30
// changed files (the endpoint's default page size) return their full
// file list rather than being silently truncated — important for the
// "all files match" invariant the discriminator depends on.
func fetchPRFilesViaGH(ctx context.Context, repo string, prNumber int) ([]prFileEntry, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	endpoint := fmt.Sprintf("repos/%s/pulls/%d/files", repo, prNumber)
	cmd := exec.CommandContext(ctx, "gh", "api", "--paginate", endpoint)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", endpoint, err, stderr.String())
	}
	var files []prFileEntry
	if err := json.Unmarshal(stdout.Bytes(), &files); err != nil {
		return nil, fmt.Errorf("decode pulls/%d/files: %w", prNumber, err)
	}
	return files, nil
}
