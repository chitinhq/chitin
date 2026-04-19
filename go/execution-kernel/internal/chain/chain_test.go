package chain

import (
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
