package gov

import (
	"path/filepath"
	"testing"
)

func newTestCounter(t *testing.T) *Counter {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	c, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCounter_LadderNormal(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 2; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if lv := c.Level("agent1"); lv != "normal" {
		t.Errorf("after 2 denials, level=%q want normal", lv)
	}
}

func TestCounter_LadderElevated(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 3; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if lv := c.Level("agent1"); lv != "elevated" {
		t.Errorf("after 3 denials, level=%q want elevated", lv)
	}
}

func TestCounter_LadderHigh(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 7; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if lv := c.Level("agent1"); lv != "high" {
		t.Errorf("after 7 denials, level=%q want high", lv)
	}
}

func TestCounter_Lockdown(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if !c.IsLocked("agent1") {
		t.Errorf("10 denials should trigger lockdown")
	}
	if lv := c.Level("agent1"); lv != "lockdown" {
		t.Errorf("level: got %q want lockdown", lv)
	}
}

func TestCounter_WeightedDenial(t *testing.T) {
	c := newTestCounter(t)
	// Self-modification rule has weight=2. Three such denials = count 6 → elevated.
	if err := c.RecordDenial("agent1", "fp-self-mod", 2); err != nil {
		t.Fatalf("RecordDenial: %v", err)
	}
	if err := c.RecordDenial("agent1", "fp-self-mod", 2); err != nil {
		t.Fatalf("RecordDenial: %v", err)
	}
	if lv := c.Level("agent1"); lv != "elevated" {
		t.Errorf("after 2 weighted-2 denials (count=4), level=%q want elevated", lv)
	}
}

func TestCounter_PerAgentIsolation(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	if c.IsLocked("agent2") {
		t.Errorf("agent2 should not be locked when only agent1 has denials")
	}
}

func TestCounter_DenialsKeyedByAgentAndFingerprint(t *testing.T) {
	c := newTestCounter(t)
	if err := c.RecordDenial("agent1", "fp-alpha", 1); err != nil {
		t.Fatalf("RecordDenial alpha #1: %v", err)
	}
	if err := c.RecordDenial("agent1", "fp-alpha", 1); err != nil {
		t.Fatalf("RecordDenial alpha #2: %v", err)
	}
	if err := c.RecordDenial("agent1", "fp-beta", 1); err != nil {
		t.Fatalf("RecordDenial beta: %v", err)
	}
	if err := c.RecordDenial("agent2", "fp-alpha", 1); err != nil {
		t.Fatalf("RecordDenial agent2 alpha: %v", err)
	}

	rows, err := c.db.Query(`SELECT agent, action_fp, count FROM denials`)
	if err != nil {
		t.Fatalf("query denials: %v", err)
	}
	defer rows.Close()

	got := map[string]int{}
	for rows.Next() {
		var agent, fp string
		var count int
		if err := rows.Scan(&agent, &fp, &count); err != nil {
			t.Fatalf("scan denials: %v", err)
		}
		got[agent+"/"+fp] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate denials: %v", err)
	}

	want := map[string]int{
		"agent1/fp-alpha": 2,
		"agent1/fp-beta":  1,
		"agent2/fp-alpha": 1,
	}
	if len(got) != len(want) {
		t.Fatalf("denial rows=%v want=%v", got, want)
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("denial %s count=%d want %d (all rows=%v)", key, got[key], count, got)
		}
	}
}

func TestCounter_Reset(t *testing.T) {
	c := newTestCounter(t)
	for i := 0; i < 10; i++ {
		if err := c.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	c.Reset("agent1")
	if c.IsLocked("agent1") {
		t.Errorf("Reset should unlock")
	}
	if lv := c.Level("agent1"); lv != "normal" {
		t.Errorf("after Reset, level=%q want normal", lv)
	}
}

func TestCounter_ManualLockdown(t *testing.T) {
	c := newTestCounter(t)
	c.Lockdown("agent1")
	if !c.IsLocked("agent1") {
		t.Errorf("Lockdown should immediately lock")
	}
}

func TestCounter_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	c1, _ := OpenCounter(dbPath)
	for i := 0; i < 10; i++ {
		if err := c1.RecordDenial("agent1", "fp1", 1); err != nil {
			t.Fatalf("RecordDenial: %v", err)
		}
	}
	c1.Close()

	c2, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer c2.Close()
	if !c2.IsLocked("agent1") {
		t.Errorf("lockdown should persist across Close/Open")
	}
}

func TestGate_LockdownPersistsAcrossGateInstances(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	p := Policy{
		Mode: "enforce",
		Rules: []Rule{
			{ID: "deny-shell", Action: ActionMatcher{string(ActShellExec)}, Effect: "deny", Reason: "blocked"},
			{ID: "allow-read", Action: ActionMatcher{string(ActFileRead)}, Effect: "allow"},
		},
	}
	if err := p.ApplyDefaults(); err != nil {
		t.Fatalf("ApplyDefaults: %v", err)
	}

	c1, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter c1: %v", err)
	}
	g1 := &Gate{Policy: p, Counter: c1, LogDir: filepath.Join(dir, "decisions-1"), Cwd: dir}
	for i := 0; i < 10; i++ {
		d := g1.Evaluate(Action{Type: ActShellExec, Target: "rm -rf tmp"}, "agent1", nil)
		if d.Allowed {
			t.Fatalf("iter %d: deny-shell unexpectedly allowed", i)
		}
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("close c1: %v", err)
	}

	c2, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter c2: %v", err)
	}
	defer c2.Close()
	g2 := &Gate{Policy: p, Counter: c2, LogDir: filepath.Join(dir, "decisions-2"), Cwd: dir}
	d := g2.Evaluate(Action{Type: ActFileRead, Target: "README.md"}, "agent1", nil)
	if d.Allowed {
		t.Fatalf("new gate instance must preserve lockdown, got allow: %+v", d)
	}
	if d.RuleID != "lockdown" {
		t.Fatalf("RuleID=%q want lockdown", d.RuleID)
	}
}

// TestCounter_RecordDenial_ErrorOnClosedDB pins the contract that
// RecordDenial surfaces SQLite failures rather than swallowing them.
// Pre-fix the function returned no error and `_, _ =` discarded every
// Exec result, so a broken DB silently dropped the escalation count and
// the agent never reached lockdown. Closing the DB before the call is
// the simplest reliable injection of a transactional failure.
func TestCounter_RecordDenial_ErrorOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gov.db")
	c, err := OpenCounter(dbPath)
	if err != nil {
		t.Fatalf("OpenCounter: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.RecordDenial("agent1", "fp1", 1); err == nil {
		t.Fatal("expected RecordDenial to surface an error after DB Close, got nil")
	}
}
