// spec_pr_classify.go — spec 115 US1 FR-001/FR-002 spec-PR discriminator.
//
// A "spec PR" is any pull request whose changeset is wholly contained under
// `.specify/specs/NNN-*/`. Spec PRs route to `SpecIterationWorkflow` (T011);
// every other PR — including a mixed-class PR that touches both spec files
// and code — falls through to spec 113's `PRIterationWorkflow` per the
// spec-115 edge case ("Spec PR also modifies code").
//
// Discrimination is computed from the GitHub PR files endpoint (FR-002 —
// no new gh-api surface): `gh api repos/<owner>/<repo>/pulls/<N>/files`,
// paginated to cover PRs that exceed the default 30-item page.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// specPRPathPattern matches a file path under `.specify/specs/NNN-*/` —
// FR-001's "spec-class" predicate, per-file. Anchored at the start so a
// suffix match (a file that happens to contain the substring) doesn't
// false-positive.
var specPRPathPattern = regexp.MustCompile(`^\.specify/specs/\d+-.*/`)

// prFilesLister is the narrow seam isSpecPR uses to fetch the changeset of a
// PR. The default implementation shells out to `gh api`; tests inject a
// fixture-driven stub.
type prFilesLister func(ctx context.Context, repo string, prNumber int) ([]string, error)

// isSpecPR returns true iff every file changed in PR #prNumber under `repo`
// is matched by specPRPathPattern. A PR with zero changed files is NOT a
// spec PR — there is nothing to review and the trigger should fall through.
//
// The signature carries a context + repo slug + lister-injection seam beyond
// the bare `isSpecPR(prNumber int) bool` named in spec 115 T001, because the
// gh-api call needs the owner/repo and the unit tests need a deterministic
// stand-in for the network round-trip. The caller (factory-listen webhook
// handler in `factory_listen.go`) already has both `p.Repository.FullName`
// and `r.Context()` in scope.
//
// Passing `nil` for `lister` selects the production gh-shelling impl. The
// error return is non-nil only when the lister fails (gh missing, network
// glitch, malformed JSON). A bool of false is returned in that case so the
// caller can route the PR to the code path by default — a safe fallback,
// since the worst case is that a spec PR gets handled by spec 113's loop
// instead of the spec-tuned loop. The reverse (a code PR misclassified as a
// spec PR) would invoke the spec-author driver on code and corrupt the PR.
func isSpecPR(ctx context.Context, repo string, prNumber int, lister prFilesLister) (bool, error) {
	if lister == nil {
		lister = listPRFilesViaGH
	}
	files, err := lister(ctx, repo, prNumber)
	if err != nil {
		return false, fmt.Errorf("list pr files for #%d: %w", prNumber, err)
	}
	return allPathsUnderSpecifySpecs(files), nil
}

// allPathsUnderSpecifySpecs is the pure-logic core of isSpecPR: returns true
// iff `filenames` is non-empty AND every entry matches specPRPathPattern.
// Pure function — exported (lowercase but package-scoped) for direct unit
// testing without faking the gh-api round trip.
func allPathsUnderSpecifySpecs(filenames []string) bool {
	if len(filenames) == 0 {
		return false
	}
	for _, f := range filenames {
		if !specPRPathPattern.MatchString(f) {
			return false
		}
	}
	return true
}

// listPRFilesViaGH shells out to `gh api --paginate
// repos/<owner>/<repo>/pulls/<N>/files` and returns every entry's `filename`.
// --paginate walks Link-header pages so a PR with >30 changed files (the
// default page size) is fully covered; without it a mixed-class PR with the
// last file off-page would silently misclassify as spec-class.
func listPRFilesViaGH(ctx context.Context, repo string, prNumber int) ([]string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not available: %w", err)
	}
	path := fmt.Sprintf("repos/%s/pulls/%d/files", repo, prNumber)
	cmd := exec.CommandContext(ctx, "gh", "api", "--paginate", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh api %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	var entries []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return nil, fmt.Errorf("decode pr files json for #%d: %w", prNumber, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Filename)
	}
	return out, nil
}
