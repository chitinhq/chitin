package gov

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// TestMain dispatches into the cross-process Spend worker when the
// CHITIN_BUDGET_TEST_WORKER env var is set. This lets the concurrency
// test re-exec the test binary as 100 separate processes — exercising
// sqlite WAL cross-process atomicity, not just intra-process goroutine
// concurrency (which would only test database/sql's connection pool).
func TestMain(m *testing.M) {
	if os.Getenv("CHITIN_BUDGET_TEST_WORKER") == "1" {
		runSpendWorker()
		return
	}
	os.Exit(m.Run())
}

func runSpendWorker() {
	dbPath := os.Getenv("CHITIN_TEST_DB")
	envID := os.Getenv("CHITIN_TEST_ENVELOPE")
	if dbPath == "" || envID == "" {
		fmt.Println("missing env")
		os.Exit(2)
	}
	store, err := OpenBudgetStore(dbPath)
	if err != nil {
		fmt.Println("err:open")
		os.Exit(0)
	}
	defer store.Close()
	env, err := store.Load(envID)
	if err != nil {
		fmt.Println("err:load")
		os.Exit(0)
	}
	if err := env.Spend(CostDelta{ToolCalls: 1, InputBytes: 1}); err != nil {
		fmt.Println("err:" + err.Error())
		os.Exit(0)
	}
	fmt.Println("ok")
	os.Exit(0)
}

func tmpStore(t *testing.T) *BudgetStore {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenBudgetStore(filepath.Join(dir, "gov.db"))
	if err != nil {
		t.Fatalf("OpenBudgetStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestEnvelope_CreateAndLoad(t *testing.T) {
	store := tmpStore(t)
	limits := BudgetLimits{MaxToolCalls: 10, MaxInputBytes: 1024, BudgetUSD: 0.05}
	env, err := store.Create(limits)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if env.ID == "" || len(env.ID) != 26 {
		t.Fatalf("ULID shape wrong: %q", env.ID)
	}
	if env.Limits != limits {
		t.Fatalf("limits round-trip: got %+v want %+v", env.Limits, limits)
	}
	loaded, err := store.Load(env.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Limits != limits {
		t.Fatalf("loaded limits mismatch: got %+v want %+v", loaded.Limits, limits)
	}
}

func TestEnvelope_LoadNotFound(t *testing.T) {
	store := tmpStore(t)
	_, err := store.Load("01-NONEXISTENT-ID-VALUE-AB")
	if !errors.Is(err, ErrEnvelopeNotFound) {
		t.Fatalf("expected ErrEnvelopeNotFound, got %v", err)
	}
}

func TestEnvelope_Spend_DebitsAccumulate(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 5, MaxInputBytes: 1000})
	for i := 0; i < 3; i++ {
		if err := env.Spend(CostDelta{ToolCalls: 1, InputBytes: 100, USD: 0.01}); err != nil {
			t.Fatalf("Spend %d: %v", i, err)
		}
	}
	st, _ := env.Inspect()
	if st.SpentCalls != 3 {
		t.Fatalf("SpentCalls=%d want 3", st.SpentCalls)
	}
	if st.SpentBytes != 300 {
		t.Fatalf("SpentBytes=%d want 300", st.SpentBytes)
	}
	if st.SpentUSD < 0.029 || st.SpentUSD > 0.031 {
		t.Fatalf("SpentUSD=%v want ~0.03", st.SpentUSD)
	}
}

func TestEnvelope_Spend_ExhaustOnCalls_StaysClosed(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 2, MaxInputBytes: 0 /* uncapped */})

	if err := env.Spend(CostDelta{ToolCalls: 1}); err != nil {
		t.Fatalf("first spend: %v", err)
	}
	if err := env.Spend(CostDelta{ToolCalls: 1}); err != nil {
		t.Fatalf("second spend: %v", err)
	}
	// Third attempt would exceed cap → exhausted, sticky-close.
	err := env.Spend(CostDelta{ToolCalls: 1})
	if !errors.Is(err, ErrEnvelopeExhausted) {
		t.Fatalf("expected exhausted, got %v", err)
	}
	// Subsequent attempts: closed-state should produce closed error
	// (not exhausted; the test of stickiness is that it stays unable
	// to spend regardless of whether room was made).
	err2 := env.Spend(CostDelta{ToolCalls: 1})
	if !errors.Is(err2, ErrEnvelopeClosed) {
		t.Fatalf("expected closed on second attempt, got %v", err2)
	}
}

