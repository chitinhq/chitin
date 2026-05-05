// Mutable status comment helper for the review-graph tiers.
//
// Goal: replace the append-per-tier comment chain with one mutable
// "chitin status comment" per PR, edited in place, carrying the
// current tier's verdict in a hidden HTML marker so downstream tooling
// (gatekeeper, pr-event-ingester, future operator commands) can read
// the verdict without scraping prose.
//
// Conventions:
//
//   1. Body marker — the FIRST line of the comment body is a fixed
//      magic string:
//
//        <!-- chitin-status-comment v1 -->
//
//      `findByMarker` searches PR comments for this prefix to identify
//      the chitin-owned comment among any number of human / bot
//      comments. The `v1` suffix lets us version the body schema
//      without changing the search predicate (parse the `vN` and
//      branch on it later).
//
//   2. Verdict marker — a structured HTML comment embedded in the
//      body, parsed by `parseVerdict`:
//
//        <!-- chitin:verdict tier=R2 status=approve workflow_id=foo ts=2026-05-05T12:00:00Z -->
//
//      Multiple tiers may write multiple verdict markers into the same
//      body (R0, R1, R2, ...) — `parseVerdict` returns the LATEST one
//      by ts, with tier index as the tie-breaker. Mismatch /
//      malformed: returns null and logs (caller treats as "no
//      verdict"). Fail-closed semantics — never mistake a malformed
//      marker for an approve.
//
// Backward compatibility: PRs that already have an append-style
// comment chain are unaffected. The first time this helper runs on
// such a PR, `findByMarker` returns null and `upsert` creates a NEW
// comment — older comments remain as historical record. No migration.

import { execFileSync } from 'node:child_process';
import { z } from 'zod';

// ─── Marker constants ────────────────────────────────────────────────────

/** Fixed first-line magic string identifying the chitin status comment. */
export const STATUS_BODY_MARKER = '<!-- chitin-status-comment v1 -->';

/** Regex for parsing verdict markers out of a comment body. */
const VERDICT_MARKER_RE =
  /<!--\s*chitin:verdict\s+([^>]+?)\s*-->/g;

// ─── Verdict schema (zod) ────────────────────────────────────────────────
//
// Inlined here rather than under libs/contracts/ to keep this entry's
// scope to apps/temporal-worker/src/comment-responder/* — the schema
// is small enough that one consumer (this helper) is the right home
// for now. Promote to libs/contracts/ when a second consumer (e.g.
// gatekeeper) lands a direct read.

export const VerdictTierSchema = z.enum(['R0', 'R1', 'R2', 'R3', 'R4']);
export type VerdictTier = z.infer<typeof VerdictTierSchema>;

export const VerdictStatusSchema = z.enum(['approve', 'changes_requested', 'pending']);
export type VerdictStatus = z.infer<typeof VerdictStatusSchema>;

export const VerdictMetaSchema = z.object({
  tier: VerdictTierSchema,
  status: VerdictStatusSchema,
  workflow_id: z.string().min(1),
  ts: z.string().datetime(),
});
export type VerdictMeta = z.infer<typeof VerdictMetaSchema>;

// ─── gh runner (injectable for tests) ────────────────────────────────────

export interface GhRunner {
  /** Run `gh <args>` and return stdout. Throw on non-zero exit. */
  (args: string[]): string;
}

const defaultGhRunner: GhRunner = (args) =>
  execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 });

// ─── PR comment shape (subset of what `gh api` returns) ──────────────────

interface PrIssueComment {
  /** Numeric comment id (used for the PATCH endpoint). */
  id: number;
  /** Markdown body. */
  body: string;
}

// ─── Public API ──────────────────────────────────────────────────────────

export interface MutableStatusReadResult {
  /** Numeric comment id, or null if no chitin status comment exists. */
  comment_id: number | null;
  /** Full body of the chitin status comment, or empty string when absent. */
  body: string;
  /** Latest verdict parsed from the body, or null when missing/malformed. */
  verdict: VerdictMeta | null;
}

export interface FindByMarkerResult {
  comment_id: number | null;
  body: string;
}

