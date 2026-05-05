// Memory-context primitive: when the dispatcher is about to send
// a prompt for entry X, look up prior chain sessions related to
// X (by entry-id substring or file-path overlap) and inject a
// markdown summary of each into the prompt.
//
// Operator framing 2026-05-03 evening: "the next agent can easily
// replay what's happened.. and we can have that replay in our
// memory layer too."
//
// Implementation: shells out to `chitin-kernel chain summarize
// <session_id>` and `chitin-kernel chain related <entry_id>
// --files=<paths>` (both shipped in the Go SDK; see
// internal/replay/). The TS layer just composes the markdown
// blocks into the next prompt.
//
// Performance: each summary call is a Go subprocess (~10-30ms).
// For an entry with 3 related sessions, total overhead is ~50ms
// at dispatch time. Negligible vs the LLM call that follows.

import { spawnSync } from 'node:child_process';

/** A backlog entry hint for relating prior sessions. */
export interface EntryHint {
  id: string;
  /** Files declared in the entry's `file:` field, parsed. */
  filePaths: string[];
}

/**
 * Look up related prior sessions and produce a markdown block
 * suitable for prompt injection. Returns empty string if no
 * related sessions found OR the kernel binary isn't available
 * (graceful no-op — never block dispatch on this).
 *
 * Bounded: up to maxSessions related sessions; each summary
 * capped at ~400 chars by the kernel's Summarize.
 */
export function buildPriorSessionContext(hint: EntryHint, maxSessions = 3): string {
  // Step 1: find related session IDs
  const relatedIds = findRelatedSessions(hint, maxSessions);
  if (relatedIds.length === 0) {
    return '';
  }

  // Step 2: get summary for each
  const summaries: string[] = [];
  for (const sid of relatedIds) {
    const summary = summarizeSession(sid);
    if (summary) summaries.push(summary);
  }
  if (summaries.length === 0) return '';

  // Step 3: compose
  let out = '\n\n---\n\nPRIOR-SESSION MEMORY CONTEXT (for similar work):\n\n';
  for (const s of summaries) {
    out += s + '\n';
  }
  out += '\nUse these summaries as background — what worked, what was denied, what files were touched. Do not assume the prior decisions still apply; the policy may have changed since.\n---\n';
  return out;
}

function findRelatedSessions(hint: EntryHint, maxSessions: number): string[] {
  const args = [
    'chain', 'related',
    '--entry-id=' + hint.id,
    '--max=' + maxSessions.toString(),
  ];
  for (const fp of hint.filePaths) {
    if (fp) args.push('--file=' + fp);
  }
  const result = spawnSync('chitin-kernel', args, {
    encoding: 'utf8',
    timeout: 5000,
  });
  if (result.error || result.status !== 0) {
    // Binary missing or command not yet implemented — graceful no-op
    return [];
  }
  const ids: string[] = [];
  for (const line of result.stdout.split('\n')) {
    const trimmed = line.trim();
    if (trimmed) ids.push(trimmed);
  }
  return ids;
}

function summarizeSession(sessionID: string): string | null {
  const result = spawnSync(
    'chitin-kernel',
    ['chain', 'summarize', '--session=' + sessionID],
    { encoding: 'utf8', timeout: 5000 },
  );
  if (result.error || result.status !== 0) {
    return null;
  }
  return result.stdout.trim();
}
