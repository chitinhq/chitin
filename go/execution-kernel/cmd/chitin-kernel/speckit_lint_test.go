package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSpeckitLint_GoodFixture_ReturnsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSpeckitLint(
		[]string{"../../internal/speckit/testdata/good"},
		&stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("expected exit 0 on good fixture, got %d\nstdout=%q\nstderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "clean") {
		t.Fatalf("expected 'clean' in stdout, got %q", stdout.String())
	}
}

func TestSpeckitLint_BadFixture_ReturnsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSpeckitLint(
		[]string{"../../internal/speckit/testdata/placeholder"},
		&stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("expected exit 1 on bad fixture, got %d\nstdout=%q\nstderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "template-placeholder") {
		t.Fatalf("expected 'template-placeholder' in stdout, got %q", stdout.String())
	}
}

func TestSpeckitLint_JSONFlag_EmitsValidJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSpeckitLint(
		[]string{"--json", "../../internal/speckit/testdata/placeholder"},
		&stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("expected exit 1 with --json on bad fixture, got %d\nstdout=%q\nstderr=%q", code, stdout.String(), stderr.String())
	}
	var parsed struct {
		SpecDir  string `json:"spec_dir"`
		Findings []struct {
			CheckID string `json:"check_id"`
		} `json:"findings"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("expected valid JSON, got error %v\nstdout=%q", err, stdout.String())
	}
	if len(parsed.Findings) == 0 {
		t.Fatalf("expected at least one finding in JSON output, got none")
	}
	if parsed.Counts["template-placeholder"] == 0 {
		t.Fatalf("expected template-placeholder count > 0 in JSON counts, got %v", parsed.Counts)
	}
}

func TestSpeckitLint_NoArgs_ReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSpeckitLint(nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 on missing arg, got %d\nstdout=%q\nstderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected usage in stderr, got %q", stderr.String())
	}
}

func TestSpeckitLint_NonexistentDir_ReturnsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSpeckitLint(
		[]string{"/tmp/this-dir-does-not-exist-for-speckit-lint-test"},
		&stdout, &stderr,
	)
	if code != 2 {
		t.Fatalf("expected exit 2 on missing dir, got %d\nstdout=%q\nstderr=%q", code, stdout.String(), stderr.String())
	}
}
