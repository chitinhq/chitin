package chain

import (
	"testing"
)

func TestBeginEmit_NewChain(t *testing.T) {
	idx := newTempIndex(t)
	tx, err := idx.BeginEmit("chain-new")
	if err != nil {
		t.Fatalf("BeginEmit error: %v", err)
	}
	if tx.Current != nil {
		t.Errorf("expected nil Current for new chain, got %+v", tx.Current)
	}
	if err := tx.Commit(1, "hash1", "decision", "lhash1", "2026-05-09T00:00:00Z"); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	// Verify upsert happened
	info, err := idx.Get("chain-new")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected non-nil info after commit")
	}
	if info.LastSeq != 1 || info.LastHash != "hash1" {
		t.Errorf("expected seq=1 hash=hash1, got %+v", info)
	}
	if info.LastEventType != "decision" || info.LastLogicalHash != "lhash1" {
		t.Errorf("expected eventType=decision logicalHash=lhash1, got eventType=%s logicalHash=%s",
			info.LastEventType, info.LastLogicalHash)
	}
}

func TestBeginEmit_ExistingChain(t *testing.T) {
	idx := newTempIndex(t)
	// Seed with upsert
	idx.Upsert("chain-exist", 5, "oldhash", "action", "oldlhash", "2026-01-01T00:00:00Z")

	tx, err := idx.BeginEmit("chain-exist")
	if err != nil {
		t.Fatalf("BeginEmit error: %v", err)
	}
	if tx.Current == nil {
		t.Fatal("expected non-nil Current for existing chain")
	}
	if tx.Current.LastSeq != 5 || tx.Current.LastHash != "oldhash" {
		t.Errorf("unexpected current state: %+v", tx.Current)
	}
	if err := tx.Commit(6, "newhash", "action", "newlhash", "2026-05-09T00:00:00Z"); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	info, _ := idx.Get("chain-exist")
	if info.LastSeq != 6 || info.LastHash != "newhash" {
		t.Errorf("expected seq=6 hash=newhash, got %+v", info)
	}
}

func TestBeginEmit_Rollback(t *testing.T) {
	idx := newTempIndex(t)
	idx.Upsert("chain-rollback", 3, "rhash", "", "", "")

	tx, err := idx.BeginEmit("chain-rollback")
	if err != nil {
		t.Fatalf("BeginEmit error: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	// State should be unchanged
	info, _ := idx.Get("chain-rollback")
	if info.LastSeq != 3 || info.LastHash != "rhash" {
		t.Errorf("rollback should leave state unchanged, got %+v", info)
	}
}

func TestBeginEmit_CommitIdempotent(t *testing.T) {
	idx := newTempIndex(t)
	tx, err := idx.BeginEmit("chain-idem")
	if err != nil {
		t.Fatalf("BeginEmit error: %v", err)
	}
	if err := tx.Commit(1, "h1", "decision", "lh1", "2026-05-09T00:00:00Z"); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	// Second call should be no-op
	if err := tx.Commit(2, "h2", "decision", "lh2", "2026-05-09T00:01:00Z"); err != nil {
		t.Errorf("second Commit should be no-op, got error: %v", err)
	}
	// State should still reflect the first commit
	info, _ := idx.Get("chain-idem")
	if info.LastSeq != 1 {
		t.Errorf("expected seq=1 after first commit, got %d", info.LastSeq)
	}
}

func TestBeginEmit_RollbackIdempotent(t *testing.T) {
	idx := newTempIndex(t)
	tx, err := idx.BeginEmit("chain-ridem")
	if err != nil {
		t.Fatalf("BeginEmit error: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	// Second call should be no-op
	if err := tx.Rollback(); err != nil {
		t.Errorf("second Rollback should be no-op, got error: %v", err)
	}
}

func TestOpenIndex_InvalidPath(t *testing.T) {
	_, err := OpenIndex("/dev/null/impossible/path/db.sqlite")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}