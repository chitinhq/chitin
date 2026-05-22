// spec: 085-operator-report-delivery
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// deliverHarness drives swarm/bin/deliver-operator-report.sh with a stubbed
// `chitin-kernel` (composition) and `openclaw` (delivery) on PATH, so the
// script's compose → deliver → audit contract (C2) is exercised hermetically.
type deliverHarness struct {
	scriptPath string
	logPath    string
	stubDir    string
}

func newDeliverHarness(t *testing.T) deliverHarness {
	t.Helper()
	root := repoRootForInstallKernelTest(t) // sibling helper — repo root
	src := filepath.Join(root, "swarm", "bin", "deliver-operator-report.sh")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("delivery script not found: %v", err)
	}
	tmp := t.TempDir()
	stubDir := filepath.Join(tmp, "stubs")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatalf("mkdir stubs: %v", err)
	}
	return deliverHarness{
		scriptPath: src,
		logPath:    filepath.Join(tmp, "operator-report.jsonl"),
		stubDir:    stubDir,
	}
}

func (h deliverHarness) env() []string {
	return []string{
		"HOME=" + filepath.Dir(h.logPath),
		"PATH=" + h.stubDir + ":/usr/bin:/bin",
		"CHITIN_KERNEL_BIN=chitin-kernel",
		"CHITIN_OPERATOR_REPORT_LOG=" + h.logPath,
		"CHITIN_OPERATOR_DISCORD_TARGET=channel:test",
	}
}

func (h deliverHarness) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{h.scriptPath}, args...)...)
	cmd.Env = h.env()
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: non-exit error %v\n%s", args, err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

func (h deliverHarness) readLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(h.logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	return string(b)
}

// Compose succeeds and delivery succeeds → exit 0, one `delivered` audit row.
func TestDeliverReport_HappyPath(t *testing.T) {
	h := newDeliverHarness(t)
	writeExecutable(t, filepath.Join(h.stubDir, "chitin-kernel"), "#!/usr/bin/env bash\necho 'heartbeat body'\n")
	writeExecutable(t, filepath.Join(h.stubDir, "openclaw"), "#!/usr/bin/env bash\nexit 0\n")

	out, code := h.run(t, "heartbeat")
	if code != 0 {
		t.Fatalf("want exit 0, got %d\n%s", code, out)
	}
	log := h.readLog(t)
	if !strings.Contains(log, `"outcome":"delivered"`) || !strings.Contains(log, `"kind":"heartbeat"`) {
		t.Errorf("want a delivered heartbeat audit record, got: %s", log)
	}
}

// Delivery fails (openclaw non-zero) → exit 1, one `failed` audit row — the
// miss is never silent (FR-010).
func TestDeliverReport_DeliveryFailureExits1(t *testing.T) {
	h := newDeliverHarness(t)
	writeExecutable(t, filepath.Join(h.stubDir, "chitin-kernel"), "#!/usr/bin/env bash\necho 'heartbeat body'\n")
	writeExecutable(t, filepath.Join(h.stubDir, "openclaw"), "#!/usr/bin/env bash\nexit 1\n")

	out, code := h.run(t, "heartbeat")
	if code != 1 {
		t.Fatalf("want exit 1 on delivery failure, got %d\n%s", code, out)
	}
	if log := h.readLog(t); !strings.Contains(log, `"outcome":"failed"`) {
		t.Errorf("want a failed audit record, got: %s", log)
	}
}

// Compose fails → a fallback notice is still delivered (the operator learns
// the pipeline is broken); exit 0, but the audit records a failure.
func TestDeliverReport_ComposeFailureStillNotifies(t *testing.T) {
	h := newDeliverHarness(t)
	writeExecutable(t, filepath.Join(h.stubDir, "chitin-kernel"), "#!/usr/bin/env bash\nexit 1\n")
	writeExecutable(t, filepath.Join(h.stubDir, "openclaw"), "#!/usr/bin/env bash\nexit 0\n")

	out, code := h.run(t, "heartbeat")
	if code != 0 {
		t.Fatalf("want exit 0 (fallback notice delivered), got %d\n%s", code, out)
	}
	if log := h.readLog(t); !strings.Contains(log, `"outcome":"failed"`) {
		t.Errorf("compose failure must record a failed audit record, got: %s", log)
	}
}

// An unknown report kind is rejected before any compose/deliver.
func TestDeliverReport_UnknownKindRejected(t *testing.T) {
	h := newDeliverHarness(t)
	_, code := h.run(t, "bogus")
	if code != 2 {
		t.Errorf("want exit 2 for an unknown report kind, got %d", code)
	}
}
