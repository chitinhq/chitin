package chain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTempIndex(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chain_index.sqlite")
	idx, err := OpenIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close(); os.Remove(path) })
	return idx
}

func TestIndex_NewChainReturnsZero(t *testing.T) {
	idx := newTempIndex(t)
	info, err := idx.Get("chainA")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil info for new chain, got %+v", info)
	}
}

func TestIndex_UpsertAndGet(t *testing.T) {
	idx := newTempIndex(t)
	if err := idx.Upsert("chainA", 0, "hash0", "", "", ""); err != nil {
		t.Fatal(err)
	}
	info, err := idx.Get("chainA")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.LastSeq != 0 || info.LastHash != "hash0" {
		t.Errorf("unexpected info: %+v", info)
	}

	if err := idx.Upsert("chainA", 1, "hash1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	info, _ = idx.Get("chainA")
	if info.LastSeq != 1 || info.LastHash != "hash1" {
		t.Errorf("expected last_seq=1 last_hash=hash1, got %+v", info)
	}
}

func TestIndex_TwoChainsIndependent(t *testing.T) {
	idx := newTempIndex(t)
	idx.Upsert("A", 0, "ha", "", "", "")
	idx.Upsert("B", 0, "hb", "", "", "")
	a, _ := idx.Get("A")
	b, _ := idx.Get("B")
	if a.LastHash != "ha" || b.LastHash != "hb" {
		t.Errorf("chains got crossed: %+v %+v", a, b)
	}
}

// writeJSONLFile writes lines (as raw JSON strings) to a file named
// events-<name>.jsonl inside dir.
func writeJSONLFile(t *testing.T, dir, name string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("events-%s.jsonl", name))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		fmt.Fprintln(f, l)
	}
}

// newTempIndexInDir creates an Index whose SQLite file lives inside dir.
func newTempIndexInDir(t *testing.T, dir string) *Index {
	t.Helper()
	path := filepath.Join(dir, "chain_index.sqlite")
	idx, err := OpenIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// TestRebuild_RestoresEmptyIndex: two well-linked chains reconcile cleanly.
func TestRebuild_RestoresEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"ha0"}`,
		`{"chain_id":"A","seq":2,"this_hash":"ha2","prev_hash":"ha1"}`,
		`{"chain_id":"B","seq":0,"this_hash":"hb0","prev_hash":null}`,
		`{"chain_id":"B","seq":1,"this_hash":"hb1","prev_hash":"hb0"}`,
	})

	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatal(err)
	}

	a, err := idx.Get("A")
	if err != nil || a == nil {
		t.Fatalf("expected chain A: err=%v info=%v", err, a)
	}
	if a.LastSeq != 2 || a.LastHash != "ha2" {
		t.Errorf("chain A: want seq=2 hash=ha2, got seq=%d hash=%s", a.LastSeq, a.LastHash)
	}

	b, err := idx.Get("B")
	if err != nil || b == nil {
		t.Fatalf("expected chain B: err=%v info=%v", err, b)
	}
	if b.LastSeq != 1 || b.LastHash != "hb1" {
		t.Errorf("chain B: want seq=1 hash=hb1, got seq=%d hash=%s", b.LastSeq, b.LastHash)
	}
}

// TestRebuild_Idempotent: calling RebuildFromJSONL twice must yield identical state.
func TestRebuild_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"ha0"}`,
	})

	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatal(err)
	}
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatal(err)
	}

	a, err := idx.Get("A")
	if err != nil || a == nil {
		t.Fatalf("expected chain A after double rebuild: %v %v", err, a)
	}
	if a.LastSeq != 1 || a.LastHash != "ha1" {
		t.Errorf("expected seq=1 hash=ha1, got seq=%d hash=%s", a.LastSeq, a.LastHash)
	}
}

// TestRebuild_TakesMaxSeqWithinChain: out-of-order lines reconcile to the tail.
func TestRebuild_TakesMaxSeqWithinChain(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":2,"this_hash":"ha2","prev_hash":"ha1"}`,
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"ha0"}`,
	})

	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatal(err)
	}

	a, err := idx.Get("A")
	if err != nil || a == nil {
		t.Fatalf("expected chain A: %v %v", err, a)
	}
	if a.LastSeq != 2 || a.LastHash != "ha2" {
		t.Errorf("expected seq=2 hash=ha2, got seq=%d hash=%s", a.LastSeq, a.LastHash)
	}
}

// TestRebuild_TolerantOfMalformedLines: a bad line mixed in must not abort the
// reconcile; valid lines must still be processed.
func TestRebuild_TolerantOfMalformedLines(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`THIS IS NOT JSON {{{`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"ha0"}`,
	})

	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatal(err)
	}

	a, err := idx.Get("A")
	if err != nil || a == nil {
		t.Fatalf("expected chain A despite malformed line: %v %v", err, a)
	}
	if a.LastSeq != 1 || a.LastHash != "ha1" {
		t.Errorf("expected seq=1 hash=ha1, got seq=%d hash=%s", a.LastSeq, a.LastHash)
	}
}

