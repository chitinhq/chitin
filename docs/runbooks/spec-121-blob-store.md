# Spec 121 Blob Store Runbook

Spec 121 keeps whole-spec driver transcripts out of Temporal activity results.
Short driver outputs remain inline. Outputs larger than 1 MiB are written to a
content-addressed filesystem blob store and the result field carries a
`blob://sha256/<hash>` pointer.

## Location

By default blobs live under:

```sh
~/.chitin/blobs/
```

Override the root with:

```sh
export CHITIN_BLOB_DIR=/var/lib/chitin/blobs
```

The active directory and inline threshold are printed in the orchestrator
startup log line.

## Reading a Blob Manually

A reference:

```text
blob://sha256/abcdef...
```

maps to:

```sh
cat ~/.chitin/blobs/ab/cdef....blob
```

The first two hash characters are the shard directory. The rest of the hash is
the filename, with `.blob` appended.

## Inline Threshold

The threshold is currently compiled as `blob.InlineThreshold`:

```text
1,048,576 bytes
```

To change it, update `go/orchestrator/internal/blob/externalize.go`, rebuild
`chitin-orchestrator`, and restart the worker. Keep the value below Temporal's
2 MiB payload ceiling so the rest of `WorkUnitResult` has headroom.

## Disk Full Or Unwritable

If the blob directory cannot be written, driver invocation fails at the activity
boundary instead of truncating output. Recovery:

1. Free disk or fix permissions on `$CHITIN_BLOB_DIR`.
2. Confirm the orchestrator user can create files there.
3. Retry the failed work unit or scheduler run.

The failed activity preserves the error in Temporal history; it does not
silently lose the transcript.

## Chain Events

Each successful new blob write emits one `blob_written` event with:

```json
{"ref":"blob://sha256/<hash>","size_bytes":123,"sha256":"<hash>"}
```

Repeated writes of identical content do not emit duplicate events because the
content-addressed file already exists.

## Future S3 Path

`internal/blob.S3Store` already satisfies the same `blob.Store` interface and
returns `s3 blob store not implemented`. A future multi-host spec can replace
the filesystem store with live AWS SDK v2 calls without changing driver code.

## Context

Spec 119 changed dispatch from per-task to whole-spec, which surfaced the
oversized transcript transport failure. Spec 118's silent-drop detector benefits
from preserving full driver transcripts instead of truncating them.
