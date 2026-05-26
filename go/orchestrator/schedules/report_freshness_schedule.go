package schedules

import (
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/internal/reportfreshness"
)

func reportFreshnessSpec() JobSpec {
	cfg, err := reportfreshness.LoadConfigOrDefault("")
	cadence := reportfreshness.DefaultCadenceMinutes
	if err == nil && cfg.CadenceMinutes > 0 {
		cadence = cfg.CadenceMinutes
	}
	return JobSpec{
		Name:          "report-freshness-canary",
		Command:       "/bin/true",
		Args:          nil,
		Interval:      cadenceInterval(cadence),
		TimeZone:      "",
		Description:   "Report freshness canary — detect stale dashboards before operators read them",
		ActivityName:  "CheckReportFreshness",
		ActivityInput: map[string]any{"cadence": "scheduled"},
	}
}

// cadenceInterval converts the configured cadence (minutes) into a Temporal
// ScheduleIntervalSpec duration. Minutes are used instead of a cron expression
// because minute-granular cadences (e.g. 360m == every 6 hours) do not all map
// cleanly onto a standard 5-field cron — the Interval form expresses them
// directly.
func cadenceInterval(minutes int) time.Duration {
	if minutes <= 0 {
		minutes = reportfreshness.DefaultCadenceMinutes
	}
	return time.Duration(minutes) * time.Minute
}
