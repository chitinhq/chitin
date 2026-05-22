package schedules

import (
	"errors"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/temporal"
)

// Spec 081 US2 tests for the Schedule-backed cron migration pattern. T009
// migrated swarm-audit; T010–T012 add the remaining six periodic crons —
// architecture-audit, the three Argus ingesters, and the two codex jobs — so
// the Registry holds at least these seven US2 JobSpecs, each on the cadence of
// its retired systemd timer. Later specs append further jobs to the Registry
// (e.g. spec 085's operator-heartbeat), so this set is a subset, not the total.

// us2Jobs is the expected US2 Registry: every periodic cron migrated by
// T009–T012, with the cron cadence the retired systemd timer fired on
// (spec 081 FR-005 — same cadence as the retired timer).
//
//	swarm-audit                OnCalendar=*-*-* 08:00:00      → "0 8 * * *"
//	architecture-audit         OnCalendar=Sun *-*-* 06:00:00  → "0 6 * * 0"
//	argus-ingest-beliefs       OnUnitActiveSec=30min          → "*/30 * * * *"
//	argus-ingest-git           OnUnitActiveSec=10min          → "*/10 * * * *"
//	argus-ingest-logs          OnUnitActiveSec=2min           → "*/2 * * * *"
//	chitin-codex-chain-ingest  OnUnitActiveSec=1h             → "0 * * * *"
//	chitin-codex-usage-feed    OnUnitActiveSec=10min          → "*/10 * * * *"
var us2Jobs = []struct {
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
}

// TestRegistry_HasUS2Jobs proves Registry() returns every periodic cron
// migrated by spec 081 US2 (T009–T012) with the cadence of its retired timer.
func TestRegistry_HasUS2Jobs(t *testing.T) {
	reg := Registry()
	if len(reg) < len(us2Jobs) {
		t.Fatalf("Registry() returned %d jobs, want at least %d (the US2 T009–T012 set); got %+v",
			len(reg), len(us2Jobs), reg)
	}

	byName := make(map[string]JobSpec, len(reg))
	for _, job := range reg {
		if _, dup := byName[job.Name]; dup {
			t.Fatalf("Registry() has a duplicate job Name %q — Name MUST be unique", job.Name)
		}
		byName[job.Name] = job
	}

	for _, want := range us2Jobs {
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
