package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/internal/blob"
)

func TestCardDeclaresClaudeCodeContract(t *testing.T) {
	d := New()
	card := d.Card()

	if d.ID() != "claudecode" {
		t.Fatalf("ID() = %q, want claudecode", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.Tier != driver.TierFrontier {
		t.Fatalf("tier = %s, want frontier", card.Tier)
	}
	// Spec 105 FR-002: claudecode declares CapTestAuthor (test-authoring
	// is in scope for a frontier code model).
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapCodeReview, driver.CapSpecAuthor, driver.CapDocsWrite, driver.CapTestAuthor} {
		if !card.HasCapability(cap) {
			t.Fatalf("card missing capability %q", cap)
		}
	}
	if !card.Constraints.WorktreeRequired || !card.Constraints.NetworkRequired || !card.Constraints.QuotaBounded {
		t.Fatalf("constraints = %+v, want governed worktree, network, quota-bounded", card.Constraints)
	}
}

func TestReadyReportsUnavailableRuntime(t *testing.T) {
	d := New(WithCommand("definitely-not-a-real-claude-code-binary"))

	ready, reason := d.Ready(context.Background())
	if ready {
		t.Fatal("Ready() = true for a missing runtime, want false")
	}
	if !strings.Contains(reason, "not found") {
		t.Fatalf("Ready() reason = %q, want it to explain the runtime was not found", reason)
	}
}

// TestInvoke_PassesSkipPermissions verifies the driver passes
// --dangerously-skip-permissions to claude. The dispatch-mode sandbox
// would otherwise block worktree writes (2026-05-24 dogfood bug #5).
// claude help describes the flag: "Bypass all permission checks.
// Recommended only for sandboxes with no internet access."
func TestInvoke_PassesSkipPermissions(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	argvPath := filepath.Join(dir, "argv.log")
	// Fake claude binary: dump argv to a file, exit 0 so the driver
	// treats it as a successful invocation.
	script := "#!/usr/bin/env bash\n" +
		"for a in \"$@\"; do echo \"$a\" >> " + argvPath + "; done\n" +
		"exit 0\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	d := New(WithCommand(binPath))
	wu := driver.WorkUnit{
		ID:           "test-wu-001",
		WorktreePath: dir,
	}
	_, err := d.Invoke(context.Background(), wu)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read captured argv: %v", err)
	}
	if !strings.Contains(string(argv), "--dangerously-skip-permissions") {
		t.Errorf("argv missing --dangerously-skip-permissions\nargv=%q", string(argv))
	}
}

func TestInvokeExternalizesLargeOutput(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	script := "#!/usr/bin/env bash\n" +
		"python3 - <<'PY'\n" +
		"import sys\n" +
		"sys.stdout.write('x' * 2621440)\n" +
		"PY\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	store := blob.NewFSStore(blob.WithDir(filepath.Join(dir, "blobs")), blob.WithEmitter(nil))
	d := New(WithCommand(binPath), WithBlobStore(store))
	res, err := d.Invoke(context.Background(), driver.WorkUnit{ID: "large", WorktreePath: dir})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !blob.IsRef(res.OutputRef) {
		t.Fatalf("OutputRef = %q, want blob ref", res.OutputRef)
	}
	body, err := blob.Resolve(context.Background(), store, res.OutputRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(body) != 2621440 || string(body[:1]) != "x" || string(body[len(body)-1:]) != "x" {
		t.Fatalf("resolved body length/content mismatch: len=%d", len(body))
	}
}

func TestInvokeKeepsSmallOutputInline(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "claude")
	script := "#!/usr/bin/env bash\n" +
		"python3 - <<'PY'\n" +
		"import sys\n" +
		"sys.stdout.write('s' * 4096)\n" +
		"PY\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	blobDir := filepath.Join(dir, "blobs")
	store := blob.NewFSStore(blob.WithDir(blobDir), blob.WithEmitter(nil))
	d := New(WithCommand(binPath), WithBlobStore(store))
	res, err := d.Invoke(context.Background(), driver.WorkUnit{ID: "small", WorktreePath: dir})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if blob.IsRef(res.OutputRef) {
		t.Fatalf("OutputRef = %q, want inline", res.OutputRef)
	}
	if len(res.OutputRef) != 4096 {
		t.Fatalf("OutputRef length = %d, want 4096", len(res.OutputRef))
	}
	if _, err := os.Stat(blobDir); !os.IsNotExist(err) {
		t.Fatalf("blob dir stat err = %v, want not exists", err)
	}
}
