package schedules

import (
	"fmt"

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
		Cron:          cadenceCron(cadence),
		TimeZone:      "",
		Description:   "Report freshness canary — detect stale dashboards before operators read them",
		ActivityName:  "CheckReportFreshness",
		ActivityInput: map[string]any{"cadence": "scheduled"},
	}
}

func cadenceCron(minutes int) string {
	if minutes <= 0 {
		minutes = reportfreshness.DefaultCadenceMinutes
	}
	return fmt.Sprintf("@every %dm", minutes)
}
