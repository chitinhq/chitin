// spec_pr_classify.go — spec 115 T001 / FR-001 / FR-002.
//
// Discriminator for the factory-listen webhook path: a PR is
// "spec-class" iff every file in its changeset lives inside a
// `.specify/specs/<NNN>-*/` directory. Spec-class PRs feed the
// SpecIterationWorkflow (T011); everything else falls through to spec
// 113's PRIterationWorkflow.
//
// The signature is bool, not (bool, error): callers want a routing
// decision, not an error to propagate. Any failure (gh exit, malformed
// JSON, empty changeset) returns false — fail-safe to the existing
// code-PR path rather than silently misrouting a real code review.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"time"
)

// specFilePathPattern matches files contained inside a real spec
// directory: `.specify/specs/<digits>-<slug>/<anything>`. The trailing
// slash in the pattern enforces "inside the dir", not "is the dir".
// Distinct from factory_listen.go's specPathPattern, which matches
// only tasks.md and captures the spec slug for push-event routing.
var specFilePathPattern = regexp.MustCompile(`^\.specify/specs/\d+-.*/`)

// specFilesLister returns the changed-file paths for a PR. Tests inject
// a fake; the default impl shells out to `gh api`.
type specFilesLister interface {
	listPRFiles(ctx context.Context, prNumber int) ([]string, error)
}

type defaultSpecFilesLister struct{}

// listPRFiles calls `gh api repos/{owner}/{repo}/pulls/<N>/files`
// using gh's placeholder substitution to resolve owner/repo from the
// current repository context ($GH_REPO or git remote). --paginate
// follows link headers so large PRs are fully enumerated; for the
// spec-class decision we need to see every file (one non-spec file
// flips the answer to false).
func (defaultSpecFilesLister) listPRFiles(ctx context.Context, prNumber int) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/files", prNumber),
		"--paginate",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api pulls/%d/files: %w", prNumber, err)
	}
	var entries []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("gh api pulls/%d/files: parse JSON: %w", prNumber, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		files = append(files, e.Filename)
	}
	return files, nil
}

// isSpecPR reports whether the PR's changeset is wholly contained
// under `.specify/specs/<NNN>-*/`. Spec 115 FR-001 / FR-002.
//
// Returns false on gh-api failure or empty changeset — the routing
// fallback is spec 113's code-PR loop, which is the safer default
// when classification is uncertain.
func isSpecPR(prNumber int) bool {
	return isSpecPRWith(context.Background(), prNumber, defaultSpecFilesLister{})
}

// isSpecPRWith is the injectable form used by tests; production
// callers go through isSpecPR.
func isSpecPRWith(ctx context.Context, prNumber int, lister specFilesLister) bool {
	files, err := lister.listPRFiles(ctx, prNumber)
	if err != nil || len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !specFilePathPattern.MatchString(f) {
			return false
		}
	}
	return true
}
