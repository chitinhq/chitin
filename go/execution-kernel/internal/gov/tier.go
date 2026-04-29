package gov

// Tier classifies an Action by cost class. It is metadata stamped on
// Decision rows for downstream routing analytics. Chitin does not
// execute differently based on Tier — the label informs future
// session-spawn-routing tools that "this action class is cheap".
//
// See docs/superpowers/specs/2026-04-29-cost-governance-kernel-design.md
// "Why permission-gate, not tool harness".
type Tier string

const (
	// TierUnset is the zero value. Decisions written before tier classification
	// shipped (or where classification is intentionally skipped) carry this.
	TierUnset Tier = ""

	// T0Local labels deterministic queries: file.read, git.{diff,log,status},
	// github.{pr,issue}.{view,list}, http.request to allowlist.
	T0Local Tier = "T0"

	// T1Cheap is reserved for Haiku-class cheap cloud routing. No
	// implementation in v3; reserved to avoid renaming when ecosystem
	// phase lands.
	T1Cheap Tier = "T1"

	// T2Expensive labels side-effect or judgment actions: file.write,
	// git.commit, github.pr.create, etc. Default for unclassified.
	T2Expensive Tier = "T2"
)
