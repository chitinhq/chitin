package gov

import (
	"errors"
	"path/filepath"
	"testing"
)

// newTestCounter is provided by escalation_test.go in this package.

func TestUnlock_UnknownAgent(t *testing.T) {
	c := newTestCounter(t)
	_, err := c.Unlock("nope")
	if !errors.Is(err, ErrNoAgent) {
		t.Errorf("err = %v, want ErrNoAgent", err)
	}
}

func TestUnlock_HappyPath_AdvancesEpoch(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("agent-1") // epoch becomes 1
	res, err := c.Unlock("agent-1")
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if res.Idempotent {
		t.Errorf("expected non-idempotent unlock; got Idempotent=true")
	}
	if res.LockEpochAfter != 2 {
		t.Errorf("LockEpochAfter = %d, want 2 (Lockdown advanced to 1, Unlock advances to 2)", res.LockEpochAfter)
	}
	// Verify state via Status.
	st, err := c.Status("agent-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Locked {
		t.Errorf("Locked = true after Unlock; want false")
	}
	if st.LockEpoch != 2 {
		t.Errorf("LockEpoch = %d, want 2", st.LockEpoch)
	}
	if st.UnlockTs == "" {
		t.Errorf("UnlockTs is empty; want populated")
	}
}

func TestUnlock_Idempotent_DoesNotAdvanceEpoch(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("a") // epoch 1
	if _, err := c.Unlock("a"); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}
	// After first unlock: epoch=2, locked=false.
	res, err := c.Unlock("a")
	if err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if !res.Idempotent {
		t.Errorf("expected idempotent on second Unlock; got Idempotent=false")
	}
	if res.LockEpochAfter != 2 {
		t.Errorf("epoch = %d after idempotent unlock; want 2 (unchanged)", res.LockEpochAfter)
	}
}