func TestEnvelope_Spend_ExhaustOnBytes(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 0, MaxInputBytes: 100})
	if err := env.Spend(CostDelta{InputBytes: 50}); err != nil {
		t.Fatalf("spend 50: %v", err)
	}
	if err := env.Spend(CostDelta{InputBytes: 50}); err != nil {
		t.Fatalf("spend cap: %v", err)
	}
	err := env.Spend(CostDelta{InputBytes: 1})
	if !errors.Is(err, ErrEnvelopeExhausted) {
		t.Fatalf("expected exhausted, got %v", err)
	}
}

func TestEnvelope_Grant_RaisesCapAndReopens(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 1})
	_ = env.Spend(CostDelta{ToolCalls: 1})
	if err := env.Spend(CostDelta{ToolCalls: 1}); !errors.Is(err, ErrEnvelopeExhausted) {
		t.Fatalf("expected exhausted, got %v", err)
	}

	if err := env.Grant(5, 0, 0, "operator-test"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if env.Limits.MaxToolCalls != 6 {
		t.Fatalf("Limits.MaxToolCalls=%d want 6", env.Limits.MaxToolCalls)
	}
	// Envelope reopened; Spend should now succeed.
	if err := env.Spend(CostDelta{ToolCalls: 1}); err != nil {
		t.Fatalf("post-grant Spend: %v", err)
	}
	st, _ := env.Inspect()
	if st.ClosedAt != "" {
		t.Fatalf("expected reopened envelope, ClosedAt=%q", st.ClosedAt)
	}
}

func TestEnvelope_CloseEnvelope_StickyAcrossSpend(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 100})
	if err := env.CloseEnvelope(); err != nil {
		t.Fatalf("CloseEnvelope: %v", err)
	}
	err := env.Spend(CostDelta{ToolCalls: 1})
	if !errors.Is(err, ErrEnvelopeClosed) {
		t.Fatalf("expected closed, got %v", err)
	}
}

func TestEnvelope_Spend_NegativeRejected(t *testing.T) {
	store := tmpStore(t)
	env, _ := store.Create(BudgetLimits{MaxToolCalls: 100})
	err := env.Spend(CostDelta{ToolCalls: -1})
	if err == nil {
		t.Fatalf("expected error on negative spend")
	}
}

// TestEnvelope_ConcurrentSpend_CrossProcessExact spawns N subprocess
// re-exec workers, each opening its own BudgetStore against the shared
// sqlite file and attempting one Spend(1). Cap is set below the worker
// count so we know exactly how many should succeed. The invariant:
// successful spends == cap, period — no over-spend, no lost spends.
func TestEnvelope_ConcurrentSpend_CrossProcessExact(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-process spend test in -short mode")
	}
	const workers = 100
	const cap = 30

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")

	// Set up the envelope using one store; then close it so workers
	// have no in-process competition for the connection.
	{
		store, err := OpenBudgetStore(dbPath)
		if err != nil {
			t.Fatalf("OpenBudgetStore: %v", err)
		}
		env, err := store.Create(BudgetLimits{MaxToolCalls: cap})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		envID := env.ID
		store.Close()

		bin := os.Args[0]
		var wg sync.WaitGroup
		var oks atomic.Int64
		var fails atomic.Int64
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cmd := exec.Command(bin, "-test.run=TestNoOp")
				cmd.Env = append(os.Environ(),
					"CHITIN_BUDGET_TEST_WORKER=1",
					"CHITIN_TEST_DB="+dbPath,
					"CHITIN_TEST_ENVELOPE="+envID,
				)
				out, _ := cmd.CombinedOutput()
				if bytes.HasPrefix(out, []byte("ok\n")) {
					oks.Add(1)
				} else {
					fails.Add(1)
				}
			}()
		}
		wg.Wait()

		if got := int(oks.Load()); got != cap {
			t.Fatalf("successful spends = %d, want exactly %d (cap)", got, cap)
		}
		if got := int(fails.Load()); got != workers-cap {
			t.Fatalf("failed spends = %d, want exactly %d", got, workers-cap)
		}

		// Final envelope state: spent_calls == cap, closed.
		store2, _ := OpenBudgetStore(dbPath)
		defer store2.Close()
		env2, err := store2.Load(envID)
		if err != nil {
			t.Fatalf("post Load: %v", err)
		}
		st, err := env2.Inspect()
		if err != nil {
			t.Fatalf("post Inspect: %v", err)
		}
		if st.SpentCalls != cap {
			t.Fatalf("SpentCalls=%d want %d", st.SpentCalls, cap)
		}
		if st.ClosedAt == "" {
			t.Fatalf("envelope expected closed (ClosedAt empty)")
		}
	}
}

// TestNoOp is the placeholder test the worker subprocess is matched
// against (with -test.run=TestNoOp). The TestMain dispatch fires
// before any test runs, so this never executes in the worker; in
// normal test runs it's a 0-cost no-op.
func TestNoOp(t *testing.T) {}
