// validate.go — DAG pre-validation for spec 097 `schedule` (FR-004).
//
// The schedule subcommand refuses to dispatch a DAG that the scheduler will
// already know it cannot complete. Two refusal kinds:
//
//   - "needs_clarification" — a task's capability resolved to the
//     adapter.NeedsClarification sentinel; the kit author's description
//     didn't match a known capability keyword set. The scheduler would mark
//     it blocked-unroutable on the first tick; better to fail at the
//     operator's keyboard.
//
//   - "unroutable" — the task's capability is a real tag but no driver
//     in the registered registry declares it. Same outcome (blocked-
//     unroutable) but a different cause; the operator's fix is to register
//     a driver, not amend the task.
//
// Per research.md D3, this re-uses the same registry construction the
// scheduler's SelectDriver activity will consult at runtime (buildRegistry
// in main.go), so pre-validation and runtime agree on routability.

package main

import (
	"context"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// ValidationError is one offense found by ValidateForDispatch. Empty
// []ValidationError = valid DAG.
type ValidationError struct {
	NodeID     string // the offending DAG node id
	Capability string // the capability the node declares (or "" if missing)
	Kind       string // "needs_clarification" | "unroutable" | "missing_capability"
	Detail     string // operator-readable explanation, one line
}

// ValidateForDispatch walks every agent node in the DAG and reports
// validation errors. Deterministic node ordering is preserved (the DAG's
// Nodes() accessor returns them sorted by ID).
//
// Deterministic nodes (Kind = NodeKindDeterministic) are not validated for
// capability routability — they're executed via the RunDeterministicStep
// activity, not by selecting a driver. They DO get a "missing-command"
// check so a malformed deterministic node fails fast.
func ValidateForDispatch(ctx context.Context, d *dag.DAG, reg *driver.Registry) []ValidationError {
	if d == nil {
		return []ValidationError{{Kind: "missing_capability", Detail: "DAG is nil"}}
	}
	var errs []ValidationError
	for _, n := range d.Nodes() {
		if n.Kind == dag.NodeKindDeterministic {
			if n.Command == "" {
				errs = append(errs, ValidationError{
					NodeID: n.ID,
					Kind:   "missing_capability",
					Detail: "deterministic node has empty Command",
				})
			}
			continue
		}
		// Agent node — must have a routable capability.
		switch {
		case n.Capability == "":
			errs = append(errs, ValidationError{
				NodeID: n.ID,
				Kind:   "missing_capability",
				Detail: "agent node has no capability tag",
			})
		case n.Capability == adapter.NeedsClarification:
			errs = append(errs, ValidationError{
				NodeID:     n.ID,
				Capability: n.Capability,
				Kind:       "needs_clarification",
				Detail:     "task description did not map to a known capability keyword (amend tasks.md to use a recognized keyword set)",
			})
		default:
			if !driver.IsKnownCapability(n.Capability) {
				errs = append(errs, ValidationError{
					NodeID:     n.ID,
					Capability: n.Capability,
					Kind:       "unroutable",
					Detail:     "capability is not in the closed taxonomy (driver/taxonomy.go)",
				})
				continue
			}
			drivers := reg.DriversFor(ctx, driver.Capability(n.Capability))
			if len(drivers) == 0 {
				errs = append(errs, ValidationError{
					NodeID:     n.ID,
					Capability: n.Capability,
					Kind:       "unroutable",
					Detail:     "no registered driver declares this capability — register a driver that does, or amend tasks.md",
				})
			}
		}
	}
	return errs
}
