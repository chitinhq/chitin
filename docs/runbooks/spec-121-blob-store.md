# Spec 121 Blob Store Runbook

Spec 121 externalizes large driver stdout/stderr-derived result fields so
whole-spec dispatches do not exceed Temporal's activity-result payload limit.
Small outputs stay inline. Outputs larger than 1 MiB are written to the local
blob store and the result carries a `blob://sha256/<hash>` pointer.

## Location

Default location:

```sh
~/.chitin/blobs/
```

Override:

```sh
export CHITIN_BLOB_DIR=/var/lib/chitin/blobs
```

The sharded path for a ref is:

```text
blob://sha256/abcdef...
~/.chitin/blobs/ab/cdef....blob
```

Manual read:

```sh
cat ~/.chitin/blobs/<first2>/<rest>.blob
```

## Inline Threshold

The orchestrator logs the active threshold and directory on startup:

```text
blob_inline_threshold=1048576 blob_dir=/home/operator/.chitin/blobs
```

The threshold is compiled as `internal/blob.InlineThreshold` and is currently
1,048,576 bytes. Bumping it requires a code change and redeploy. Keep it well
below Temporal's 2 MiB payload ceiling so the rest of `WorkUnitResult` has
headroom.

## Disk Full Or Permission Errors

If the blob directory cannot be written, driver result construction fails and
the activity fails visibly. The orchestrator does not truncate or silently
fall back for oversized bodies because that would erase the audit signal.

Recovery:

1. Free disk or fix permissions for `CHITIN_BLOB_DIR`.
2. Confirm the worker startup log points at the expected blob directory.
3. Retry the failed work unit or re-dispatch the scheduler run.

## Chain Audit

Every new blob write emits one `blob_written` chain event:

```json
{"ref":"blob://sha256/<hash>","size_bytes":3145728,"sha256":"<hash>"}
```

Idempotent re-writes of identical content do not emit duplicate events.

## Related Specs

Spec 119 introduced whole-spec dispatch, which made multi-megabyte driver
transcripts common enough to hit Temporal's payload ceiling. Spec 118's
silent-drop detector benefits from full transcripts because externalized
outputs remain byte-for-byte available instead of being truncated.

## Future S3 Path

`internal/blob.S3Store` now satisfies the same `blob.Store` interface as the
filesystem store, but live S3 calls are intentionally not implemented in this
spec. A future multi-host topology can wire S3-compatible storage without
changing driver result construction.
