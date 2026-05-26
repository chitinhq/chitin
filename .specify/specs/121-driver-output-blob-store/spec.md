---
spec_id: 121
title: Externalize large driver outputs to a content-addressed blob store
status: Draft
owner: chitinhq
created: 2026-05-25
depends_on:
  - 070
  - 119
related:
  - 094
  - 113
  - 116
  - 118
  - 120
---

# Spec 121 — Driver-output blob store

## Why

Spec 119 flipped the default dispatch unit from "task" to "spec".
The flip works — proven by spec 117 shipping as one coherent PR
within a single driver invocation — but it surfaced a latent
contract violation between drivers and Temporal that per-task
dispatch had hidden.

**The failure mode.** When the orchestrator dispatched specs 118
and 120 under whole-spec mode on 2026-05-25, both runs were
reported as `failed` in the Temporal UI. The driver work was
actually correct — the resulting PRs (#1130, #1131) merged
cleanly. What failed was the activity-result transport:

  - The codex driver's stdout for a 4-hour whole-spec invocation
    is the full session transcript (tool calls, file diffs,
    reasoning traces). Captured size on the spec 118 run: **2.8
    MiB.**
  - `driver/claudecode/driver.go:155` (and the codex analog at
    `driver/codex/driver.go:171`) builds the Result struct with
    `OutputRef: stdout` — the whole transcript becomes the
    activity-result field.
  - Temporal's activity-result payload ceiling is **2 MiB.** When
    the activity returns and the worker tries to record the
    result in workflow history, the gRPC frame exceeds the
    limit. Temporal raises `payloadSizeError`, fails the activity
    permanently, marks the workflow run failed.

The fix is not a bigger ceiling — Temporal's limit exists to keep
workflow history replayable in bounded memory. The fix is to stop
sending the full transcript across the activity boundary.

**Why externalize, not truncate.** `capture_pr_snapshot.go` already
solves a related problem for diffs via truncation
(`MaxTotalDiffBytes = 1.5 MiB`, `[diff truncated]` marker). That
trade-off is right for diffs because reviewer drivers can re-fetch
the full diff via `gh pr diff` when they need it. It is wrong for
whole-spec driver output because:

  - Sentinel's silent-drop detector (spec 118) reads driver output
    to detect partial implementations and dropped tasks; truncated
    output silently weakens that signal.
  - The dialectic re-review (spec 094 + 116) inspects driver
    reasoning traces to score adversarial robustness; reasoning
    that fell past the truncation boundary is invisible to it.
  - Operators debugging "why did this spec ship incomplete?" lose
    the most diagnostic part of the transcript — the late stages
    where the driver decided to stop.

A content-addressed blob store preserves the full artifact and
keeps the activity-result payload to a small pointer
(`blob://sha256/<hash>`). Consumers that need the body
(`Resolve`) get the full text; consumers that only need to route
on metadata (workflow history, chain emit) carry the pointer.

## User stories

### US1 (P1) — Driver outputs larger than the inline budget are externalized

> As the orchestrator running a whole-spec dispatch through
> claudecode or codex, when the driver's captured stdout exceeds
> the inline budget (1 MiB), the driver MUST write the full output
> to the blob store and set `Result.OutputRef` to a
> `blob://sha256/<hash>` reference. Outputs under the inline budget
> MUST continue to pass through as literal strings — no change for
> the common case.

**Independent test:** A hermetic test that constructs a synthetic
stdout buffer of 2.5 MiB, invokes the result-builder, asserts the
returned `Result.OutputRef` is a `blob://sha256/...` reference of
the expected length, and that `blob.Resolve(ctx, ref)` returns
exactly the original 2.5 MiB byte-for-byte. A second test passes a
4 KiB stdout buffer and asserts `OutputRef` is the literal string,
no blob write occurred.

### US2 (P1) — Workflow + activity round-trip succeeds for large outputs

> As the operator dispatching `chitin-orchestrator schedule
> 118-factory-dispatch-failed-reason-taxonomy` under whole-spec
> mode, the activity-result payload stays well under Temporal's
> 2 MiB limit regardless of how much the driver produces. The
> workflow records `WorkUnitResult{Status: succeeded, OutputRef:
> "blob://sha256/<hash>"}` in history and proceeds to delivery
> without `payloadSizeError`.

**Independent test:** Re-run spec 118's whole-spec dispatch against
the same driver/model that produced the 2.8 MiB output on
2026-05-25 (codex/gpt-5.5-codex). The workflow now completes
successfully; the resulting `WorkUnitResult.OutputRef` resolves
via the blob store to the same 2.8 MiB transcript; Temporal
records no `payloadSizeError`.

### US3 (P1) — Downstream consumers resolve pointers transparently

> As a downstream consumer (chain emit, sentinel ingest, dialectic
> re-review, any operator-facing surface that renders `OutputRef`),
> when I read a `WorkUnitResult` whose `OutputRef` is a blob
> reference, I call `blob.Resolve(ctx, ref)` to get the full body.
> Consumers that don't need the body (e.g. workflow routing on
> `Status`) are unaffected.

