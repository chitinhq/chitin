// validate_driver_coverage_test.go — spec 105 FR-006/007/008 tests.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestProductionRegistry_CoversEveryTaxonomyCapability — FR-008 regression.
// Builds the production registry via buildRegistry() and asserts that
// every Capability in driver.KnownCapabilities() has ≥ 1 declaring
// driver. Fails on any future taxonomy addition that lacks an implementer.
func TestProductionRegistry_CoversEveryTaxonomyCapability(t *testing.T) {
	registry, err := buildUnfilteredRegistry()
	if err != nil {
		t.Fatalf("buildUnfilteredRegistry: %v", err)
	}
	var missing []driver.Capability
	for _, c := range driver.KnownCapabilities() {
		if len(registry.DriversDeclaring(c)) == 0 {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("taxonomy entries with zero declaring drivers (add CapXxx to a driver's Capabilities slice):\n  %v", missing)
	}
}

// TestRunValidateDriverCoverage_ExitsZero_WhenFullCoverage exercises
// FR-004 happy path against the production registry — after FR-001/002
// landed, every capability has a declarer so the subcommand exits 0.
func TestRunValidateDriverCoverage_ExitsZero_WhenFullCoverage(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runValidateDriverCoverage(context.Background(), nil, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want 0; stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "code.implement") {
		t.Errorf("stdout should list taxonomy capabilities; got: %q", out.String())
	}
}

// TestRunValidateDriverCoverage_JSONOutput exercises FR-005 — machine
// readable output with the declared CoverageRow shape.
func TestRunValidateDriverCoverage_JSONOutput(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runValidateDriverCoverage(context.Background(), []string{"--json"}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want 0; stderr=%q", code, errBuf.String())
	}
	var rows []CoverageRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json unmarshal: %v\nout=%s", err, out.String())
	}
	var rawRows []map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &rawRows); err != nil {
		t.Fatalf("json raw unmarshal: %v\nout=%s", err, out.String())
	}
	if len(rawRows) == 0 || rawRows[0]["impl_drivers"] == nil || rawRows[0]["review_drivers"] == nil {
		t.Fatalf("json output must include impl_drivers and review_drivers keys; out=%s", out.String())
	}
	if len(rows) != len(driver.KnownCapabilities()) {
		t.Errorf("got %d rows; want %d (one per known capability)", len(rows), len(driver.KnownCapabilities()))
	}
	// Find test.author row and assert codex + claudecode are declared.
	for _, r := range rows {
		if r.Capability != "test.author" {
			continue
		}
		seen := map[string]bool{}
		for _, id := range r.DeclaringIDs {
			seen[id] = true
		}
		for _, want := range []string{"codex", "claudecode"} {
			if !seen[want] {
				t.Errorf("test.author declarers = %v; want includes %q (spec 105 FR-001/002)", r.DeclaringIDs, want)
			}
		}
	}
}

func TestRunValidateDriverCoverage_TableShowsTieredPools(t *testing.T) {
	t.Setenv("CHITIN_DRIVER_ALLOW", "")
	t.Setenv("CHITIN_DRIVER_ALLOW_IMPL", "codex")
	t.Setenv("CHITIN_DRIVER_ALLOW_REVIEW", "codex,claudecode")

	var out, errBuf bytes.Buffer
	code := runValidateDriverCoverage(context.Background(), nil, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want 0; stderr=%q", code, errBuf.String())
	}
	rendered := out.String()
	for _, want := range []string{"capability", "impl", "review"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("stdout missing %q column/header:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "code.review") ||
		!strings.Contains(rendered, "codex   claudecode, codex") {
		t.Fatalf("stdout does not show distinct impl/review pools for code.review:\n%s", rendered)
	}
}

// TestCoverageRows_FlagsMissingDeclarer creates a synthetic registry with
// a deliberate gap and asserts coverageRows surfaces it.
func TestCoverageRows_FlagsMissingDeclarer(t *testing.T) {
	// Build a minimal registry that excludes the one driver declaring
	// CapBrowserAutomate (no driver declares it today, but we verify the
	// detection mechanism works on a synthetic gap).
	reg := driver.NewRegistry()
	rows := coverageRows(context.Background(), reg)
	for _, r := range rows {
		if len(r.DeclaringIDs) != 0 {
			t.Errorf("empty registry should produce zero declarers for every capability; got %s → %v",
				r.Capability, r.DeclaringIDs)
		}
	}
}
