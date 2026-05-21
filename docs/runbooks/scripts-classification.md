# Scripts Classification

`scripts/` is a mixed-surface directory, so every maintained runnable file in
it must be classified before merge. `scripts/MANIFEST.yaml` is the chokepoint:
if a file is not covered there, `scripts/check-scripts-manifest.sh` fails CI.

## Categories

- `ci`: checks, smoke tests, and install flows whose important regression path
  runs in CI or other automated validation. These scripts may also be run by an
  operator, but breaking them should be caught before runtime.
- `operator`: ad-hoc human tools. They are useful, but their failure is
  recoverable by the operator rerunning or repairing them locally.
- `migration`: one-shot upgrade or backfill helpers. They must carry
  `added_on` and `expires_on`; after 90 days they should either be deleted or
  reclassified if they turned into a durable runtime path.
- `runtime-critical`: scripts on timers, cron, or the swarm happy path where
  failure directly disrupts autonomous operation or a core invariant.

## Manifest rules

- Add one `entries` record per maintained file under `scripts/`.
- Every entry needs `path`, `category`, and `purpose`.
- `runtime-critical` entries must include either `tested_by` or `port_ticket`.
- `migration` entries must include `added_on` and `expires_on`.
- Use `exclude_patterns` only for support assets that live under `scripts/` but
  are not part of the maintained runnable surface, such as test fixtures,
  stubs, or prompt documents.

## When To Add

- Add a manifest entry in the same change that introduces a new file under
  `scripts/`.
- If the file is a runtime path, land the test reference in the same change or
  file the followup ticket before merge.
- If the file is a migration, set the 90-day TTL immediately. Do not leave it
  open-ended.

## When To Delete

- Delete the manifest entry in the same change that removes the script.
- Remove an `exclude_patterns` entry once the excluded support asset no longer
  exists.
- Delete expired migrations instead of extending them by habit; only reclassify
  them if they demonstrably became a durable operational path.

## Review heuristic

- Start from invocation surface, not implementation detail.
- If a timer, cron entry, or shared SDLC flow calls the script, assume
  `runtime-critical` until proven otherwise.
- If the main value is catching drift before merge, it is probably `ci`.
- If the script exists to help a person inspect, bootstrap, or repair the
  system manually, it is probably `operator`.
