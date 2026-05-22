# Feature Specification: Event-Hash Consolidation

**Feature Branch**: `086-event-hash-consolidation`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Consolidate the duplicated event-hash implementation surfaced
by the Pathfinder codebase analysis (cluster CF1 / proposal UP1 / handoff HP1)."

The tamper-evident audit chain is only trustworthy if every component that emits an event
computes the *same* hash for the *same* event. Today the canonical-JSON + SHA-256 event
hash is implemented three separate times — once in TypeScript and twice in Go — and one of
the Go copies has drifted in behavior. This feature removes that risk by giving the Go side
a single source of truth and proving cross-language agreement with an automated test.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - An event hashes identically no matter which component emits it (Priority: P1)

An event emitted through the standalone run SDK and the same event emitted through the
kernel must receive an identical chain hash. Today they can differ: the two Go hash copies
disagree at one boundary case (handling of a payload value that is not directly
representable as plain JSON), so the same event can produce two different hashes depending
on which component hashed it. When that happens, the audit chain either fails verification
or accepts a record that should have linked cleanly — both outcomes erode the integrity
guarantee the chain exists to provide. This story makes the kernel and the run SDK share
one hashing behavior so the disagreement cannot occur.

**Why this priority**: This is the correctness fix. Without it, the audit chain — the
platform's cross-driver, cross-session source of truth — can silently produce false tamper
signals or break linkage for SDK-emitted events. Everything else in this feature protects
or tidies this fix; this story alone delivers the value.

**Independent Test**: Run a shared corpus of events (including the boundary case) through
the kernel hashing path and the run-SDK hashing path; assert every event yields an
identical hash value, and that the boundary case yields one defined, identical result from
both. Delivers a viable MVP on its own — the divergence is gone.

**Acceptance Scenarios**:

1. **Given** an event with a nested, multi-level payload, **When** it is hashed by the
   kernel and by the run SDK, **Then** both produce the identical hash value.
2. **Given** an event whose payload contains a value at the historically divergent boundary
   case, **When** it is hashed by the kernel and by the run SDK, **Then** both produce the
   same outcome — either the same hash or the same defined error — never two different hashes.
3. **Given** an audit chain whose events were hashed before this change, **When** the chain
   is verified after this change, **Then** every event still verifies and no stored hash has changed.

---

### User Story 2 - A change that breaks hash agreement is caught before it merges (Priority: P2)

The three implementations agree today only by manual diligence; nothing fails when they
drift. This story adds an automated parity check: a shared corpus of events — ordinary
cases plus edge cases — is run through every implementation, and the check fails if any two
implementations disagree. It runs in continuous integration so a drift is blocked before
merge rather than discovered when a chain fails to verify in production.

**Why this priority**: Consolidation that is not enforced re-diverges. P2 because the P1
fix delivers value immediately, but without this guard the value decays the next time
anyone edits a hashing codepath.

**Independent Test**: Confirm the parity check exists and runs in CI; then deliberately
introduce a divergence into one implementation and confirm the check fails and blocks the
change.

**Acceptance Scenarios**:

1. **Given** the shared parity corpus, **When** the test suite runs, **Then** it exercises
   every implementation against the full corpus and reports identical output across all of them.
2. **Given** a deliberately introduced divergence in one implementation, **When** continuous
   integration runs, **Then** the parity check fails and the change is blocked from merging.

---

### User Story 3 - There is exactly one Go hash implementation to maintain (Priority: P3)

After the fix and the guard are in place, the duplicate Go hash source is removed so no
stale copy survives to be edited by mistake. An engineer changing hashing behavior changes
it in one place; there is no second Go copy that can quietly fall out of step.

**Why this priority**: Cleanup that prevents the next divergence at the source. Lower
priority because P1 + P2 already make the system correct and protected; this removes the
latent footgun.

**Independent Test**: Search the Go codebase for the event-hash implementation and confirm
exactly one exists; run the full build and existing test suites and confirm they pass with
no reference to a removed duplicate.

**Acceptance Scenarios**:

1. **Given** the consolidated codebase, **When** an engineer searches for the Go
   event-hash implementation, **Then** exactly one implementation is found.
2. **Given** the consolidation is complete, **When** the full build and the existing kernel
   and run-SDK test suites run, **Then** they all pass and nothing references a removed copy.

---

### Edge Cases

- **Non-JSON-representable payload value** — the historical point of divergence. The two Go
  copies handle it differently today (one is lenient, one rejects). The unified behavior
  must be a single defined outcome applied everywhere.
- **Empty or null content** — an event with an empty payload, or null envelope fields, must
  hash deterministically and identically across implementations.