export interface UpsertInput {
  pr_number: number;
  repo: string;
  /** Pre-rendered comment body WITHOUT the body/verdict markers — this
   *  helper prepends the body marker and appends the verdict marker so
   *  callers can't accidentally drift the schema. */
  body: string;
  verdict: VerdictMeta;
  /** Optional logger. Defaults to console.warn for parse-failures. */
  log?: (line: string) => void;
  /** Optional gh injection (tests). */
  gh?: GhRunner;
}

export interface UpsertResult {
  /** 'created' on first tier, 'edited' on every subsequent tier. */
  action: 'created' | 'edited';
  comment_id: number;
}

/**
 * Render the canonical comment body: body marker on the first line,
 * caller-supplied body in the middle, verdict marker at the end.
 *
 * Public so review-graph callers can preview / log the rendered body
 * without making a `gh` call.
 */
export function renderStatusBody(body: string, verdict: VerdictMeta): string {
  const verdictLine =
    `<!-- chitin:verdict tier=${verdict.tier} status=${verdict.status} ` +
    `workflow_id=${verdict.workflow_id} ts=${verdict.ts} -->`;
  // Trim trailing whitespace on caller body so we don't accumulate
  // blank lines across tier edits.
  const trimmed = body.replace(/\s+$/, '');
  return `${STATUS_BODY_MARKER}\n\n${trimmed}\n\n${verdictLine}\n`;
}

/**
 * Parse the LATEST verdict marker out of a comment body.
 *
 * Strategy:
 *   1. Scan all `<!-- chitin:verdict ... -->` blocks.
 *   2. For each, parse the `key=value` pairs (whitespace-separated,
 *      no quotes — values must not contain whitespace; we own the
 *      writer side, so this is fine).
 *   3. Validate via zod. Malformed entries are logged + skipped.
 *   4. Return the entry with the largest `ts` (string compare on ISO-8601
 *      ts is correct because the format is lexically sortable). Tier
 *      index breaks ties — R4 > R3 > R2 > R1 > R0 — so a same-ts
 *      conflict resolves to the higher tier (closer to merge).
 *
 * Returns null when:
 *   - no markers in body
 *   - every marker fails schema validation
 *
 * Fail-closed: caller treats null as "no verdict", never as approve.
 */
export function parseVerdict(
  body: string,
  log: (line: string) => void = (l) => console.warn(l),
): VerdictMeta | null {
  const markers = body.match(VERDICT_MARKER_RE);
  if (!markers || markers.length === 0) {
    return null;
  }

  const tierOrder: Record<VerdictTier, number> = {
    R0: 0,
    R1: 1,
    R2: 2,
    R3: 3,
    R4: 4,
  };

  const parsed: VerdictMeta[] = [];
  for (const marker of markers) {
    // Strip the surrounding `<!-- chitin:verdict ` and ` -->`.
    const inner = marker.replace(/^<!--\s*chitin:verdict\s+/, '').replace(/\s*-->\s*$/, '');
    const fields: Record<string, string> = {};
    for (const tok of inner.split(/\s+/)) {
      const eq = tok.indexOf('=');
      if (eq <= 0) continue;
      fields[tok.slice(0, eq)] = tok.slice(eq + 1);
    }
    const result = VerdictMetaSchema.safeParse(fields);
    if (!result.success) {
      log(JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'mutable-status',
        msg: 'malformed verdict marker; skipping',
        marker,
        issues: result.error.issues.map((i) => ({ path: i.path, message: i.message })),
      }));
      continue;
    }
    parsed.push(result.data);
  }

  if (parsed.length === 0) {
    return null;
  }

  parsed.sort((a, b) => {
    if (a.ts !== b.ts) return a.ts < b.ts ? 1 : -1;
    return tierOrder[b.tier] - tierOrder[a.tier];
  });
  return parsed[0];
}

/**
 * Locate the chitin status comment on a PR. Returns the comment_id +
 * body, or {comment_id: null, body: ''} when none exists.
 *
 * Searches via `gh api repos/<repo>/issues/<pr>/comments`. We use the
 * issue-comments endpoint (not pulls/comments) because top-level PR
 * comments live on the issue side; pulls/comments is for inline
 * review comments, which we don't want for the status comment.
 */
