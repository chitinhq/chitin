package schedules

import (
	"errors"
	"path/filepath"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/temporal"
)

// Spec 081 tests for the Schedule-backed cron migration pattern. With US2
// T009–T012 and US3 T015–T019 landed, Registry() holds the seven periodic
// read-mostly jobs plus the five watchdog / mutation / ops jobs. Later specs
// (e.g. spec 085) append further jobs to the Registry, so the US3 set is a
// verified subset, not the total.

// expectedJobs is the migrated Registry: every JobSpec landed by spec 081's
// US2 + the landed US3 cron migrations, with the cadence of its retired timer
// or cron source of truth (spec 081 FR-005 — same cadence as the retired
// runner).
//
//	swarm-audit                OnCalendar=*-*-* 08:00:00      → "0 8 * * *"
//	architecture-audit         OnCalendar=Sun *-*-* 06:00:00  → "0 6 * * 0"
//	argus-ingest-beliefs       OnUnitActiveSec=30min          → "*/30 * * * *"
//	argus-ingest-git           OnUnitActiveSec=10min          → "*/10 * * * *"
//	argus-ingest-logs          OnUnitActiveSec=2min           → "*/2 * * * *"
//	chitin-codex-chain-ingest  OnUnitActiveSec=1h             → "0 * * * *"
//	chitin-codex-usage-feed    OnUnitActiveSec=10min          → "*/10 * * * *"
//	chitin-chain-watch         OnUnitActiveSec=1min           → "* * * * *"
//	chitin-agent-unlock        OnUnitActiveSec=15min          → "*/15 * * * *"
//	chitin-envelope-rotate     OnUnitActiveSec=5min           → "*/5 * * * *"
//	chitin-kernel-redeploy     OnUnitActiveSec=15min          → "*/15 * * * *"
//	openclaw-gateway-restart   spec 036 hourly cron           → "0 * * * *"
var expectedJobs = []struct {
	name string
	cron string
	tz   string
}{
	{"swarm-audit", "0 8 * * *", "America/Detroit"},
	{"architecture-audit", "0 6 * * 0", "America/Detroit"},
	{"argus-ingest-beliefs", "*/30 * * * *", ""},
	{"argus-ingest-git", "*/10 * * * *", ""},
	{"argus-ingest-logs", "*/2 * * * *", ""},
	{"chitin-codex-chain-ingest", "0 * * * *", ""},
	{"chitin-codex-usage-feed", "*/10 * * * *", ""},
	{"chitin-chain-watch", "* * * * *", ""},
	{"chitin-agent-unlock", "*/15 * * * *", ""},
	{"chitin-envelope-rotate", "*/5 * * * *", ""},
	{"chitin-kernel-redeploy", "*/15 * * * *", ""},
	{"openclaw-gateway-restart", "0 * * * *", ""},
}

// TestRegistry_HasMigratedJobs proves Registry() returns every JobSpec
// migrated by spec 081 US2 + the landed US3 cron migrations, with the cadence
// of its retired timer / cron source of truth.
func TestRegistry_HasMigratedJobs(t *testing.T) {
	reg := Registry()
	if len(reg) < len(expectedJobs) {
		t.Fatalf("Registry() returned %d jobs, want at least %d (spec 081 US2 + landed US3); got %+v",
			len(reg), len(expectedJobs), reg)
	}

	byName := make(map[string]JobSpec, len(reg))
	for _, job := range reg {
		if _, dup := byName[job.Name]; dup {
			t.Fatalf("Registry() has a duplicate job Name %q — Name MUST be unique", job.Name)
		}
		byName[job.Name] = job
	}

	for _, want := range expectedJobs {
		job, ok := byName[want.name]
		if !ok {
			t.Errorf("Registry() is missing job %q", want.name)
			continue
		}
		if job.Cron != want.cron {
			t.Errorf("job %q Cron = %q, want %q", want.name, job.Cron, want.cron)
		}
		if job.TimeZone != want.tz {
			t.Errorf("job %q TimeZone = %q, want %q", want.name, job.TimeZone, want.tz)
		}
		if job.Command == "" {
			t.Errorf("job %q Command is empty — the activity has no script to run", want.name)
		}
		if !filepath.IsAbs(job.Command) {
			t.Errorf("job %q Command = %q, want an absolute on-disk executable path", want.name, job.Command)
		}
		if job.Description == "" {
			t.Errorf("job %q Description is empty — the Schedule note would be blank", want.name)
		}
	}
}

// TestJobSpec_ScheduleIDIsStable proves ScheduleID is a pure function of the
// job Name — the property that makes EnsureSchedules idempotent: a second
// Create with the same ID collides and is a no-op, never a duplicate.
func TestJobSpec_ScheduleIDIsStable(t *testing.T) {
	job := JobSpec{Name: "swarm-audit"}
	want := "chitin-job-swarm-audit"
	if got := job.ScheduleID(); got != want {
		t.Errorf("ScheduleID() = %q, want %q", got, want)
	}
	// Pure function — same input, same output across calls.
	if job.ScheduleID() != job.ScheduleID() {
		t.Error("ScheduleID() is not deterministic")
	}
}

// TestRegistry_ReturnsFreshSlice proves Registry() never hands out a shared
// mutable global — a caller mutating its result must not corrupt the next
// caller's view of the canonical inventory.
func TestRegistry_ReturnsFreshSlice(t *testing.T) {
	first := Registry()
	first[0].Name = "mutated"
	second := Registry()
	if second[0].Name != "swarm-audit" {
		t.Errorf("Registry() returned a shared slice — mutation leaked; got %q", second[0].Name)
	}
}

// TestIsAlreadyExists proves the idempotency predicate: the Temporal SDK's
// ErrScheduleAlreadyRunning sentinel and a serviceerror.AlreadyExists / a
// *serviceerror.WorkflowExecutionAlreadyStarted are all recognized as the
// already-registered case, while an unrelated error is not — so EnsureSchedules
// treats a re-registration as success but still propagates a real fault.
func TestIsAlreadyExists(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"schedule-already-running sentinel", temporal.ErrScheduleAlreadyRunning, true},
		{"serviceerror already-exists", serviceerror.NewAlreadyExists("dup"), true},
		{"serviceerror workflow already started", serviceerror.NewWorkflowExecutionAlreadyStarted("dup", "id", "run"), true},
		{"unrelated error", errors.New("connection refused"), false},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAlreadyExists(tc.err); got != tc.want {
				t.Errorf("isAlreadyExists(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsAlreadyExists_WrappedSentinel proves a sentinel wrapped with fmt.Errorf
// %w is still recognized — errors.Is unwraps it. EnsureSchedules wraps Create
// errors, so this is the realistic wrapping path.
func TestIsAlreadyExists_WrappedSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("ensuring schedule"), temporal.ErrScheduleAlreadyRunning)
	if !isAlreadyExists(wrapped) {
		t.Error("a sentinel joined into a wrapped error must still be recognized as already-exists")
	}
}
