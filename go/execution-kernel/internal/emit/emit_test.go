package emit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/chain"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

func newEnv(t *testing.T) (string, *chain.Index) {
	t.Helper()
	dir := t.TempDir()
	idx, err := chain.OpenIndex(filepath.Join(dir, "chain_index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return dir, idx
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			t.Fatalf("bad JSON: %v", err)
		}
		lines = append(lines, m)
	}
	return lines
}

func minimalSessionStart(chainID string, seq int64) *event.Event {
	return &event.Event{
		SchemaVersion:    "2",
		RunID:            "550e8400-e29b-41d4-a716-446655440000",
		SessionID:        "550e8400-e29b-41d4-a716-446655440001",
		Surface:          "claude-code",
		DriverIdentity:   event.DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "a" + repeat("0", 63)},
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655440002",
		AgentFingerprint: "b" + repeat("0", 63),
		EventType:        "session_start",
		ChainID:          chainID,
		ChainType:        "session",
		Seq:              seq, // ignored by Emit — recomputed
		Ts:               "2026-04-19T12:00:00.000Z",
		Labels:           map[string]string{},
		Payload:          json.RawMessage(`{"cwd":"/","client_info":{"name":"claude-code","version":"1"},"model":{"name":"x","provider":"y"},"system_prompt_hash":"` + repeat("0", 64) + `","tool_allowlist_hash":"` + repeat("0", 64) + `","agent_version":"1"}`),
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func TestEmit_FirstInChainHasZeroSeqAndNilPrev(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	ev := minimalSessionStart("chainA", 0)
	if err := e.Emit(ev); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, e.LogPath)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0]["seq"].(float64) != 0 {
		t.Errorf("expected seq=0, got %v", lines[0]["seq"])
	}
	if lines[0]["prev_hash"] != nil {
		t.Errorf("expected prev_hash=null, got %v", lines[0]["prev_hash"])
	}
	if lines[0]["this_hash"] == "" {
		t.Errorf("this_hash must be non-empty")
	}
}

func TestEmit_SecondInChainLinksToFirst(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	a := minimalSessionStart("chainA", 0)
	if err := e.Emit(a); err != nil {
		t.Fatal(err)
	}
	b := minimalSessionStart("chainA", 0) // seq ignored; emitter computes 1
	b.EventType = "user_prompt"
	b.Payload = json.RawMessage(`{"text":"hi"}`)
	if err := e.Emit(b); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, e.LogPath)
	if lines[1]["seq"].(float64) != 1 {
		t.Errorf("expected seq=1, got %v", lines[1]["seq"])
	}
	if lines[1]["prev_hash"] != lines[0]["this_hash"] {
		t.Errorf("prev_hash should equal previous this_hash: prev=%v prior_this=%v", lines[1]["prev_hash"], lines[0]["this_hash"])
	}
}

func TestEmit_TwoChainsAreIndependent(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}
	a := minimalSessionStart("chainA", 0)
	b := minimalSessionStart("chainB", 0)
	e.Emit(a)
	e.Emit(b)
	lines := readLines(t, e.LogPath)
	if lines[0]["seq"].(float64) != 0 || lines[1]["seq"].(float64) != 0 {
		t.Errorf("each new chain should start at seq=0; got %v, %v", lines[0]["seq"], lines[1]["seq"])
	}
}

