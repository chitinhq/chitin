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

// The operator-digest job is registered, daily, and runs the delivery script
// in digest mode (spec 085 US2, contract C3).
func TestRegistry_HasOperatorDigest(t *testing.T) {
	var spec *JobSpec
	for _, s := range Registry() {
		if s.Name == "operator-digest" {
			sCopy := s
			spec = &sCopy
			break
		}
	}
	if spec == nil {
		t.Fatal("operator-digest not found in Registry()")
	}
	if spec.Cron != "0 8 * * *" {
		t.Errorf("operator-digest cron = %q, want daily \"0 8 * * *\"", spec.Cron)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "digest" {
		t.Errorf("operator-digest args = %v, want [digest]", spec.Args)
	}
	if !strings.HasSuffix(spec.Command, "deliver-operator-report.sh") {
		t.Errorf("operator-digest command should be the delivery script, got %q", spec.Command)
	}
	if spec.ScheduleID() != "chitin-job-operator-digest" {
		t.Errorf("ScheduleID = %q, want chitin-job-operator-digest", spec.ScheduleID())
	}
}