export function findByMarker(
  pr_number: number,
  repo: string,
  marker: string = STATUS_BODY_MARKER,
  gh: GhRunner = defaultGhRunner,
): FindByMarkerResult {
  // --paginate so we don't miss the chitin comment on PRs with >100
  // comments. `gh api --paginate` concatenates the JSON arrays into
  // one valid array — no jq needed.
  const stdout = gh([
    'api',
    '--paginate',
    `repos/${repo}/issues/${pr_number}/comments`,
  ]);
  let comments: PrIssueComment[];
  try {
    comments = JSON.parse(stdout) as PrIssueComment[];
  } catch (err) {
    throw new Error(
      `mutable-status.findByMarker: gh api returned non-JSON (pr=${pr_number}): ${
        err instanceof Error ? err.message : String(err)
      }`,
    );
  }
  if (!Array.isArray(comments)) {
    throw new Error(
      `mutable-status.findByMarker: gh api returned non-array for pr=${pr_number}`,
    );
  }
  for (const c of comments) {
    if (typeof c.body === 'string' && c.body.startsWith(marker)) {
      return { comment_id: c.id, body: c.body };
    }
  }
  return { comment_id: null, body: '' };
}

/**
 * Read the chitin status comment from a PR and parse its latest
 * verdict in one call. Convenience wrapper around findByMarker +
 * parseVerdict — the gatekeeper's "what's the current verdict on this
 * PR?" path.
 */
export function read(
  pr_number: number,
  repo: string,
  log: (line: string) => void = (l) => console.warn(l),
  gh: GhRunner = defaultGhRunner,
): MutableStatusReadResult {
  const found = findByMarker(pr_number, repo, STATUS_BODY_MARKER, gh);
  if (found.comment_id === null) {
    return { comment_id: null, body: '', verdict: null };
  }
  return {
    comment_id: found.comment_id,
    body: found.body,
    verdict: parseVerdict(found.body, log),
  };
}

/**
 * Create or edit the chitin status comment on a PR.
 *
 *   - First-tier call (no existing comment): POST a new comment via
 *     `gh pr comment`.
 *   - Subsequent tiers (existing comment): PATCH the comment in place
 *     via `gh api -X PATCH repos/<repo>/issues/comments/<id>`.
 *
 * Returns the action taken + the comment id.
 */
export function upsert(input: UpsertInput): UpsertResult {
  // Validate verdict shape upfront — fail loudly here rather than
  // letting a malformed verdict reach the marker writer and pollute
  // future reads.
  VerdictMetaSchema.parse(input.verdict);

  const gh = input.gh ?? defaultGhRunner;
  const log = input.log ?? ((l: string) => console.warn(l));

  const renderedBody = renderStatusBody(input.body, input.verdict);
  const existing = findByMarker(input.pr_number, input.repo, STATUS_BODY_MARKER, gh);

  if (existing.comment_id === null) {
    // Create. Use `gh pr comment` with --body-file via stdin to avoid
    // shell escaping pitfalls on multi-line bodies. gh accepts -F - to
    // read from stdin? Actually --body-file=-. Use --body and pass via
    // a temp file to be safe.
    //
    // gh pr comment <pr> --repo <repo> --body <body>
    // Output ends with a comment URL — parse the trailing /<id> off it
    // for the return value. If parse fails, we re-find via findByMarker
    // so the next tier's edit still works.
    const stdout = gh([
      'pr', 'comment', String(input.pr_number),
      '--repo', input.repo,
      '--body', renderedBody,
    ]);
    const idMatch = stdout.match(/#issuecomment-(\d+)/);
    if (idMatch) {
      return { action: 'created', comment_id: Number(idMatch[1]) };
    }
    // Fallback — re-fetch to recover the id.
    const refound = findByMarker(input.pr_number, input.repo, STATUS_BODY_MARKER, gh);
    if (refound.comment_id === null) {
      log(JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'mutable-status',
        msg: 'created comment but could not recover id from gh stdout or re-find',
        pr_number: input.pr_number,
      }));
      throw new Error(
        `mutable-status.upsert: created comment on pr=${input.pr_number} but failed to recover id`,
      );
    }
    return { action: 'created', comment_id: refound.comment_id };
  }

  // Edit. PATCH the issue comment by id.
  gh([
    'api',
    '-X', 'PATCH',
    `repos/${input.repo}/issues/comments/${existing.comment_id}`,
    '-f', `body=${renderedBody}`,
  ]);
  return { action: 'edited', comment_id: existing.comment_id };
}
