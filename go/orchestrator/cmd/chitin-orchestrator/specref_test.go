package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSpecRef_ExactMatch(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"091-fix-clawta-lockdown-loop",
		"096-operator-session-state-surface",
		"097-operator-scheduler-entrypoint",
	})

	r, err := resolveSpecRef(repo, "096-operator-session-state-surface")
	if err != nil {
		t.Fatalf("expected exact match, got error: %v", err)
	}
	if r.SpecRef != "096-operator-session-state-surface" {
		t.Errorf("SpecRef = %q, want %q", r.SpecRef, "096-operator-session-state-surface")
	}
	if r.Numeric != "096" {
		t.Errorf("Numeric = %q, want %q", r.Numeric, "096")
	}
	if r.Slug != "operator-session-state-surface" {
		t.Errorf("Slug = %q, want %q", r.Slug, "operator-session-state-surface")
	}
}

func TestResolveSpecRef_NumericPrefixUnique(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"091-fix-clawta-lockdown-loop",
		"096-operator-session-state-surface",
		"097-operator-scheduler-entrypoint",
	})

	r, err := resolveSpecRef(repo, "096")
	if err != nil {
		t.Fatalf("expected unique numeric match, got: %v", err)
	}
	if r.SpecRef != "096-operator-session-state-surface" {
		t.Errorf("SpecRef = %q, want %q", r.SpecRef, "096-operator-session-state-surface")
	}
}

func TestResolveSpecRef_NumericPrefixAmbiguous(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"091-fix-clawta-lockdown-loop",
		"092-codify-swarm-orchestrator",
		"093-merge-queue-orchestrator",
		"094-pr-review-mechanism",
		"095-continue-checks-pilot",
		"096-operator-session-state-surface",
		"097-operator-scheduler-entrypoint",
	})

	_, err := resolveSpecRef(repo, "09")
	if err == nil {
		t.Fatal("expected ambiguous error, got nil")
	}
	var sre *SpecRefError
	if !errors.As(err, &sre) {
		t.Fatalf("expected *SpecRefError, got %T", err)
	}
	if sre.Kind != "ambiguous" {
		t.Errorf("Kind = %q, want %q", sre.Kind, "ambiguous")
	}
	if len(sre.Candidates) != 7 {
		t.Errorf("len(Candidates) = %d, want 7; got %v", len(sre.Candidates), sre.Candidates)
	}
	// Candidates must be sorted (deterministic output for the error message).
	for i := 1; i < len(sre.Candidates); i++ {
		if sre.Candidates[i-1] > sre.Candidates[i] {
			t.Errorf("candidates not sorted: %v", sre.Candidates)
			break
		}
	}
}

func TestResolveSpecRef_SlugMatch(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"096-operator-session-state-surface",
	})

	r, err := resolveSpecRef(repo, "operator-session-state-surface")
	if err != nil {
		t.Fatalf("expected slug match, got: %v", err)
	}
	if r.SpecRef != "096-operator-session-state-surface" {
		t.Errorf("SpecRef = %q, want %q", r.SpecRef, "096-operator-session-state-surface")
	}
}

func TestResolveSpecRef_NoMatch(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"091-fix-clawta-lockdown-loop",
	})

	_, err := resolveSpecRef(repo, "999-nonexistent")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	var sre *SpecRefError
	if !errors.As(err, &sre) {
		t.Fatalf("expected *SpecRefError, got %T", err)
	}
	if sre.Kind != "not-found" {
		t.Errorf("Kind = %q, want %q", sre.Kind, "not-found")
	}
}

func TestResolveSpecRef_NoSpecsDir(t *testing.T) {
	repo := t.TempDir()
	_, err := resolveSpecRef(repo, "096")
	if err == nil {
		t.Fatal("expected error for repo with no specs dir, got nil")
	}
	if !strings.Contains(err.Error(), "no specs directory") {
		t.Errorf("error = %q, want it to mention 'no specs directory'", err.Error())
	}
}

func TestResolveSpecRef_IgnoresNonSpecDirs(t *testing.T) {
	repo := writeSpecsFixture(t, []string{
		"097-operator-scheduler-entrypoint",
		"audit-2026-05-18", // no numeric prefix → skipped
		"INDEX.md.d",       // weird name → skipped
	})

	// 097 should still resolve cleanly.
	r, err := resolveSpecRef(repo, "097")
	if err != nil {
		t.Fatalf("expected 097 to resolve, got: %v", err)
	}
	if r.SpecRef != "097-operator-scheduler-entrypoint" {
		t.Errorf("SpecRef = %q, want %q", r.SpecRef, "097-operator-scheduler-entrypoint")
	}
}

// writeSpecsFixture creates a temp repo with a .specify/specs/ tree
// containing empty directories for each given name. Returns the repo root.
func writeSpecsFixture(t *testing.T, dirs []string) string {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, ".specify", "specs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("setup mkdir %s: %v", d, err)
		}
	}
	return repo
}
