package main

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// openBudgetStoreForTest is a tiny shim so the hook test file doesn't
// import gov for the test-helper-only call. Lives in a separate _test.go
// file to keep gate_hook_test.go focused on hook-specific assertions.
func openBudgetStoreForTest(t *testing.T, dbPath string) (*gov.BudgetStore, error) {
	t.Helper()
	return gov.OpenBudgetStore(dbPath)
}

func budgetLimits(_ *testing.T, calls, bytes int64) gov.BudgetLimits {
	return gov.BudgetLimits{MaxToolCalls: calls, MaxInputBytes: bytes}
}