**Independent test:** Sentinel's existing silent-drop detector
runs against a whole-spec WorkUnitResult whose OutputRef is a
blob pointer. The detector calls `blob.Resolve` once, inspects the
full transcript, and produces the same finding it would have
produced if the transcript had been inline. No regression on
fixtures whose OutputRef is a literal string.

### US4 (P2) — Blob store is pluggable; filesystem is the default

> As an operator deploying chitin on a single Linux box (today's
> topology), the default blob store is a filesystem directory under
> `~/.chitin/blobs/` — zero configuration. As an operator
> deploying chitin across multiple hosts in the future, I can
> point the orchestrator at an S3-compatible bucket via
> environment variable, with no driver-side code changes.

**Independent test:** The blob store is a Go interface
(`blob.Store`) with two impls: `FSStore` (default) and `S3Store`
(stub interface satisfied; full impl can be a follow-up). A unit
test asserts `FSStore.Put` writes
`~/.chitin/blobs/<first2>/<rest>.blob`, returns the expected
`blob://sha256/<hash>` reference, and `Get` round-trips the
bytes. The `S3Store` interface compiles and has a build-only
smoke test (no live S3 dependency in CI).

### US5 (P2) — Chain events carry the pointer, not the body

> As the chain consumer reading `whole_spec_completed` events
> emitted per spec 119 FR-006, the event payload's `output_ref`
> field carries the `blob://sha256/<hash>` reference (when the
> driver output was externalized) or the literal short string
> (when inline). The event size stays bounded regardless of
> driver verbosity.

**Independent test:** Emit a `whole_spec_completed` event for a
work unit whose result was externalized; assert the event JSON
size is under 4 KiB regardless of the underlying transcript size.
Assert a downstream chain consumer reading the event can resolve
the body via `blob.Resolve` using only the event's `output_ref`
field.

## Functional requirements

- **FR-001** A `blob.Store` interface MUST be added at
  `go/orchestrator/internal/blob/store.go` with two methods:
  `Put(ctx context.Context, body []byte) (Ref, error)` and
  `Get(ctx context.Context, ref Ref) ([]byte, error)`. The `Ref`
  type wraps a `blob://sha256/<hex>` URI; its zero value is the
  empty reference (no blob).

- **FR-002** The default impl `FSStore` MUST write blobs under
  `$CHITIN_BLOB_DIR` (default: `~/.chitin/blobs/`) using the
  two-character-prefix sharding convention
  (`~/.chitin/blobs/ab/cdef...blob`). Writes MUST be atomic
  (write-to-tmp, fsync, rename) so a crash mid-write never leaves
  a partial blob with a valid name.

- **FR-003** Blob references MUST be content-addressed: the hash
  in the reference is the SHA-256 of the body bytes. Two Put
  calls with identical bodies MUST produce the same reference and
  MUST NOT duplicate the on-disk blob (the second `rename`
  becomes a no-op when the destination already exists with the
  same hash).

- **FR-004** A helper `Externalize(ctx, store, body) (string,
  error)` MUST be added at `internal/blob/externalize.go` that
  applies the inline-vs-blob policy: bodies ≤ `InlineThreshold`
  bytes are returned verbatim as their string form; bodies >
  threshold are written to the store and returned as a
  `blob://sha256/<hash>` URI string. `InlineThreshold` defaults
  to **1 MiB** (1,048,576 bytes) — well under Temporal's 2 MiB
  ceiling, leaving headroom for the rest of the `Result` struct.

- **FR-005** The claudecode driver's `resultFromCommand` (at
  `driver/claudecode/driver.go:154`) MUST call `Externalize` on
  `stdout` before assigning to `Result.OutputRef`. The codex
  driver's analog at `driver/codex/driver.go:170` (the
  build-result function, not the review-mode variant) MUST do
  the same.

- **FR-006** The `Result.Explanation` field MUST also be passed
  through `Externalize` at the same sites. Today the Explanation
  is small in the success path (`"driver %q completed work unit
  %q"`) but the failure path appends `runErr`'s message and
  `stderr`; a stderr blob over the threshold MUST also be
  externalized rather than truncated.

