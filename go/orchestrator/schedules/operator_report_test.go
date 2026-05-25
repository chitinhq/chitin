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

// The operator-queue-digest job (spec 114 US2 FR-009) is registered, daily at
// 09:00 America/Detroit, and dispatches to the in-process
// OperatorQueueDigestWorkflow rather than the generic subprocess runner.
func TestRegistry_HasOperatorQueueDigest(t *testing.T) {
	var spec *JobSpec
	for _, s := range Registry() {
		if s.Name == "operator-queue-digest" {
			sCopy := s
			spec = &sCopy
			break
		}
	}
	if spec == nil {
		t.Fatal("operator-queue-digest not found in Registry()")
	}
	if spec.Cron != "0 9 * * *" {
		t.Errorf("operator-queue-digest cron = %q, want daily \"0 9 * * *\"", spec.Cron)
	}
	if spec.TimeZone != "America/Detroit" {
		t.Errorf("operator-queue-digest TimeZone = %q, want %q", spec.TimeZone, "America/Detroit")
	}
	if spec.Workflow != OperatorQueueDigestWorkflowName {
		t.Errorf("operator-queue-digest Workflow = %q, want %q",
			spec.Workflow, OperatorQueueDigestWorkflowName)
	}
	// In-process — no subprocess Command should be set. The Workflow override
	// is the entire dispatch surface (spec 114 US2: NOT via subprocess).
	if spec.Command != "" {
		t.Errorf("operator-queue-digest Command = %q, want empty (in-process)", spec.Command)
	}
	if len(spec.Args) != 0 {
		t.Errorf("operator-queue-digest Args = %v, want none (in-process)", spec.Args)
	}
	if spec.Description == "" {
		t.Error("operator-queue-digest Description is empty — the Schedule note would be blank")
	}
	if spec.ScheduleID() != "chitin-job-operator-queue-digest" {
		t.Errorf("ScheduleID = %q, want chitin-job-operator-queue-digest", spec.ScheduleID())
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
