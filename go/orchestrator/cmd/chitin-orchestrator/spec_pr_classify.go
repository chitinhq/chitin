// spec_pr_classify.go — spec 115 FR-001 + FR-002: spec-PR
// discriminator extending the factory-listen webhook eligibility
// surface (spec 113 FR-001). A PR is "spec-class" iff every
// changed file sits strictly under `.specify/specs/NNN-*/`. Spec
// PRs route to SpecIterationWorkflow (T011); code PRs route to
// spec 113's PRIterationWorkflow unchanged.
//
// Failure mode is fail-CLOSED on spec-class: any gh-api or parse
// error returns false (treat as code PR). This matches the FR-001
// "spec PR also modifies code" edge case — a partial signal must
// not silently bypass spec 113's review path. The conservative
// default also keeps a discriminator outage from misrouting code
// PRs into the (slower, spec-tuned) iteration loop.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// specPathRe matches any path strictly under a `.specify/specs/NNN-*/`
// directory. The trailing `/` is load-bearing — a bare directory
// path (no file segment) does not match, but GitHub's pull-files API
// only ever returns files so this is a defensive bound, not a
// reachable case.
var specPathRe = regexp.MustCompile(`^\.specify/specs/\d+-.*/`)

// specPRFilesRunner is the test seam for the `gh api .../pulls/N/files`
// shell-out. Production binds defaultSpecPRFilesRunner; the package-
// level runSpecPRFiles binding is swapped by tests.
type specPRFilesRunner func(ctx context.Context, repo string, prNumber int) ([]byte, error)

func defaultSpecPRFilesRunner(ctx context.Context, repo string, prNumber int) ([]byte, error) {
	// --paginate walks the full file list. Without it, GitHub caps
	// the response at 100 files — a 101-file PR whose first 100
	// files happen to be under .specify/specs/ would misclassify
	// as spec-class even when later pages add code files.
	args := []string{
		"api",
		"--paginate",
		fmt.Sprintf("repos/%s/pulls/%d/files", repo, prNumber),
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return nil, fmt.Errorf("gh api pulls/%d/files: %w; stderr=%s", prNumber, err, stderr)
		}
		return nil, fmt.Errorf("gh api pulls/%d/files: %w", prNumber, err)
	}
	return out, nil
}

// runSpecPRFiles is the package-level binding. Tests swap it in a
// deferred teardown so they stay hermetic without invoking gh.
var runSpecPRFiles specPRFilesRunner = defaultSpecPRFilesRunner

// isSpecPR returns true iff every changed file in the named PR is
// under `.specify/specs/NNN-*/`. The owner/repo string is the
// `owner/repo` slug GitHub puts in webhook payloads at
// `repository.full_name`; the caller (factory_listen) reads it from
// the parsed payload before invoking the discriminator.
//
// Returns false on:
//   - empty repo or non-positive PR number (caller bug; defensive)
//   - gh-api failure (network, auth, repo not found)
//   - JSON parse failure
//   - empty file list (a PR with zero changed files cannot be classified
//     as spec-class — vacuous-true is the wrong default here per the
//     fail-closed-on-spec-class policy)
//   - any single file path that does not match specPathRe
func isSpecPR(ctx context.Context, repo string, prNumber int) bool {
	if repo == "" || prNumber <= 0 {
		return false
	}
	raw, err := runSpecPRFiles(ctx, repo, prNumber)
	if err != nil {
		return false
	}
	files, err := parseSpecPRFiles(raw)
	if err != nil || len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !specPathRe.MatchString(f) {
			return false
		}
	}
	return true
}

// ghPullsFile is the subset of GitHub's pull-files response we read.
// The full response carries patch/sha/status fields; the
// discriminator only consumes filename.
type ghPullsFile struct {
	Filename string `json:"filename"`
}

// parseSpecPRFiles tolerates both shapes `gh api --paginate` may
// emit: a single concatenated JSON array (the common case) and a
// stream of arrays separated by whitespace (the shape gh produces
// when a paged endpoint returns naked top-level arrays). The
// streaming decoder handles both transparently.
func parseSpecPRFiles(raw []byte) ([]string, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	var all []string
	for dec.More() {
		var page []ghPullsFile
		if err := dec.Decode(&page); err != nil {
			return nil, err
		}
		for _, f := range page {
			all = append(all, f.Filename)
		}
	}
	return all, nil
}
