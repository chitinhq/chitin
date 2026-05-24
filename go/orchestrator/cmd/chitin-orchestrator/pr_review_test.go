package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGhResolver lets unit tests bypass the real gh CLI.
type fakeGhResolver struct {
	repo   string
	author string
	err    error
}

func (f fakeGhResolver) resolveRepoAndAuthor(_ context.Context, _ int) (string, string, error) {
	return f.repo, f.author, f.err
}

// noopGhResolver fails fast if the runPRReview code path ever calls
// gh — used by tests that pre-resolve --repo + --author explicitly.
type noopGhResolver struct{}

func (noopGhResolver) resolveRepoAndAuthor(_ context.Context, _ int) (string, string, error) {
	return "", "", errors.New("noopGhResolver: gh should not have been called")
}

// TestRunPRReview_BadArgs covers the input-validation paths that exit
// with exitUserError before any Temporal or gh interaction. Each table
// row exercises a specific reject condition; the test asserts both the
// exit code and that the stderr message names the offending field so
// the operator sees what to fix.
func TestRunPRReview_BadArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantErrIn string
	}{
		{"no PR#", []string{}, "exactly one positional argument"},
		{"negative PR#", []string{"--", "-3"}, "positive integer"},
		{"zero PR#", []string{"0"}, "positive integer"},
		{"non-numeric PR#", []string{"abc"}, "positive integer"},
		{"too many args", []string{"1", "2"}, "exactly one positional argument"},
		{"bad policy-class", []string{"--policy-class", "wrong", "1"}, "policy-class"},
		{"bad arbiter", []string{"--arbiter", "wrong", "1"}, "arbiter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runPRReview(context.Background(), c.args, &stdout, &stderr, noopGhResolver{})
			if code != exitUserError {
				t.Errorf("exit code = %d, want %d (exitUserError); stderr=%q", code, exitUserError, stderr.String())
			}
			if !strings.Contains(stderr.String(), c.wantErrIn) {
				t.Errorf("stderr does not contain %q; got: %q", c.wantErrIn, stderr.String())
			}
		})
	}
}

// TestRunPRReview_GhFailure confirms gh failures during auto-resolution
// surface as runtime-error exit codes with a user-facing message.
func TestRunPRReview_GhFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runPRReview(context.Background(),
		[]string{"42"},
		&stdout, &stderr,
		fakeGhResolver{err: errors.New("gh: not found")},
	)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d", code, exitRuntimeError)
	}
	if !strings.Contains(stderr.String(), "could not look up PR #42") {
		t.Errorf("stderr does not surface gh failure: %q", stderr.String())
	}
}

// unreachableTemporal is a host:port that's guaranteed not to accept
// Temporal connections. Used in tests so the dial fails reproducibly
// whether or not the developer has temporal-dev running locally.
// 0.0.0.0:1 — port 1 is reserved (tcpmux) and not bound by anything;
// dial fails fast with "connection refused" without making any network
// noise.
const unreachableTemporal = "0.0.0.0:1"

// TestRunPRReview_AllFlagsAvoidsGh confirms that providing --repo and
// --author explicitly skips the gh auto-detection path entirely (the
// noopGhResolver would error if it were called).
//
// Uses --temporal-host with an unreachable address so the test fails at
// the Temporal-dial step regardless of whether the developer has
// temporal-dev running. Asserts the gh-skip happened and the failure
// point is Temporal, not gh.
func TestRunPRReview_AllFlagsAvoidsGh(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runPRReview(context.Background(),
		[]string{"--repo", "x/y", "--author", "u", "--temporal-host", unreachableTemporal, "42"},
		&stdout, &stderr,
		noopGhResolver{}, // would error if gh path is hit
	)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d (Temporal dial); stderr=%q", code, exitRuntimeError, stderr.String())
	}
	if strings.Contains(stderr.String(), "gh should not have been called") {
		t.Errorf("noopGhResolver was called despite --repo + --author both set; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Temporal unreachable") {
		t.Errorf("stderr did not surface Temporal-dial failure as expected: %q", stderr.String())
	}
}

// TestRunPRReview_PartialFlagsTriggerGh confirms that providing ONLY
// --repo (not --author) still triggers gh auto-detection — the
// resolver must fill in the missing author field. Uses unreachable
// Temporal so the test reaches the dial step deterministically.
func TestRunPRReview_PartialFlagsTriggerGh(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runPRReview(context.Background(),
		[]string{"--repo", "x/y", "--temporal-host", unreachableTemporal, "42"}, // --author omitted
		&stdout, &stderr,
		fakeGhResolver{repo: "x/y", author: "from-gh", err: nil},
	)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d; stderr=%q", code, exitRuntimeError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Temporal unreachable") {
		t.Errorf("stderr does not surface Temporal failure: %q", stderr.String())
	}
}

// TestRepoFromURL covers the parser for the URL field gh returns. The
// happy path is the chitinhq/chitin pattern; the edge cases are
// non-GitHub URLs and malformed shapes (no /pull/, missing org).
func TestRepoFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/chitinhq/chitin/pull/953", "chitinhq/chitin"},
		{"https://github.com/org/repo/pull/1", "org/repo"},
		{"https://github.com/org/repo", "org/repo"},
		{"", ""},
		{"https://example.com/foo/bar", ""},
		{"https://github.com/", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := repoFromURL(c.in); got != c.want {
				t.Errorf("repoFromURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestValidPolicyClasses confirms the closed enum matches the six
// classes spec 093 names. A drift here would let a typo reach the
// workflow's PolicyClass field undetected.
func TestValidPolicyClasses(t *testing.T) {
	want := map[string]bool{
		"governance": true, "spec-only": true, "impl": true,
		"live-fix": true, "bookkeeping": true, "research-docs": true,
	}
	if len(validPolicyClasses) != len(want) {
		t.Errorf("validPolicyClasses has %d entries, want %d", len(validPolicyClasses), len(want))
	}
	for k := range want {
		if !validPolicyClasses[k] {
			t.Errorf("missing class %q from validPolicyClasses", k)
		}
	}
	for k := range validPolicyClasses {
		if !want[k] {
			t.Errorf("unexpected class %q in validPolicyClasses", k)
		}
	}
}