// TestEmit_ConcurrentEmitsToSameChain_NoDuplicateSeq verifies that N goroutines
// emitting concurrently into the same chain_id produce exactly N lines with
// unique, contiguous seqs (0..N-1) and a valid prev_hash chain.
//
// Correctness argument (Dijkstra-style):
//   - Precondition: the index is empty; N goroutines each hold a distinct event.
//   - Each goroutine calls BeginEmit, which issues BEGIN IMMEDIATE. SQLite's
//     RESERVED lock ensures at most one such transaction is open at a time.
//     All others block at the kernel level until the holder commits or rolls back.
//   - Within the critical section, seq = last_seq + 1 is computed against the
//     committed state, JSONL is appended, and tx.Commit advances last_seq atomically.
//   - Postcondition: the set of seq values is exactly {0, 1, ..., N-1} and
//     for every k > 0, event[k].prev_hash == event[k-1].this_hash.
func TestEmit_ConcurrentEmitsToSameChain_NoDuplicateSeq(t *testing.T) {
	const N = 20
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := minimalSessionStart(fmt.Sprintf("chain-concurrent-%d", 0), 0)
			ev.ChainID = "chain-concurrent"
			ev.RunID = fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i)
			errs[i] = e.Emit(ev)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	lines := readLines(t, e.LogPath)
	if len(lines) != N {
		t.Fatalf("expected %d lines, got %d", N, len(lines))
	}

	// Build seq → line map.
	bySeq := make(map[int64]map[string]any, N)
	for _, l := range lines {
		seq := int64(l["seq"].(float64))
		if _, dup := bySeq[seq]; dup {
			t.Errorf("duplicate seq %d", seq)
		}
		bySeq[seq] = l
	}

	// Verify contiguous 0..N-1.
	seqs := make([]int64, 0, N)
	for s := range bySeq {
		seqs = append(seqs, s)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for k, s := range seqs {
		if s != int64(k) {
			t.Errorf("seqs not contiguous: position %d has seq %d", k, s)
		}
	}

	// Verify hash linkage.
	for k := int64(1); k < N; k++ {
		prev := bySeq[k-1]["this_hash"]
		cur := bySeq[k]["prev_hash"]
		if prev != cur {
			t.Errorf("seq %d: prev_hash=%v, but seq %d this_hash=%v", k, cur, k-1, prev)
		}
	}
	if bySeq[0]["prev_hash"] != nil {
		t.Errorf("seq=0 must have null prev_hash, got %v", bySeq[0]["prev_hash"])
	}
}

// TestEmit_ConcurrentEmitsToDifferentChains_NoCorruption emits to two chains
// concurrently and verifies that neither chain is corrupted. Note: SQLite
// BEGIN IMMEDIATE serializes all writers DB-wide, so the two chains will
// serialize on the lock — but correctness (no dup seq, valid hashes) must hold.
func TestEmit_ConcurrentEmitsToDifferentChains_NoCorruption(t *testing.T) {
	const N = 10
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	var wg sync.WaitGroup
	for _, chainID := range []string{"chainA", "chainB"} {
		for i := 0; i < N; i++ {
			wg.Add(1)
			go func(cid string, i int) {
				defer wg.Done()
				ev := minimalSessionStart(cid, 0)
				ev.RunID = fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", i)
				if err := e.Emit(ev); err != nil {
					t.Errorf("chain %s goroutine %d: %v", cid, i, err)
				}
			}(chainID, i)
		}
	}
	wg.Wait()

	lines := readLines(t, e.LogPath)
	// 2*N total lines
	if len(lines) != 2*N {
		t.Fatalf("expected %d lines, got %d", 2*N, len(lines))
	}

	// Partition by chain and verify each is contiguous.
	perChain := map[string][]map[string]any{}
	for _, l := range lines {
		cid := l["chain_id"].(string)
		perChain[cid] = append(perChain[cid], l)
	}
	for cid, cls := range perChain {
		if len(cls) != N {
			t.Errorf("chain %s: expected %d lines, got %d", cid, N, len(cls))
		}
		bySeq := make(map[int64]map[string]any, N)
		for _, l := range cls {
			seq := int64(l["seq"].(float64))
			if _, dup := bySeq[seq]; dup {
				t.Errorf("chain %s: duplicate seq %d", cid, seq)
			}
			bySeq[seq] = l
		}
		for k := int64(1); k < N; k++ {
			prev := bySeq[k-1]["this_hash"]
			cur := bySeq[k]["prev_hash"]
			if prev != cur {
				t.Errorf("chain %s seq %d: hash linkage broken", cid, k)
			}
		}
	}
}