- **Deep nesting** — object keys must be ordered deterministically at every level of nesting,
  not just the top level.
- **Non-ASCII / Unicode string values** in payload or envelope fields.
- **Numeric edge values** — large integers and floating-point values must serialize
  identically across implementations.
- **Historical chains** — events hashed by a pre-consolidation implementation must continue
  to verify unchanged; the consolidation must not alter the hash of any valid event.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST have exactly one Go implementation of the event hash (canonical
  JSON serialization followed by SHA-256), used by both the kernel and the standalone run SDK.
- **FR-002**: The unified Go implementation MUST produce a hash byte-identical to the
  TypeScript reference implementation for every event in a shared parity corpus.
- **FR-003**: For a payload value that is not directly representable as plain JSON (the
  historical divergence point), every implementation MUST exhibit one identical, defined
  behavior — they MUST NOT produce two different hashes for the same input.
- **FR-004**: The consolidation MUST NOT change the hash of any valid event; every event in
  every existing audit chain MUST continue to verify exactly as it did before the change.
- **FR-005**: An automated parity check MUST run every implementation against the shared
  corpus and MUST fail if any two implementations disagree.
- **FR-006**: The parity check MUST run in continuous integration so a hash divergence is
  blocked before merge.
- **FR-007**: The standalone run SDK MUST remain embeddable by third-party tools without
  requiring the kernel or kernel-only dependencies; the shared hash code MUST NOT introduce
  such a dependency.
- **FR-008**: After consolidation, no Go component MUST retain a private copy of the event
  hash; the duplicate source MUST be removed.
- **FR-009**: The TypeScript implementation MUST remain the cross-language specification of
  record for hashing behavior; any future behavior change is defined there first and the
  parity check enforces the Go side against it.

### Key Entities

- **Event**: The unit being hashed — an envelope of metadata fields plus a payload. Its
  hash is the link that chains it to the previous event in the audit chain.
- **Canonical form**: The deterministic serialization of an event (recursively key-sorted,
  whitespace-free) over which the hash is computed. Two events are "the same" for hashing
  purposes if and only if their canonical forms are identical.
- **Parity corpus**: The shared, version-controlled set of fixture events — ordinary cases
  plus every edge case above — that all implementations are tested against.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across the full parity corpus, every component that emits or verifies events
  produces an identical hash for an identical event — 100% agreement, zero disagreements.
- **SC-002**: Changing event-hashing behavior requires editing one implementation per
  language; the most-duplicated language drops from two implementations to one.
- **SC-003**: A deliberately introduced hash divergence is detected by the automated parity
  check 100% of the time, and is blocked before it can merge.
- **SC-004**: Zero events in any existing audit chain fail verification after the change —
  historical chains verify exactly as before.
- **SC-005**: Tools that embed the standalone run SDK can continue to do so with no new
  external or third-party dependency.

## Assumptions

- **Boundary behavior resolves to strict.** Where the two Go copies differ on a
  non-JSON-representable payload value, the unified implementation adopts the strict
  behavior (reject with a defined error) rather than the lenient round-trip fallback. The
  run SDK already converts payloads to plain JSON values before hashing, so the lenient
  fallback is effectively unreachable in practice; making the behavior strict everywhere
  removes the divergence without affecting real callers. (Source: Pathfinder proposal UP1.)
- **The hashing algorithm itself does not change.** This is a consolidation, not a redesign.
  The canonical-form rules (recursive key sorting, no whitespace) and SHA-256 hex output
  are unchanged, which is what guarantees historical chains keep verifying (FR-004, SC-004).
- **The TypeScript implementation is not merged into Go.** It is a different language and
  remains the cross-language reference; only the *two Go copies* are consolidated into one.
- **Separate Go modules.** The kernel and the run SDK are separate Go modules today, so a
  neutral shared module (or an equivalent workspace arrangement) is required for both to
  import one implementation. The exact mechanism is a planning-phase decision; the shared
  code must itself stay dependency-light to satisfy FR-007.
- **Scope boundary — in scope**: the two Go hash implementations, the shared parity corpus,
  the automated parity check, and removal of the duplicate.
- **Scope boundary — out of scope**: rewriting the TypeScript implementation; any change to
  the canonical-JSON algorithm or hash output; the kernel's deferred full chain-verify hash
  recomputation (a separate, unimplemented effort).
- **Dependency**: This feature originates from the Pathfinder analysis in
  `PATHFINDER-2026-05-22/` (duplication cluster CF1, unified proposal UP1, handoff prompt
  HP1), which carries the verified `file:line` evidence for every implementation involved.