// TestRebuild_NoJSONLFiles_IsNoOp: empty chitinDir must produce no error and
// leave the index empty.
func TestRebuild_NoJSONLFiles_IsNoOp(t *testing.T) {
	dir := t.TempDir()
	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}
	info, err := idx.Get("any-chain")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil info for empty index, got %+v", info)
	}
}

// TestRebuild_RejectsGap: a chain with a missing seq must be refused so
// subsequent emits don't fork onto a stale/forged head.
func TestRebuild_RejectsGap(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":2,"this_hash":"ha2","prev_hash":"ha1"}`,
	})
	idx := newTempIndexInDir(t, dir)
	err := idx.RebuildFromJSONL(dir)
	if err == nil {
		t.Fatalf("expected error on gap; got nil and index populated")
	}
	if !strings.Contains(err.Error(), "chain A") || !strings.Contains(err.Error(), "seq") {
		t.Errorf("expected error to identify chain and seq gap, got: %v", err)
	}
	if info, _ := idx.Get("A"); info != nil {
		t.Errorf("expected DB unchanged on rejected chain, got %+v", info)
	}
}

// TestRebuild_RejectsBadPrevHash: a chain whose prev_hash does not match the
// previous event's this_hash must be refused.
func TestRebuild_RejectsBadPrevHash(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"DIFFERENT"}`,
	})
	idx := newTempIndexInDir(t, dir)
	err := idx.RebuildFromJSONL(dir)
	if err == nil {
		t.Fatal("expected error on bad prev_hash linkage")
	}
	if !strings.Contains(err.Error(), "prev_hash") {
		t.Errorf("expected error to mention prev_hash, got: %v", err)
	}
}

// TestRebuild_RejectsNonNullPrevHashAtHead: seq 0 with a non-null prev_hash is
// forged (the head of a chain has no predecessor).
func TestRebuild_RejectsNonNullPrevHashAtHead(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":"fabricated"}`,
	})
	idx := newTempIndexInDir(t, dir)
	err := idx.RebuildFromJSONL(dir)
	if err == nil {
		t.Fatal("expected error on non-null prev_hash at seq 0")
	}
	if !strings.Contains(err.Error(), "seq 0") {
		t.Errorf("expected error to mention seq 0, got: %v", err)
	}
}

// TestRebuild_RejectsDuplicateSeqConflict: two lines claiming the same seq
// with different this_hash are a branching-attack signature.
func TestRebuild_RejectsDuplicateSeqConflict(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"real","prev_hash":"ha0"}`,
		`{"chain_id":"A","seq":1,"this_hash":"fake","prev_hash":"ha0"}`,
	})
	idx := newTempIndexInDir(t, dir)
	err := idx.RebuildFromJSONL(dir)
	if err == nil {
		t.Fatal("expected error on duplicate seq with different this_hash")
	}
	if !strings.Contains(err.Error(), "conflicting this_hash") {
		t.Errorf("expected error to mention conflicting this_hash, got: %v", err)
	}
}

// TestRebuild_AcceptsExactDuplicate: the same line appearing twice (same
// chain, seq, and this_hash) is a recoverable duplicate-emit scenario and
// must not fail rebuild.
func TestRebuild_AcceptsExactDuplicate(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":0,"this_hash":"ha0","prev_hash":null}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1","prev_hash":"ha0"}`,
	})
	idx := newTempIndexInDir(t, dir)
	if err := idx.RebuildFromJSONL(dir); err != nil {
		t.Fatalf("expected no error on exact-duplicate line, got: %v", err)
	}
	a, _ := idx.Get("A")
	if a == nil || a.LastSeq != 1 || a.LastHash != "ha1" {
		t.Errorf("expected seq=1 hash=ha1 after dup collapse, got %+v", a)
	}
}

// --- BeginEmit / Commit / Rollback tests ---

func TestBeginEmit_NewChainThenCommit(t *testing.T) {
	idx := newTempIndex(t)

	// BeginEmit on a brand-new chain should return Current=nil.
	tx, err := idx.BeginEmit("chain-new")
	if err != nil {
		t.Fatal(err)
	}
	if tx.Current != nil {
		t.Fatalf("expected nil Current for new chain, got %+v", tx.Current)
	}

	// Commit writes the chain tail.
	if err := tx.Commit(0, "hash0", "tool_call", "lhash0", "2026-05-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	info, err := idx.Get("chain-new")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.LastSeq != 0 || info.LastHash != "hash0" {
		t.Errorf("expected seq=0 hash=hash0, got %+v", info)
	}
	if info.LastEventType != "tool_call" {
		t.Errorf("expected LastEventType=tool_call, got %q", info.LastEventType)
	}
	if info.LastLogicalHash != "lhash0" {
		t.Errorf("expected LastLogicalHash=lhash0, got %q", info.LastLogicalHash)
	}
	if info.LastEmitTs != "2026-05-09T00:00:00Z" {
		t.Errorf("expected LastEmitTs=2026-05-09..., got %q", info.LastEmitTs)
	}
}

