// integration_test.go — round-trip tests for spec 097 against a real
// Temporal server. Skipped automatically when $TEST_TEMPORAL_HOSTPORT
// is unset, so CI without a Temporal container is fast and unaffected.
//
// To run locally against a `temporal server start-dev`:
//
//   TEST_TEMPORAL_HOSTPORT=127.0.0.1:7233 go test -run Integration -v \
//     ./cmd/chitin-orchestrator/...
//
// These tests EXECUTE workflows against the configured Temporal cluster.
// Make sure no production `chitin-orchestrator` worker is polling the
// same task queue, or it may actually try to dispatch the fixture's
// nodes to real drivers. The default chitin TaskQueue is "chitin"; if
// a worker is polling that, stop it first or use a sandbox cluster.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
)

// liveTemporalGate skips a test when $TEST_TEMPORAL_HOSTPORT is unset.
// Sets the env var that dialTemporal reads so the subcommand handlers
// reach the test cluster, not the operator's default.
func liveTemporalGate(t *testing.T) string {
	t.Helper()
	host := os.Getenv("TEST_TEMPORAL_HOSTPORT")
	if host == "" {
		t.Skip("integration: skipped — set $TEST_TEMPORAL_HOSTPORT (e.g., 127.0.0.1:7233) to run against a live Temporal")
	}
	// Verify the cluster is actually reachable; a dead env var is a wasted run.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(client.Options{HostPort: host})
	if err != nil {
		t.Skipf("integration: Temporal at %s not reachable: %v", host, err)
	}
	c.Close()
	_ = ctx
	t.Setenv("TEMPORAL_HOSTPORT", host)
	return host
}

// integrationFixtureRepo points the subcommands at the repo this very
// test file lives in — its testdata/specs/097-fixture/ is the canonical
// 3-node DAG that resolves entirely to code.implement.
func integrationFixtureRepo(t *testing.T) string {
	t.Helper()
	// cwd during `go test` is the package directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Join(cwd, "testdata")
}

// silenceChainEmit installs a fake kernel binary that exits 0 silently,
// so the schedule/cancel subcommands don't print warnings about a
// missing chitin-kernel during integration runs.
func silenceChainEmit(t *testing.T) {
	t.Helper()
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
}

// extractRunID parses the schedule subcommand's stdout success line to
// pull out the run_id token. Format:
//   "scheduled spec X (N nodes, M capabilities required); run_id=<uuid>"
func extractRunID(t *testing.T, stdout string) string {
	t.Helper()
	const marker = "run_id="
	i := strings.Index(stdout, marker)
	if i == -1 {
		t.Fatalf("no run_id in stdout: %q", stdout)
	}
	id := strings.TrimSpace(stdout[i+len(marker):])
	if id == "" {
		t.Fatalf("empty run_id in stdout: %q", stdout)
	}
	return id
}

func TestIntegration_ScheduleRoundTrip(t *testing.T) {
	liveTemporalGate(t)
	silenceChainEmit(t)
	repo := integrationFixtureRepo(t)

	var stdout, stderr bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "097-fixture"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("runSchedule exit=%d stderr=%q", code, stderr.String())
	}
	runID := extractRunID(t, stdout.String())
	t.Logf("scheduled run_id=%s", runID)

	// Sanity: the success line should mention 3 nodes (the fixture's
	// task count) and 1 capability (code.implement, deduped).
	if !strings.Contains(stdout.String(), "3 nodes") {
		t.Errorf("expected '3 nodes' in stdout, got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "1 capabilities") {
		t.Errorf("expected '1 capabilities' in stdout, got: %q", stdout.String())
	}

	// Cleanup — cancel the run we just started so it doesn't sit
	// forever waiting for a worker. The cancel may exit 1 with
	// "already in terminal state" if the workflow finished before we
	// got here, which is fine.
	defer func() {
		var cancelOut, cancelErr bytes.Buffer
		_ = runCancel(context.Background(), []string{"-run-id", runID, "-reason", "integration cleanup"}, &cancelOut, &cancelErr)
		t.Logf("cleanup: cancel stderr=%q", cancelErr.String())
	}()
}

func TestIntegration_ScheduleStatusCancel(t *testing.T) {
	liveTemporalGate(t)
	silenceChainEmit(t)
	repo := integrationFixtureRepo(t)

	// 1. Schedule.
	var schedOut, schedErr bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "097-fixture"}, &schedOut, &schedErr)
	if code != exitSuccess {
		t.Fatalf("schedule exit=%d stderr=%q", code, schedErr.String())
	}
	runID := extractRunID(t, schedOut.String())

	// 2. Inspect via status -run-id.
	var statOut, statErr bytes.Buffer
	code = runStatus(context.Background(), []string{"-run-id", runID}, &statOut, &statErr)
	if code != exitSuccess {
		t.Errorf("status inspect exit=%d stderr=%q", code, statErr.String())
	}
	if !strings.Contains(statOut.String(), runID) {
		t.Errorf("status output didn't contain the run_id: %q", statOut.String())
	}

	// 3. List mode — at least one active run should be visible.
	var listOut, listErr bytes.Buffer
	code = runStatus(context.Background(), nil, &listOut, &listErr)
	if code != exitSuccess {
		t.Errorf("status list exit=%d stderr=%q", code, listErr.String())
	}
	if !strings.Contains(listOut.String(), runID) {
		t.Errorf("our just-scheduled run_id is missing from the list: %q", listOut.String())
	}

	// 4. Cancel.
	var canOut, canErr bytes.Buffer
	code = runCancel(context.Background(), []string{"-run-id", runID, "-reason", "spec 097 integration test"}, &canOut, &canErr)
	if code != exitSuccess {
		t.Errorf("cancel exit=%d stderr=%q", code, canErr.String())
	}
	if !strings.Contains(canOut.String(), "canceled") {
		t.Errorf("cancel stdout didn't confirm: %q", canOut.String())
	}

	// 5. Second cancel — idempotent, MUST exit 1 with terminal-state stderr.
	// (Allow a small wait for Temporal to fully process the cancel.)
	time.Sleep(2 * time.Second)
	var can2Out, can2Err bytes.Buffer
	code = runCancel(context.Background(), []string{"-run-id", runID}, &can2Out, &can2Err)
	if code != exitUserError {
		// It's possible the workflow is still in "Canceled" transition
		// rather than terminal yet — only enforce the idempotency
		// behavior if Temporal already marks it terminal. Otherwise
		// log and move on (the unit tests cover the deterministic
		// terminal-state path).
		t.Logf("idempotent cancel returned exit=%d (workflow may not be terminal yet); stderr=%q", code, can2Err.String())
	} else if !strings.Contains(can2Err.String(), "already in terminal state") {
		t.Errorf("idempotent cancel stderr missing terminal-state message: %q", can2Err.String())
	}
}