- **FR-007** A `blob.Resolve` helper MUST be added that detects
  the `blob://` URI prefix, parses the hash, calls
  `Store.Get`, and returns the body. Inputs without the
  `blob://` prefix are returned as-is (no-op), so consumers can
  call `Resolve` unconditionally without first inspecting the
  string.

- **FR-008** The `WorkUnitResult` struct and its consumers
  (`workflows/work_unit.go:235-242`, the delivery shim at lines
  256-303) MUST NOT change shape — the externalization is
  transparent to the workflow layer. The `OutputRef` field
  carries a `blob://` URI or a literal string; nothing else
  changes.

- **FR-009** Any operator-facing surface that today renders
  `OutputRef` MUST call `blob.Resolve` before rendering, so
  operator-facing output is always the body (transparent to
  humans). Same for chain-event consumers that surface
  `output_ref` to operators.

- **FR-010** The blob store MUST emit one chain event per Put
  (`blob_written`) with payload `{ref, size_bytes, sha256}`. The
  event is the audit trail that explains where bytes went and
  enables future GC (out of scope here, but the event is the
  hook).

- **FR-011** The `InlineThreshold` and `CHITIN_BLOB_DIR` MUST be
  surfaced in the orchestrator's startup log line so operators
  can see at a glance which threshold is active and where blobs
  will land.

- **FR-012** An `S3Store` skeleton MUST be added at
  `internal/blob/s3store.go` that satisfies the `Store` interface
  using the AWS SDK v2 types. The skeleton MAY return
  `errors.New("not implemented")` from `Put` / `Get` initially;
  the goal is to lock the interface shape now so a future spec
  can wire in the live AWS calls without touching driver code.

## Success criteria

- **SC-001** Re-running the spec 118 whole-spec dispatch against
  the same driver+model that produced the 2.8 MiB output (codex /
  gpt-5.5-codex on 2026-05-25) completes with workflow status
  `Completed` (not `Failed`). The resulting `WorkUnitResult.
  OutputRef` resolves to a 2.8 MiB byte-for-byte match against
  the original transcript captured from the failed run.

- **SC-002** Activity-result payload size for whole-spec
  dispatches is bounded above by ~4 KiB regardless of driver
  verbosity. Measured by Temporal's history payload bytes for
  the activity-completion event over a sample of 5 whole-spec
  runs.

- **SC-003** Inline pass-through is preserved for the common
  case: at least 80% of per-task dispatches (which produce small
  outputs) carry literal-string `OutputRef`s, not blob
  references. Measured via a one-shot script over the chain.

- **SC-004** Sentinel's silent-drop detector (spec 118) runs
  against externalized whole-spec outputs and produces findings
  byte-equivalent to a hypothetical inline run. Measured via a
  regression test that runs the detector against the same
  fixture in both inline and externalized form.

- **SC-005** Blob store size on disk after 7 days of normal
  whole-spec dispatch load (~5 whole-spec runs/day) stays under
  500 MiB. Reasoning: each run writes ~3 MiB; content-addressed
  dedup catches repeated boilerplate; the empirical ceiling is
  comfortably below the 500 MiB cap. (If this is exceeded, the
  GC follow-up becomes higher priority.)

## Scope

In:
  - `internal/blob/store.go` — `Store` interface + `Ref` type
  - `internal/blob/fsstore.go` — filesystem impl
  - `internal/blob/s3store.go` — S3 skeleton (interface only,
    `not implemented` body OK)
  - `internal/blob/externalize.go` — `Externalize` helper,
    inline-threshold policy
  - `internal/blob/resolve.go` — `Resolve` helper, transparent
    `blob://` → bytes
  - `driver/claudecode/driver.go` — `resultFromCommand` calls
    `Externalize` on `OutputRef` + `Explanation`
  - `driver/codex/driver.go` — same for the build-result function
    (NOT the review-mode `extractStructuredVerdict` path — that
    output is bounded by the verdict schema, can't blow the
    limit)
  - `cmd/chitin-orchestrator/main.go` (or wherever boot logs land)
    — log the active `InlineThreshold` and `CHITIN_BLOB_DIR`
  - `cmd/chitin-orchestrator/describe_run.go` (or analog) — call
    `Resolve` before rendering operator-facing output
  - Chain emit sites that include `output_ref` — no change to
    schema; the field just carries a `blob://` URI when
    externalized
  - Tests + documentation

