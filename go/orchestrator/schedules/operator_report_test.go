// spec: 085-operator-report-delivery
package schedules

import (
	"strings"
	"testing"
)

// The operator-heartbeat job is registered, hourly, and runs the delivery
// script in heartbeat mode (spec 085 US1, contract C3).
func TestRegistry_HasOperatorHeartbeat(t *testing.T) {
	var spec *JobSpec
	for _, s := range Registry() {
		if s.Name == "operator-heartbeat" {
			sCopy := s
			spec = &sCopy
			break
		}
	}
	if spec == nil {
		t.Fatal("operator-heartbeat not found in Registry()")
	}
	if spec.Cron != "0 * * * *" {
		t.Errorf("operator-heartbeat cron = %q, want hourly \"0 * * * *\"", spec.Cron)
	}
	if !strings.HasSuffix(spec.Command, "deliver-operator-report.sh") {
		t.Errorf("operator-heartbeat command should be the delivery script, got %q", spec.Command)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "heartbeat" {
		t.Errorf("operator-heartbeat args = %v, want [heartbeat]", spec.Args)
	}
	if spec.ScheduleID() != "chitin-job-operator-heartbeat" {
		t.Errorf("ScheduleID = %q, want chitin-job-operator-heartbeat", spec.ScheduleID())
	}
}
