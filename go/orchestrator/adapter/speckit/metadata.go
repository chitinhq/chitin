package speckit

import (
	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// priorityBase is the priority a spec-kit task in the first phase receives.
// Each later phase steps the priority down by priorityStep, so the scheduler
// — which dispatches higher priority first (spec 076 Node.Priority) — runs
// earlier phases before later ones. The base is high enough that several
// dozen phases still leave every priority positive.
const (
	priorityBase = 1000
	priorityStep = 10
)

// DerivePriority maps a task's phase ordinal to a node priority. Phase 1
// tasks get priorityBase; each later phase is priorityStep lower. A task that
// appeared before any phase header (PhaseSeq 0) is treated as setup and gets
// the highest priority (priorityBase + priorityStep) so it leads.
//
// Priority is derived purely from declared phase position — never from a
// heuristic read of the description (spec 070 FR-015: priority is a declared
// property). Two tasks in the same phase get the same priority; the
// scheduler breaks that tie by node ID.
func DerivePriority(t Task) int {
	if t.PhaseSeq == 0 {
		return priorityBase + priorityStep
	}
	return priorityBase - (t.PhaseSeq-1)*priorityStep
}

// MapCapability maps a task to exactly one closed-taxonomy capability tag, or
// reports that the task is ambiguous (FR-004, FR-014). It delegates to
// adapter.MapCapability — the shared, conservative keyword mapping — so every
// kit draws capabilities from the same closed spec-075 vocabulary.
//
// When the mapping is unambiguous it returns (tag, true) and tag is
// guaranteed to satisfy driver.IsKnownCapability. When the description maps
// to no capability, or to more than one, it returns ("", false): the caller
// MUST then set the node's capability to adapter.NeedsClarification and
// record the reason — never invent a tag (FR-014).
func MapCapability(t Task) (driver.Capability, bool) {
	return adapter.MapCapability(t.Description)
}

// capabilityClarification is the clarification reason recorded on a node
// whose task description could not be mapped to a single taxonomy
// capability. It is stable text so tests and operators can match on it.
const capabilityClarification = "capability could not be mapped to the closed taxonomy from the task description"
