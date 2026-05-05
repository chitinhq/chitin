# comment-responder

This folder contains the dispatch helpers and prompt builders for the
`comment-responder` agent role plus the **mutable status comment**
helper used by the review-graph tiers (R0–R4) to maintain a single,
edited-in-place comment per PR rather than appending a fresh comment
per pass.

## Mutable status comment

The review-graph used to post a new comment for every reviewer tier.
A PR walking R0 → R1 → R2 → R3 ended up with a stack of overlapping
comments — all relevant, none authoritative — forcing operators to
scroll for the latest verdict and downstream tooling to scrape prose.

`mutable-status.ts` replaces that pattern. Each tier calls
`upsert(prNumber, repo, body, verdict)`; the first call creates the
comment and every subsequent call edits it in place.

### Body marker

The first line of the comment body is a fixed magic string used to
identify the chitin-owned comment among any number of human / bot
comments on the PR:

```
<!-- chitin-status-comment v1 -->
```

`findByMarker` looks for this prefix via
`gh api repos/<repo>/issues/<pr>/comments` (with `--paginate` for PRs
with more than 100 comments) and returns the matching comment's id +
body, or `{comment_id: null, body: ''}` when no chitin comment exists
yet. The `v1` suffix lets us version the body schema without changing
the search predicate.

### Verdict marker

The current tier's verdict lives in a structured HTML comment embedded
in the body and parsed by `parseVerdict`:

```
<!-- chitin:verdict tier=R2 status=approve workflow_id=<id> ts=2026-05-05T12:00:00Z -->
```

Schema (zod-validated):

| Field         | Values                                          |
|---------------|-------------------------------------------------|
| `tier`        | `R0` \| `R1` \| `R2` \| `R3` \| `R4`            |
| `status`      | `approve` \| `changes_requested` \| `pending`   |
| `workflow_id` | non-empty string (the dispatching workflow id)  |
| `ts`          | ISO-8601 datetime                               |

A body may contain markers from multiple tiers (one per pass). `parseVerdict`
returns the latest by `ts`, with tier index (R4 > R3 > … > R0) as the
tie-breaker for same-`ts` collisions. Malformed or missing markers
return `null` — callers (e.g. `gatekeeper.ts`) treat `null` as **no
verdict**, never as an approve. Fail-closed by design.

### Backward compatibility

PRs touched before this helper landed retain their append-style
comment chain — there is no migration. The first tier that runs after
the helper is wired in creates a new mutable comment; older comments
remain visible as historical record. Operators expecting forensic
"R1 said X, then R2 said Y" history in the PR thread should consult
the chain audit log (`.chitin/events-<run_id>.jsonl`) instead — the
GitHub thread shows the current verdict only.
