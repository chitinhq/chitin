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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
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
	listPRFiles(ctx context.Context, repo string, prNumber int) ([]string, error)
}

type defaultSpecFilesLister struct{}

// listPRFiles calls `gh api --paginate repos/<repo>/pulls/<N>/files`.
// The repo slug ("owner/repo") is passed explicitly rather than
// relying on gh's `{owner}/{repo}` placeholder substitution — the
// factory-listen webhook handler does not chdir to a repo root, so
// placeholder resolution (which falls back to cwd git remote / $GH_REPO)
// would fail in production. The webhook payload carries the repo slug
// in p.Repository.FullName; pass it through. --paginate follows link
// headers so large PRs are fully enumerated; for the spec-class
// decision we need to see every file (one non-spec file flips the
// answer to false).
func (defaultSpecFilesLister) listPRFiles(ctx context.Context, repo string, prNumber int) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "gh", "api", "--paginate",
		fmt.Sprintf("repos/%s/pulls/%d/files", repo, prNumber),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api --paginate repos/%s/pulls/%d/files: %w: %s", repo, prNumber, err, strings.TrimSpace(stderr.String()))
	}
	var entries []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return nil, fmt.Errorf("gh api --paginate repos/%s/pulls/%d/files: parse JSON: %w", repo, prNumber, err)
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
// repo is the "owner/repo" slug (from p.Repository.FullName on the
// webhook payload). Returns false on gh-api failure or empty
// changeset — the routing fallback is spec 113's code-PR loop, which
// is the safer default when classification is uncertain.
func isSpecPR(repo string, prNumber int) bool {
	return isSpecPRWith(context.Background(), repo, prNumber, defaultSpecFilesLister{})
}

// isSpecPRWith is the injectable form used by tests; production
// callers go through isSpecPR.
func isSpecPRWith(ctx context.Context, repo string, prNumber int, lister specFilesLister) bool {
	files, err := lister.listPRFiles(ctx, repo, prNumber)
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