Out:
  - Blob garbage collection (TTL-based or refcount-based). The
    `blob_written` event per FR-010 is the audit hook a future
    spec can use. Until then the 500 MiB / 7-day ceiling in
    SC-005 governs.
  - Encryption at rest. The blob store is plain bytes on disk;
    same trust posture as the rest of `~/.chitin/`.
  - Live S3 wiring. FR-012 locks the interface; a follow-up
    spec adds the AWS calls when multi-host topology lands.
  - Compression. Whole-spec transcripts are mostly text and
    would compress 3-5x with gzip, but the on-disk ceiling
    (SC-005) is already comfortable; deferred until measurement
    says otherwise.
  - Migration of existing on-disk artifacts (none today; the
    blob store is purely write-forward).

## Edge cases

  - **Concurrent Puts of the same content.** Two drivers
    finishing simultaneously with byte-identical stdout (e.g. two
    re-dispatches of the same spec): both compute the same hash,
    both rename to the same target. The OS handles the race —
    one rename wins, the other is a no-op (or the second writer
    detects the existing file and skips). Atomic-rename
    semantics on the underlying filesystem (ext4, xfs, btrfs)
    are sufficient.
  - **Driver exits with stderr larger than the threshold.** A
    failed driver invocation can emit huge stderr (claudecode
    occasionally dumps full conversation history on crash).
    FR-006 covers this — `Explanation` is externalized too. A
    sub-edge: the externalized Explanation's blob URI is then
    surfaced in the operator-facing `describe-run` output; the
    operator calls `Resolve` (transparent per FR-009) to see the
    stderr.
  - **Blob directory unwritable** (disk full, permission error).
    `Externalize` returns the error; the caller propagates it as
    an activity failure. The driver invocation is preserved in
    Temporal's failed-activity history (the failure is visible);
    the operator's recovery is "free disk and retry". The
    failure does NOT silently fall back to truncation — that
    would erase the audit signal.
  - **Blob reference is malformed in a consumer.** `Resolve`
    treats anything without the `blob://` prefix as a literal
    string and returns it unchanged. A consumer who reads
    garbage gets garbage back (same as today). A consumer who
    reads a well-formed but unknown hash gets a "blob not
    found" error from the store — surfaced, not swallowed.
  - **Operator deletes `~/.chitin/blobs/` while the orchestrator
    is running.** Future Gets of pre-existing references fail
    with "not found"; future Puts re-create the directory and
    proceed. The orchestrator does not pre-validate the
    directory at startup beyond logging the path, so the
    behavior matches Unix-tool norms (you can delete the disk
    out from under any process).
  - **`Result.OutputRef` is set by a code path other than the
    driver** (e.g. `deliverWorkProduct` at `work_unit.go:289`
    sets `OutputRef = d.PRURL`). PR URLs are small strings —
    inline always. `Externalize` is a no-op for them. The
    delivery shim does NOT need to know about the blob store.
  - **Chain consumer running an older orchestrator build.** The
    `output_ref` field continues to carry a string; if the older
    consumer doesn't know about `blob://` URIs, it surfaces them
    as opaque strings to the operator. The operator can manually
    resolve via the on-disk blob path (`~/.chitin/blobs/<first2>/
    <rest>.blob`). No schema break.

## Composability

  - **Spec 070** (work-unit workflow) — `WorkUnitResult` shape
    is unchanged per FR-008; the externalization is invisible
    above the driver layer.
  - **Spec 094 / 116** (dialectic + internal re-review) — these
    consume `Result.OutputRef`/`Explanation` to inspect driver
    output. They call `Resolve` transparently per FR-007 / FR-009;
    no behavior change beyond reading the full body instead of a
    truncated one.
  - **Spec 113** (PR iteration loop) — unaffected. It reads the
    Copilot review on the resulting PR, not the driver
    transcript.
  - **Spec 118** (silent-drop detector) — strengthened. The
    detector now sees full transcripts instead of failing on a
    `payloadSizeError`-aborted run.
  - **Spec 119** (whole-spec dispatch) — the spec whose load
    surfaced this problem. Spec 121 is the missing piece that
    makes whole-spec dispatch's reporting layer trustworthy:
    today a `failed` run might mean the work failed OR the
    transport failed; post-121, `failed` means the work failed,
    period.
  - **Spec 120** (claudecode-glm driver) — the new T4 local
    driver also routes through `resultFromCommand`; it inherits
    externalization for free.
  - **Future GC spec** — the `blob_written` chain event per
    FR-010 plus the content-addressed naming gives the GC
    follow-up everything it needs (a candidate set + a way to
    detect references that are still live in the chain).
  - **Future multi-host topology** — `S3Store` skeleton per
    FR-012 means a future spec swaps the impl without touching
    driver code.
