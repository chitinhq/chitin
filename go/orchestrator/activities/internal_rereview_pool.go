package activities

import (
	"fmt"
	"os"
	"strings"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// internalRereviewPoolEnv names the env var the operator uses to configure
// the spec 116 internal re-review pool — a comma-separated list of driver
// ids eligible to review a fixup. Default pool when unset: codex +
// claudecode (the two drivers currently registered on the operator-host).
const internalRereviewPoolEnv = "CHITIN_INTERNAL_REREVIEW_POOL"

// defaultInternalRereviewPool is the fallback when the env var is unset.
// Matches the operator-host's standard registered driver set. Keeping the
// default short and stable so a fresh operator-host gets a sensible
// re-review without having to tune env vars.
var defaultInternalRereviewPool = []string{"codex", "claudecode"}

// PoolSelectionReason classifies why the resolver returned an empty
// selection. Operators (and the chain event) read this to triage why a
// fixup didn't get an internal re-review.
type PoolSelectionReason string

const (
	// PoolReasonNoPool — no env-configured pool AND the default list
	// resolved to zero registered drivers (operator-host has nothing).
	PoolReasonNoPool PoolSelectionReason = "no_pool_configured"
	// PoolReasonEmptyAfterExclusion — pool was non-empty but every
	// member was excluded by R-AUTHORID (the only registered driver IS
	// the fixup author). Common on single-driver operator-hosts.
	PoolReasonEmptyAfterExclusion PoolSelectionReason = "empty_after_author_exclusion"
	// PoolReasonNoRegistry — registry was nil (shouldn't happen in
	// production but the resolver is defensive).
	PoolReasonNoRegistry PoolSelectionReason = "no_registry_bound"
)

// PoolSelection is the closed result of resolveRereviewerDriver. Exactly
// one of (DriverID populated) or (Reason populated) is non-empty on any
// return; never both.
type PoolSelection struct {
	// DriverID is the selected re-reviewer driver id when non-empty.
	DriverID string
	// Reason names why the selection is empty when DriverID is empty.
	Reason PoolSelectionReason
}

// resolveRereviewerDriver picks the driver that re-reviews a fixup commit
// per spec 116 FR-001/002. It:
//
//  1. Reads $CHITIN_INTERNAL_REREVIEW_POOL (comma-separated driver ids)
//     OR falls back to defaultInternalRereviewPool when unset.
//  2. Intersects against the driver registry — pool entries with no
//     matching registered driver are silently dropped.
//  3. Excludes the fixupAuthor id per spec 094 R-AUTHORID (no driver
//     reviews its own work).
//  4. Returns the FIRST surviving entry (pool order = priority).
//
// On empty result, returns a populated Reason so the workflow can emit
// the right chain event (internal_rereview_skipped with the reason).
func resolveRereviewerDriver(reg *driver.Registry, fixupAuthor string) PoolSelection {
	if reg == nil {
		return PoolSelection{Reason: PoolReasonNoRegistry}
	}

	pool := configuredPool()

	// Filter to (a) registered AND (b) NOT the fixup author.
	for _, id := range pool {
		if id == fixupAuthor {
			continue
		}
		if _, ok := reg.Driver(id); ok {
			return PoolSelection{DriverID: id}
		}
	}

	// Distinguish "no pool" from "pool ruled out by R-AUTHORID" — the
	// chain reason differs because the operator's fix differs (add a
	// driver vs. configure more drivers in the pool).
	if hasNonAuthorEntry(pool, fixupAuthor) {
		// Pool had at least one non-author entry but it wasn't in the
		// registry — operator misconfigured the pool to reference an
		// unregistered driver. Surface as no_pool so the operator goes
		// looking for the registry mismatch.
		return PoolSelection{Reason: PoolReasonNoPool}
	}
	return PoolSelection{Reason: PoolReasonEmptyAfterExclusion}
}

// configuredPool returns the pool order — env-configured if set, else the
// default. Empty / whitespace-only entries are dropped; the input order
// is preserved (pool order = priority).
func configuredPool() []string {
	env := strings.TrimSpace(os.Getenv(internalRereviewPoolEnv))
	if env == "" {
		out := make([]string, len(defaultInternalRereviewPool))
		copy(out, defaultInternalRereviewPool)
		return out
	}
	var pool []string
	for _, raw := range strings.Split(env, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		pool = append(pool, id)
	}
	return pool
}

// hasNonAuthorEntry reports whether pool contains at least one entry
// other than fixupAuthor. Used to disambiguate the empty-after-exclusion
// case from the no-pool case in the chain reason.
func hasNonAuthorEntry(pool []string, fixupAuthor string) bool {
	for _, id := range pool {
		if id != fixupAuthor {
			return true
		}
	}
	return false
}

// poolDescribe renders the selection for log lines / chain payloads.
func (s PoolSelection) String() string {
	if s.DriverID != "" {
		return fmt.Sprintf("selected=%s", s.DriverID)
	}
	return fmt.Sprintf("empty reason=%s", s.Reason)
}
