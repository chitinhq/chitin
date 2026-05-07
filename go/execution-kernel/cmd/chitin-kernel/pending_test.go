package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestPendingList_OrdersOldestFirst(t *testing.T) {
	dir := t.TempDir()
	store, _ := gov.OpenEscalateStore(filepath.Join(dir, "p.sqlite"))
	defer store.Close()

	mk := func(id string, ts int64) {
		_ = store.InsertPending(gov.PendingApproval{
			ID: id, Agent: "a", RuleID: "r", ActionType: "shell.exec",
			ActionTarget: "x", Cwd: "/tmp", Reason: "r",
			Channel: "cli-only", TimeoutSeconds: 600,
			RememberWindowSeconds: 0, CreatedTs: ts,
		})
	}
	mk("01C", 1700000300)
	mk("01A", 1700000100)
	mk("01B", 1700000200)

	var buf bytes.Buffer
	if err := pendingList(store, &buf, true /* json */); err != nil {
		t.Fatalf("pendingList: %v", err)
	}

	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if len(out) != 3 {
		t.Fatalf("got %d rows, want 3", len(out))
	}
	want := []string{"01A", "01B", "01C"}
	for i, w := range want {
		if out[i]["id"] != w {
			t.Errorf("row %d id = %v, want %s", i, out[i]["id"], w)
		}
	}
}
