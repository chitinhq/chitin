package main

import (
	"errors"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// FactoryDispatchFailureKind is the closed taxonomy for
// factory_dispatch_failed.failure_kind. New values require a spec update.
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

// Valid reports whether k is one of the declared dispatch failure kinds.
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

// classifyDispatchError maps dispatch failures to the closed FR-001 taxonomy.
// It prefers typed errors from the local scheduler path and falls back to the
// legacy stderr strings wrapped by factoryHandler.dispatch.
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
	case strings.Contains(msg, "no spec matching ref"):
		return FactoryDispatchFailureKindSpecRefNotFound
	case strings.Contains(msg, " is ambiguous") || strings.Contains(msg, "ref ") && strings.Contains(msg, "ambiguous"):
		return FactoryDispatchFailureKindSpecRefAmbiguous
	case strings.Contains(msg, "tasks.md") && (strings.Contains(msg, "required artifact is missing") || strings.Contains(msg, "no such file")):
		return FactoryDispatchFailureKindTasksMDMissing
	case strings.Contains(msg, "tasks.md") && (strings.Contains(msg, "malformed artifact") || strings.Contains(msg, "parse") || strings.Contains(msg, "not a spec-kit")):
		return FactoryDispatchFailureKindTasksMDParseError
	case strings.Contains(msg, "temporal unreachable") || strings.Contains(msg, "client.dial") || strings.Contains(msg, "dial tcp"):
		return FactoryDispatchFailureKindTemporalDialFailed
	case strings.Contains(msg, "executeworkflow failed") || strings.Contains(msg, "startworkflow"):
		return FactoryDispatchFailureKindTemporalStartWorkflowFailed
	case strings.Contains(msg, "dag validation failed") || strings.Contains(msg, "unroutable") || strings.Contains(msg, "no registered driver declares") || strings.Contains(msg, "capability mismatch"):
		return FactoryDispatchFailureKindCapabilityMismatch
	default:
		return FactoryDispatchFailureKindInternal
	}
}