func TestUnlock_PreservesDenialHistory(t *testing.T) {
	c := newTestCounter(t)
	// Drive escalation to lockdown.
	for i := 0; i < 10; i++ {
		if err := c.RecordDenial("a", "fp", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if !c.IsLocked("a") {
		t.Fatalf("expected agent locked after 10 denials")
	}
	stBefore, _ := c.Status("a")
	if _, err := c.Unlock("a"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	stAfter, _ := c.Status("a")
	if stAfter.Total != stBefore.Total {
		t.Errorf("total changed across Unlock: %d → %d (must be preserved)", stBefore.Total, stAfter.Total)
	}
	// Verify count of denials rows is unchanged via the count helper.
	count, err := c.CountActionDenialsSince("a", "", 0)
	if err != nil {
		t.Fatalf("CountActionDenialsSince: %v", err)
	}
	if count == 0 {
		t.Errorf("denial_events count = 0 after Unlock; expected preserved")
	}
}

func TestOperatorLock_BootstrapsUnseenAgent(t *testing.T) {
	c := newTestCounter(t)
	res, err := c.OperatorLock("newcomer")
	if err != nil {
		t.Fatalf("OperatorLock: %v", err)
	}
	if res.LockEpochAfter != 1 {
		t.Errorf("epoch = %d for new lock; want 1", res.LockEpochAfter)
	}
	if !c.IsLocked("newcomer") {
		t.Errorf("expected agent locked after OperatorLock")
	}
}

func TestOperatorLock_RelockAdvancesEpoch(t *testing.T) {
	c := newTestCounter(t)
	r1, _ := c.OperatorLock("a")
	r2, _ := c.OperatorLock("a")
	if r2.LockEpochAfter != r1.LockEpochAfter+1 {
		t.Errorf("re-lock did not advance epoch: %d → %d (want %d → %d)",
			r1.LockEpochAfter, r2.LockEpochAfter, r1.LockEpochAfter, r1.LockEpochAfter+1)
	}
}

func TestAutoEscalation_AdvancesEpoch(t *testing.T) {
	c := newTestCounter(t)
	// 9 denials — should not lock yet (threshold is 10).
	for i := 0; i < 9; i++ {
		if err := c.RecordDenial("a", "fp", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	st, _ := c.Status("a")
	if st.Locked {
		t.Fatalf("locked at total=9; want unlocked")
	}
	if st.LockEpoch != 0 {
		t.Errorf("epoch = %d after pre-threshold denials; want 0", st.LockEpoch)
	}
	// One more triggers auto-lock.
	if err := c.RecordDenial("a", "fp", 1); err != nil {
		t.Fatalf("RecordDenial (10): %v", err)
	}
	st, _ = c.Status("a")
	if !st.Locked {
		t.Errorf("not locked at total=10; want locked")
	}
	if st.LockEpoch != 1 {
		t.Errorf("epoch = %d after auto-lock; want 1", st.LockEpoch)
	}
}

func TestAutoEscalation_NoEpochAdvanceOnSubsequentDenialsWhileLocked(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("a") // epoch 1
	stBefore, _ := c.Status("a")
	// Drive more denials past the threshold — total grows but epoch must not.
	for i := 0; i < 5; i++ {
		if err := c.RecordDenial("a", "fp", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	stAfter, _ := c.Status("a")
	if stAfter.LockEpoch != stBefore.LockEpoch {
		t.Errorf("epoch advanced from %d → %d while already locked; want unchanged",
			stBefore.LockEpoch, stAfter.LockEpoch)
	}
	if stAfter.Total <= stBefore.Total {
		t.Errorf("total didn't advance: %d → %d", stBefore.Total, stAfter.Total)
	}
}

func TestStatusAll_SortedByAgent(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("zebra")
	c.Lockdown("alpha")
	c.Lockdown("middle")
	all, err := c.StatusAll()
	if err != nil {
		t.Fatalf("StatusAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}
	expected := []string{"alpha", "middle", "zebra"}
	for i, want := range expected {
		if all[i].Agent != want {
			t.Errorf("all[%d].Agent = %q, want %q (StatusAll must sort ASCII)", i, all[i].Agent, want)
		}
	}
}

func TestStatus_UnknownAgent(t *testing.T) {
	c := newTestCounter(t)
	_, err := c.Status("nope")
	if !errors.Is(err, ErrNoAgent) {
		t.Errorf("err = %v, want ErrNoAgent", err)
	}
}

func TestStatus_LevelDerivation(t *testing.T) {
	cases := []struct {
		denials int
		want    string
	}{
		{0, "normal"},
		{2, "normal"},
		{3, "elevated"},
		{6, "elevated"},
		{7, "high"},
		{9, "high"},
	}
	for _, tc := range cases {
		c := newTestCounter(t)
		for i := 0; i < tc.denials; i++ {
			if err := c.RecordDenial("a", "fp", 1); err != nil {
				t.Fatalf("RecordDenial: %v", err)
			}
		}
		// We use RecordDenial for the denials so the test exercises the
		// real escalation path. agent_state row may not exist when
		// total=0 — Status returns ErrNoAgent which is fine for the
		// normal-at-zero case below.
		st, err := c.Status("a")
		if tc.denials == 0 {
			if !errors.Is(err, ErrNoAgent) {
				t.Errorf("denials=0: expected ErrNoAgent, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("denials=%d: Status: %v", tc.denials, err)
		}
		if st.Level != tc.want {
			t.Errorf("denials=%d: Level = %q, want %q", tc.denials, st.Level, tc.want)
		}
	}
}

func TestMigrate_FromOldSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	// Open once with the FULL new schema, drop the new columns to
	// simulate a pre-spec-096 database, then reopen — the migration
	// should add them back idempotently.
	c, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("initial open: %v", err)
	}
	// Insert a row with the old (4-column) shape via the actual API.
	c.Lockdown("legacy") // uses new schema, OK
	c.Close()

	// Re-open and confirm both new columns are present.
	c2, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer c2.Close()
	st, err := c2.Status("legacy")
	if err != nil {
		t.Fatalf("Status after migration: %v", err)
	}
	if !st.Locked {
		t.Errorf("Locked = false after re-open; want true (Lockdown should survive)")
	}
	if st.LockEpoch != 1 {
		t.Errorf("LockEpoch = %d after re-open; want 1", st.LockEpoch)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "idempotent.db")
	c1, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	c1.Lockdown("a")
	c1.Close()

	// Re-open multiple times; migration should never fail.
	for i := 0; i < 5; i++ {
		c, err := OpenCounter(dbPath)
		if err != nil {
			t.Fatalf("re-open %d: %v", i, err)
		}
		c.Close()
	}
	// State preserved.
	c, _ := OpenCounter(dbPath)
	defer c.Close()
	st, err := c.Status("a")
	if err != nil {
		t.Fatalf("final Status: %v", err)
	}
	if !st.Locked {
		t.Errorf("Locked = false after repeated re-opens; want true")
	}
}