func TestBeginEmit_ExistingChainReturnsCurrent(t *testing.T) {
	idx := newTempIndex(t)
	idx.Upsert("chain-exist", 5, "hash5", "tool_call", "lhash5", "2026-05-09T01:00:00Z")

	tx, err := idx.BeginEmit("chain-exist")
	if err != nil {
		t.Fatal(err)
	}
	if tx.Current == nil {
		t.Fatal("expected non-nil Current for existing chain")
	}
	if tx.Current.LastSeq != 5 || tx.Current.LastHash != "hash5" {
		t.Errorf("expected seq=5 hash=hash5, got %+v", tx.Current)
	}
	if tx.Current.LastEventType != "tool_call" {
		t.Errorf("expected EventType=tool_call, got %q", tx.Current.LastEventType)
	}

	if err := tx.Commit(6, "hash6", "shell_run", "lhash6", "2026-05-09T02:00:00Z"); err != nil {
		t.Fatal(err)
	}

	info, _ := idx.Get("chain-exist")
	if info.LastSeq != 6 || info.LastHash != "hash6" {
		t.Errorf("expected seq=6 hash=hash6 after commit, got %+v", info)
	}
}

func TestBeginEmit_RollbackLeavesUnchanged(t *testing.T) {
	idx := newTempIndex(t)
	idx.Upsert("chain-rollback", 3, "orig3", "session_end", "lh3", "2026-05-09T03:00:00Z")

	tx, err := idx.BeginEmit("chain-rollback")
	if err != nil {
		t.Fatal(err)
	}

	// Rollback should leave the chain state unchanged.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	info, _ := idx.Get("chain-rollback")
	if info.LastSeq != 3 || info.LastHash != "orig3" {
		t.Errorf("rollback should leave state unchanged, got %+v", info)
	}
}

func TestBeginEmit_CommitAfterCommitIsNoop(t *testing.T) {
	idx := newTempIndex(t)

	tx, _ := idx.BeginEmit("chain-dbl")
	tx.Commit(0, "h0", "ev0", "lh0", "ts0")

	// Second Commit should be a no-op (tx.done=true).
	if err := tx.Commit(1, "h1", "ev1", "lh1", "ts1"); err != nil {
		t.Fatalf("second Commit should be no-op, got: %v", err)
	}

	info, _ := idx.Get("chain-dbl")
	if info.LastSeq != 0 || info.LastHash != "h0" {
		t.Errorf("double commit should keep first value, got %+v", info)
	}
}

func TestBeginEmit_RollbackAfterCommitIsNoop(t *testing.T) {
	idx := newTempIndex(t)

	tx, _ := idx.BeginEmit("chain-rba")
	tx.Commit(0, "h0", "ev0", "lh0", "ts0")

	// Rollback after commit should be a no-op.
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback after Commit should be no-op, got: %v", err)
	}

	info, _ := idx.Get("chain-rba")
	if info.LastSeq != 0 {
		t.Errorf("rollback-after-commit should not undo, got seq=%d", info.LastSeq)
	}
}

// --- LogicalHash tests ---

func TestLogicalHash_EmptyEventType(t *testing.T) {
	h := LogicalHash("", json.RawMessage(`{"a":1}`))
	if h != "" {
		t.Fatalf("empty event type should return empty hash, got %q", h)
	}
}

func TestLogicalHash_Deterministic(t *testing.T) {
	h1 := LogicalHash("tool_call", json.RawMessage(`{"tool":"Bash","cmd":"ls"}`))
	h2 := LogicalHash("tool_call", json.RawMessage(`{"tool":"Bash","cmd":"ls"}`))
	if h1 != h2 {
		t.Fatalf("identical inputs should produce same hash: %s vs %s", h1, h2)
	}
}

func TestLogicalHash_DifferentEventType(t *testing.T) {
	h1 := LogicalHash("tool_call", json.RawMessage(`{"a":1}`))
	h2 := LogicalHash("shell_run", json.RawMessage(`{"a":1}`))
	if h1 == h2 {
		t.Fatal("different event types should produce different hashes")
	}
}

func TestLogicalHash_DifferentPayload(t *testing.T) {
	h1 := LogicalHash("tool_call", json.RawMessage(`{"a":1}`))
	h2 := LogicalHash("tool_call", json.RawMessage(`{"a":2}`))
	if h1 == h2 {
		t.Fatal("different payloads should produce different hashes")
	}
}

func TestLogicalHash_EmptyPayload(t *testing.T) {
	h := LogicalHash("event", json.RawMessage(``))
	if h == "" {
		t.Fatal("non-empty event type with empty payload should still produce a hash")
	}
}
