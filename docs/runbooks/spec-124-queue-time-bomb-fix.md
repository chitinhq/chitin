# Spec 124 Queue Time-Bomb Fix

## Pattern

A time-bomb test combines a frozen fixture anchor with production code that
reads `time.Now()` directly. The fixture is correct only near the day it was
authored; later wall-clock drift can cross age thresholds and make a different
rule win.

Spec 124 fixed that shape in `chitin-orchestrator queue`: the test fixture uses
`queueTestNow`, while `runQueue` previously read wall-clock time internally.
Once wall-clock drift crossed 24 hours, the stale rule could shadow the
conflict rule for PR #9008.

## Seam Idiom

Use a package-internal helper that accepts `now time.Time`, and keep the public
entry point as the default-value wrapper:

```go
func runQueue(...) int {
	return runQueueWithNow(..., time.Now().UTC(), ...)
}

func runQueueWithNow(..., now time.Time, ...) int {
	// all downstream age windows, filters, and formatters use now
}
```

The injected value must flow through the whole request path. Do not call
`time.Now()` again after entering the helper, or the test is only partly
deterministic.

## Future Audit Candidates

- `go/orchestrator/loop/`: queue and scheduling tests use fixed timestamps;
  audit production loop code before adding threshold-based assertions.
- `go/orchestrator/schedules/`: dispatch and lease windows are naturally
  time-sensitive; add seams before testing frozen schedule anchors.
- `go/orchestrator/activities/`: delivery, review, and telemetry activities
  stamp timestamps; frozen-fixture tests should inject the activity clock.
- `go/orchestrator/internal/queue/format_table.go` and
  `go/orchestrator/internal/queue/format_md.go`: both already accept `now` and
  only fall back to wall-clock when callers pass zero.

## Checklist

Does this spec's tests use a frozen `now` while the production code reads
`time.Now()` directly? If yes, add a seam.
