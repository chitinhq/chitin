package driver

import (
	"context"
	"fmt"
	"sort"
)

// Registry is the startup-loaded set of AgentDrivers the scheduler queries
// (FR-004). It answers two questions: "which registered, ready drivers
// satisfy capability C?" (DriversFor) and "which one should run this work?"
// (Select). The registry is in-memory and loaded from configuration at
// orchestrator startup; it holds no datastore.
//
// A Registry value must be created with NewRegistry. The zero value is not
// usable. Registry is not safe for concurrent registration; register all
// drivers at startup, then treat the Registry as read-only — DriversFor and
// Select are safe to call concurrently once registration is complete.
type Registry struct {
	// byID holds drivers keyed by their stable id. It is only ever used for
	// lookup and membership — never iterated for ordering. Every order this
	// package produces comes from an explicit sort, never from ranging this
	// map (determinism — FR-005).
	byID map[string]AgentDriver
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]AgentDriver)}
}

// Register admits one driver into the registry. It enforces the registration
// contract before the driver becomes routable:
//
//   - the driver id must be non-empty and must match its card's DriverID;
//   - the id must be unique within the registry;
//   - the card's Tier and CostClass must be declared enum values;
//   - every capability tag on the card must belong to the closed taxonomy —
//     a card carrying an unknown tag is rejected with an "unknown
//     capability" error naming the offending tag (FR-015, edge case
//     "declares a tag outside the capability taxonomy").
//
// On any violation Register returns a non-nil error and the registry is
// unchanged. (FR-009 — rejecting a driver whose agent can bypass the kernel
// — is enforced here too; that check needs the governance probe wired in
// Phase 5 and is left as a documented TODO below.)
func (r *Registry) Register(d AgentDriver) error {
	if d == nil {
		return fmt.Errorf("driver: cannot register a nil driver")
	}
	id := d.ID()
	if id == "" {
		return fmt.Errorf("driver: cannot register a driver with an empty id")
	}

	card := d.Card()
	if card.DriverID != id {
		return fmt.Errorf(
			"driver %q: card DriverID %q does not match AgentDriver.ID()",
			id, card.DriverID,
		)
	}
	if _, exists := r.byID[id]; exists {
		return fmt.Errorf("driver %q: already registered", id)
	}
	if !card.Tier.Valid() {
		return fmt.Errorf("driver %q: card declares invalid tier %d", id, card.Tier)
	}
	if !card.CostClass.Valid() {
		return fmt.Errorf("driver %q: card declares invalid cost class %d", id, card.CostClass)
	}
	for _, cap := range card.Capabilities {
		if !IsKnownCapability(string(cap)) {
			return fmt.Errorf(
				"driver %q: unknown capability %q — not in the closed taxonomy",
				id, cap,
			)
		}
	}

	// TODO(spec-075 Phase 5, FR-009/FR-010): reject a driver whose agent can
	// bypass chitin kernel governance, and record the capability card in the
	// chitin chain here so the declared contract is itself audited. Both
	// need the kernel-governance probe and the chain client, which are not
	// yet wired into the orchestrator module.

	r.byID[id] = d
	return nil
}

// Len reports how many drivers are registered.
func (r *Registry) Len() int { return len(r.byID) }

// Driver returns the registered driver with the given id, or (nil, false)
// if no driver is registered under that id.
func (r *Registry) Driver(id string) (AgentDriver, bool) {
	d, ok := r.byID[id]
	return d, ok
}

// Drivers returns every registered driver, ordered by driver id. The order
// is deterministic (lexical by id) and the slice is freshly allocated; the
// caller may not mutate the registry through it.
func (r *Registry) Drivers() []AgentDriver {
	out := make([]AgentDriver, 0, len(r.byID))
	for _, d := range r.byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// LookupByGitIdentity finds the driver, if any, whose CapabilityCard
// declares the given GitIdentity (spec 094 R-AUTHORID, FR-005). It is the
// bridge between a PR-author identifier (e.g., a GitHub login) and the
// driver that authored the work — the no-self-review exclusion's lookup
// substrate.
//
// Returns (driver, true) on a unique match; (nil, false) if no driver has
// that git identity or if the identity is empty. A driver with an empty
// GitIdentity field is never matched (the empty string is not a valid
// identity even if multiple drivers leave it unset).
//
// Lookup is O(n) over the registry; the registry is small (single-digit
// drivers at v1) so this is fine without an index. If multiple drivers
// somehow share a git identity — a registration-time error that this
// function does not validate — the first one in driver-id-ordered traversal
// wins, so the behaviour stays deterministic.
func (r *Registry) LookupByGitIdentity(gitIdentity string) (AgentDriver, bool) {
	if gitIdentity == "" {
		return nil, false
	}
	for _, d := range r.Drivers() { // id-ordered for deterministic tie-break
		if d.Card().GitIdentity == gitIdentity {
			return d, true
		}
	}
	return nil, false
}

// DriversFor returns exactly the registered, ready drivers whose capability
// card declares capability cap (FR-004). A driver is included only if both:
//
//   - its card lists cap, and
//   - its Ready(ctx) reports true.
//
// A driver whose agent is down or quota-exhausted reports not-ready and is
// therefore omitted (FR-008), so the scheduler routes elsewhere. The result
// is ordered deterministically by driver id; an unknown cap simply yields an
// empty slice (no card can legally declare an unknown tag). The returned
// slice is freshly allocated.
func (r *Registry) DriversFor(ctx context.Context, cap Capability) []AgentDriver {
	var matches []AgentDriver
	for _, d := range r.Drivers() { // r.Drivers() is already id-ordered
		if !d.Card().HasCapability(cap) {
			continue
		}
		if ready, _ := d.Ready(ctx); !ready {
			continue
		}
		matches = append(matches, d)
	}
	return matches
}

// BlockedUnroutableError is returned by Select when no registered, ready
// driver satisfies the required capability (FR-012). It names the missing
// capability so the scheduler can mark the work unit blocked-unroutable —
// the work is never silently dropped or arbitrarily assigned. Callers detect
// it with errors.As.
type BlockedUnroutableError struct {
	// Capability is the required tag that no ready driver could satisfy.
	Capability Capability
}

// Error implements the error interface.
func (e *BlockedUnroutableError) Error() string {
	return fmt.Sprintf(
		"blocked-unroutable: no registered, ready driver satisfies capability %q",
		e.Capability,
	)
}

// Select chooses the one driver that should run work requiring capability
// cap, deterministically (FR-004 + FR-005). It finds the ready,
// capability-matching candidates (DriversFor) and ranks them by the
// tier → cost class → driver-id total order (select.go).
//
// It returns the chosen driver and a human-readable selection reason for
// the audit record. When no ready driver satisfies cap it returns a
// *BlockedUnroutableError naming the missing capability (FR-012) — the
// caller marks the work unit blocked-unroutable; the work is never dropped
// or arbitrarily assigned.
//
// Select is deterministic: given the same registry state and the same cap
// it always returns the same driver and the same reason (SC-003).
func (r *Registry) Select(ctx context.Context, cap Capability) (AgentDriver, string, error) {
	candidates := r.DriversFor(ctx, cap)
	if len(candidates) == 0 {
		return nil, "", &BlockedUnroutableError{Capability: cap}
	}
	return selectDriver(candidates)
}
