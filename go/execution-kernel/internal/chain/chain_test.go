package chain

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := idx.Upsert("chainA", 0, "hash0"); err != nil {
		t.Fatal(err)
	}
	info, err := idx.Get("chainA")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.LastSeq != 0 || info.LastHash != "hash0" {
		t.Errorf("unexpected info: %+v", info)
	}

	if err := idx.Upsert("chainA", 1, "hash1"); err != nil {
		t.Fatal(err)
	}
	info, _ = idx.Get("chainA")
	if info.LastSeq != 1 || info.LastHash != "hash1" {
		t.Errorf("expected last_seq=1 last_hash=hash1, got %+v", info)
	}
}

func TestIndex_TwoChainsIndependent(t *testing.T) {
	idx := newTempIndex(t)
	idx.Upsert("A", 0, "ha")
	idx.Upsert("B", 0, "hb")
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

// TestRebuild_RestoresEmptyIndex: 3 events for chain A (seqs 0,1,2) and 2 for
// chain B (seqs 0,1) written to JSONL; fresh index; after RebuildFromJSONL the
// index must reflect the highest seq and its hash for each chain.
func TestRebuild_RestoresEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":0,"this_hash":"ha0"}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1"}`,
		`{"chain_id":"A","seq":2,"this_hash":"ha2"}`,
		`{"chain_id":"B","seq":0,"this_hash":"hb0"}`,
		`{"chain_id":"B","seq":1,"this_hash":"hb1"}`,
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
		`{"chain_id":"A","seq":0,"this_hash":"ha0"}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1"}`,
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

// TestRebuild_TakesMaxSeqWithinChain: out-of-order lines; index must reflect
// the line with the highest seq, regardless of file order.
func TestRebuild_TakesMaxSeqWithinChain(t *testing.T) {
	dir := t.TempDir()
	writeJSONLFile(t, dir, "run1", []string{
		`{"chain_id":"A","seq":2,"this_hash":"ha2"}`,
		`{"chain_id":"A","seq":0,"this_hash":"ha0"}`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1"}`,
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
		`{"chain_id":"A","seq":0,"this_hash":"ha0"}`,
		`THIS IS NOT JSON {{{`,
		`{"chain_id":"A","seq":1,"this_hash":"ha1"}`,
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
	// Index should have no rows for any chain.
	info, err := idx.Get("any-chain")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("expected nil info for empty index, got %+v", info)
	}
}
