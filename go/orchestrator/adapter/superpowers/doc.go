// Package superpowers is a documented stub for the superpowers spec-kit
// adapter (spec 077, FR-006). superpowers is a skill-driven methodology:
// instead of a `tasks.md` dependency-ordered list, the work units live inside
// `docs/superpowers/plans/<date>-<name>.md` plan documents whose phases and
// numbered steps imply the ordering.
//
// The spec-077 foundation slice delivers two complete, tested adapters —
// spec-kit (the P1 MVP) and OpenSpec (the P2 second kit) — which together
// already prove the kit-agnostic thesis: two kits compile through the one
// SpecKitAdapter interface with zero scheduler change (SC-001, SC-003). A
// third adapter is additive and, by construction, isolated to this
// sub-directory.
//
// TODO(spec-077 Phase 5, US3): implement adapter.go + parse.go here — a
// superpowers.Adapter whose:
//
//   - Detect reports true on the presence of `docs/superpowers/` (the
//     superpowers skill marker);
//   - Compile parses a plan document's "## Phase N" / numbered-step
//     structure into one dag.Node per step, edges from step ordering, and
//     capability mapped from each step's text via adapter.MapCapability —
//     a step that maps to no closed-taxonomy capability is marked
//     adapter.NeedsClarification, never given an invented tag (FR-014).
//
// Registering it is then a single line in the default registry — nothing in
// the scheduler or orchestrator core moves (FR-002). Left as a documented
// stub because the spec-kit and OpenSpec adapters already satisfy FR-006's
// "at least two adapters" requirement and the P1/P2 slices.
package superpowers
