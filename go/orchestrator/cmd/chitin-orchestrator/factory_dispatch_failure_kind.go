package main

import (
	"errors"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// FactoryDispatchFailureKind is the closed taxonomy carried by
// factory_dispatch_failed events. New values require a follow-up spec; callers
// must not synthesize values outside this type's declared constants.
type FactoryDispatchFailureKind string

const (
	FactoryDispatchFailureKindSpecRefNotFound             FactoryDispatchFailureKind = "spec_ref_not_found"
	FactoryDispatchFailureKindSpecRefAmbiguous            FactoryDispatchFailureKind = "spec_ref_ambiguous"
	FactoryDispatchFailureKindTasksMDMissing              FactoryDispatchFailureKind = "tasks_md_missing"
	FactoryDispatchFailureKindTasksMDParseError           FactoryDispatchFailureKind = "tasks_md_parse_error"
	FactoryDispatchFailureKindTemporalDialFailed          FactoryDispatchFailureKind = "temporal_dial_failed"
	FactoryDispatchFailureKindTemporalStartWorkflowFailed FactoryDispatchFailureKind = "temporal_start_workflow_failed"
	FactoryDispatchFailureKindCapabilityMismatch          FactoryDispatchFailureKind = "capability_mismatch"
	FactoryDispatchFailureKindInternal                    FactoryDispatchFailureKind = "internal"
)

func (k FactoryDispatchFailureKind) Valid() bool {
	switch k {
	case FactoryDispatchFailureKindSpecRefNotFound,
		FactoryDispatchFailureKindSpecRefAmbiguous,
		FactoryDispatchFailureKindTasksMDMissing,
		FactoryDispatchFailureKindTasksMDParseError,
		FactoryDispatchFailureKindTemporalDialFailed,
		FactoryDispatchFailureKindTemporalStartWorkflowFailed,
		FactoryDispatchFailureKindCapabilityMismatch,
		FactoryDispatchFailureKindInternal:
		return true
	default:
		return false
	}
}

// classifyDispatchError maps schedule/dispatch errors into the closed
// factory-dispatch taxonomy. Keep every heuristic here so the emit boundary
// stays closed even when upstream packages return string-typed errors.
func classifyDispatchError(err error) FactoryDispatchFailureKind {
	if err == nil {
		return FactoryDispatchFailureKindInternal
	}

	var sre *SpecRefError
	if errors.As(err, &sre) {
		switch sre.Kind {
		case "not-found":
			return FactoryDispatchFailureKindSpecRefNotFound
		case "ambiguous":
			return FactoryDispatchFailureKindSpecRefAmbiguous
		}
	}

	var malformed *adapter.MalformedArtifactError
	if errors.As(err, &malformed) {
		if strings.HasSuffix(malformed.File, "tasks.md") &&
			strings.Contains(strings.ToLower(malformed.Reason), "missing") {
			return FactoryDispatchFailureKindTasksMDMissing
		}
		return FactoryDispatchFailureKindTasksMDParseError
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no spec matching ref") ||
		strings.Contains(msg, "no spec directory matching"):
		return FactoryDispatchFailureKindSpecRefNotFound
	case strings.Contains(msg, "spec ref") && strings.Contains(msg, "ambiguous"),
		strings.Contains(msg, "ref ") && strings.Contains(msg, " is ambiguous"):
		return FactoryDispatchFailureKindSpecRefAmbiguous
	case strings.Contains(msg, "tasks.md") &&
		(strings.Contains(msg, "required artifact is missing") ||
			strings.Contains(msg, "no such file") ||
			strings.Contains(msg, "missing")):
		return FactoryDispatchFailureKindTasksMDMissing
	case strings.Contains(msg, "tasks.md") &&
		(strings.Contains(msg, "parse") ||
			strings.Contains(msg, "malformed artifact") ||
			strings.Contains(msg, "compile failed")):
		return FactoryDispatchFailureKindTasksMDParseError
	case strings.Contains(msg, "temporal unreachable") ||
		strings.Contains(msg, "client.dial") ||
		strings.Contains(msg, "dial tcp"):
		return FactoryDispatchFailureKindTemporalDialFailed
	case strings.Contains(msg, "executeworkflow failed") ||
		strings.Contains(msg, "startworkflow"):
		return FactoryDispatchFailureKindTemporalStartWorkflowFailed
	case strings.Contains(msg, "dag validation failed") ||
		strings.Contains(msg, "unroutable") ||
		strings.Contains(msg, "no registered driver declares") ||
		strings.Contains(msg, "capability is not in the closed taxonomy"):
		return FactoryDispatchFailureKindCapabilityMismatch
	default:
		return FactoryDispatchFailureKindInternal
	}
}
