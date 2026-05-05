// Structural linter: every entry in docs/swarm-backlog.md parses
// cleanly through the dispatcher's existing parseBacklog function,
// AND its frontmatter passes the schema constraints the dispatcher
// implicitly relies on:
//
//   - heading id (` ### `<id>` `) matches `id:` in the YAML frontmatter
//   - `tier:` (if present) is one of T0..T5
//   - `role:` (if present) is in @chitin/contracts' RoleSchema
//   - `status:` (if present) is in 'ready' | 'in_design' | 'needs_human'
//     (the parser's EntryStatus union)
//   - no `blockedBy:` field — the parser only reads `blocks:`, so a
//     blockedBy field is a silent footgun (same shape as PR #200's
//     review-time finding)
//
// All checks are pure-function-driven so the test suite can pin them
// without touching the actual swarm-backlog.md.

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// ── Pure logic ──────────────────────────────────────────────────────

const TIER_PATTERN = /^T[0-5]$/;

// Status vocabulary actually used in docs/swarm-backlog.md as of
// 2026-05-03. Broader than the parser's `EntryStatus` type
// declaration (which only lists ready/in_design/needs_human) —
// real entries use lifecycle states like `shipped`, `completed`,
// `blocked`, `decomposed`, `partial`. The parser passes status
// through as a string regardless; the typing in
// apps/runner/src/grooming/parse-backlog.ts is
// historical and should be widened in a follow-up to match.
//
// The dispatcher only DISPATCHES entries with `status: ready`; the
// other values are bookkeeping the parser tolerates. The linter
// matches that tolerance.
const VALID_STATUSES = new Set([
  'ready',
  'in_design',
  'needs_human',
  'blocked',
  'in_flight',
  'completed',
  'shipped',
  'partial',
  'decomposed',
]);

export interface RawEntry {
  /** Heading id (text inside `### \`<here>\``). */
  headingId: string;
  /** YAML frontmatter body (between ```yaml and ```). */
  rawFrontmatter: string;
  /** Parsed flat key→value map from the YAML. */
  fields: Record<string, string>;
  /** Approximate line offset in the source for error messages
   *  (1-indexed, points at the heading line). */
  line: number;
}

export interface EntryError {
  /** Heading id (or '<unparseable>' if not extractable). */
  entry: string;
  line: number;
  /** What rule was violated. */
  rule:
    | 'missing-yaml-block'
    | 'heading-id-frontmatter-id-mismatch'
    | 'invalid-tier'
    | 'invalid-status'
    | 'invalid-role'
    | 'forbidden-blockedBy'
    | 'malformed-blocks';
  message: string;
}

/**
 * Pure: lint a list of raw parsed entries against the schema rules.
 *
 * Knuth-style invariant: every error is produced by exactly one rule
 * checker; rule checkers don't mutate inputs and are independent of
 * order. Output errors are sorted by (entry, rule) for deterministic
 * CI logs.
 */
export function lintRawEntries(
  entries: readonly RawEntry[],
  validRoles: ReadonlySet<string>,
): EntryError[] {
  const errors: EntryError[] = [];
  for (const entry of entries) {
    errors.push(...checkEntry(entry, validRoles));
  }
  errors.sort((a, b) =>
    a.entry !== b.entry ? a.entry.localeCompare(b.entry) : a.rule.localeCompare(b.rule),
  );
  return errors;
}

function checkEntry(entry: RawEntry, validRoles: ReadonlySet<string>): EntryError[] {
  const errs: EntryError[] = [];

  // Rule: heading id must equal frontmatter id (if frontmatter has id).
  // Rule from PR #200 review — a mismatch breaks the dispatcher's
  // entry lookup since it indexes by heading id but humans search by
  // frontmatter id.
  const frontmatterId = entry.fields.id;
  if (frontmatterId !== undefined && frontmatterId !== entry.headingId) {
    errs.push({
      entry: entry.headingId,
      line: entry.line,
      rule: 'heading-id-frontmatter-id-mismatch',
      message:
        `Heading \`${entry.headingId}\` does not match frontmatter \`id: ${frontmatterId}\`. ` +
        `Align them — the parser indexes by the heading id; the frontmatter id is for humans.`,
    });
  }

  // Rule: tier (if present) must be T0..T5 — UNLESS the entry is
  // not yet groomed (status=in_design or absent), in which case
  // `TBD` is a recognized sentinel for "to be decided when this
  // entry is promoted to ready". The dispatcher only acts on
  // status=ready entries, so a TBD tier on an unready entry is
  // never reached at runtime.
  const status = entry.fields.status;
  const isUngroomed = status === 'in_design' || status === undefined;
  if (entry.fields.tier !== undefined) {
    const tierOk = TIER_PATTERN.test(entry.fields.tier)
      || (isUngroomed && entry.fields.tier === 'TBD');
    if (!tierOk) {
      errs.push({
        entry: entry.headingId,
        line: entry.line,
        rule: 'invalid-tier',
        message:
          `tier: ${JSON.stringify(entry.fields.tier)} is not one of T0..T5. ` +
          `Use T0/T1/T2/T3/T4 for dispatchable tiers, T5 for human-only. ` +
          `(\`TBD\` is allowed only on \`status: in_design\` entries.)`,
      });
    }
  }

  // Rule: status (if present) must be in EntryStatus union.
  if (entry.fields.status !== undefined && !VALID_STATUSES.has(entry.fields.status)) {
    errs.push({
      entry: entry.headingId,
      line: entry.line,
      rule: 'invalid-status',
      message:
        `status: ${JSON.stringify(entry.fields.status)} is not one of ` +
        `${[...VALID_STATUSES].join(' | ')}.`,
    });
  }

  // Rule: role (if present) must be in RoleSchema.
  if (entry.fields.role !== undefined && !validRoles.has(entry.fields.role)) {
    errs.push({
      entry: entry.headingId,
      line: entry.line,
      rule: 'invalid-role',
      message:
        `role: ${JSON.stringify(entry.fields.role)} is not in RoleSchema. ` +
        `Valid roles: ${[...validRoles].sort().join(', ')}.`,
    });
  }

  // Rule: NO blockedBy field. The parser only reads `blocks:`; using
  // blockedBy is a silent no-op the dispatcher won't honor.
  if (entry.fields.blockedBy !== undefined || entry.fields.blocked_by !== undefined) {
    errs.push({
      entry: entry.headingId,
      line: entry.line,
      rule: 'forbidden-blockedBy',
      message:
        `Found \`blockedBy\` field — the parser only reads \`blocks:\`. ` +
        `Express the dependency from the parent's frame (the entry that ` +
        `BLOCKS this one declares \`blocks: [<this-id>]\`).`,
    });
  }

  // Rule: blocks (if present) must be a parseable YAML inline array.
  if (entry.fields.blocks !== undefined) {
    const v = entry.fields.blocks.trim();
    // Accept '[]' (empty), '[a, b]', or absence.
    if (v && !/^\[.*\]$/.test(v)) {
      errs.push({
        entry: entry.headingId,
        line: entry.line,
        rule: 'malformed-blocks',
        message:
          `blocks: ${JSON.stringify(entry.fields.blocks)} is not a YAML inline ` +
          `array. Use \`blocks: []\` or \`blocks: [<id>, <id>]\`.`,
      });
    }
  }

  return errs;
}

/**
 * Parse a backlog markdown file's raw entries (heading + YAML)
 * directly — without the dispatcher's parseBacklog, which silently
 * discards malformed sections. The linter wants the malformed ones.
 */
export function extractRawEntries(text: string): RawEntry[] {
  const lines = text.split('\n');
  const out: RawEntry[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? '';
    if (line.startsWith('### ')) {
      const headingMatch = line.match(/^### `([^`]+)`/);
      const headingId = headingMatch ? headingMatch[1] : '<unparseable-heading>';
      const headingLineNum = i + 1;
      // Find the next yaml fence
      let j = i + 1;
      let yamlStart = -1;
      while (j < lines.length && !(lines[j] ?? '').startsWith('### ') && !(lines[j] ?? '').startsWith('## ')) {
        if ((lines[j] ?? '').trim() === '```yaml') {
          yamlStart = j + 1;
          break;
        }
        j += 1;
      }
      let rawFrontmatter = '';
      let fields: Record<string, string> = {};
      if (yamlStart >= 0) {
        let yamlEnd = yamlStart;
        while (yamlEnd < lines.length && (lines[yamlEnd] ?? '').trim() !== '```') {
          yamlEnd += 1;
        }
        rawFrontmatter = lines.slice(yamlStart, yamlEnd).join('\n');
        fields = parseSimpleYaml(rawFrontmatter);
      }
      out.push({ headingId, rawFrontmatter, fields, line: headingLineNum });
      // Advance past this section
      while (i < lines.length - 1 && !(lines[i + 1] ?? '').startsWith('### ') && !(lines[i + 1] ?? '').startsWith('## ')) {
        i += 1;
      }
    }
    i += 1;
  }
  return out;
}

// Minimal YAML parser — same shape as parse-backlog.ts's. Reproduced
// here so the linter doesn't import the worker-side parser (which
// pulls in node:fs eagerly via readFileSync at the call-site, and
// keeps the linter's transitive dep set small).
function parseSimpleYaml(text: string): Record<string, string> {
  const fields: Record<string, string> = {};
  for (const rawLine of text.split('\n')) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    const colonIdx = line.indexOf(':');
    if (colonIdx <= 0) continue;
    const key = line.slice(0, colonIdx).trim();
    let val = line.slice(colonIdx + 1).trim();
    if (val.startsWith('"') && val.endsWith('"')) val = val.slice(1, -1);
    fields[key] = val;
  }
  return fields;
}

// ── I/O wrappers ───────────────────────────────────────────────────

export async function loadValidRoles(): Promise<Set<string>> {
  const mod = await import('@chitin/contracts');
  const schema = (mod as { RoleSchema?: unknown }).RoleSchema;
  if (!schema || typeof schema !== 'object') {
    throw new Error('@chitin/contracts did not export a RoleSchema');
  }
  const opts = (schema as { options?: unknown; values?: unknown }).options
    ?? (schema as { values?: unknown }).values;
  if (!Array.isArray(opts)) {
    throw new Error('RoleSchema.options is not an array (zod shape changed?)');
  }
  return new Set(opts.filter((v): v is string => typeof v === 'string'));
}

// ── main entrypoint ────────────────────────────────────────────────

export interface LintResult {
  ok: boolean;
  errors: EntryError[];
  entriesScanned: number;
}

export async function lintBacklogEntryShape(opts: {
  backlogPath: string;
}): Promise<LintResult> {
  const text = readFileSync(opts.backlogPath, 'utf8');
  const entries = extractRawEntries(text);
  const validRoles = await loadValidRoles();
  const errors = lintRawEntries(entries, validRoles);
  return {
    ok: errors.length === 0,
    errors,
    entriesScanned: entries.length,
  };
}

async function main(): Promise<void> {
  const root = process.cwd();
  const backlogPath = resolve(root, 'docs/swarm-backlog.md');
  const result = await lintBacklogEntryShape({ backlogPath });

  if (result.errors.length > 0) {
    console.error(
      `backlog-entry-shape: ${result.errors.length} error(s) ` +
      `across ${result.entriesScanned} scanned entries:`,
    );
    for (const err of result.errors) {
      console.error(`  ${err.entry} (line ${err.line}): [${err.rule}]`);
      console.error(`    ${err.message}`);
    }
  } else {
    console.error(
      `backlog-entry-shape: ok (${result.entriesScanned} entries pass all rules)`,
    );
  }

  process.exit(result.ok ? 0 : 1);
}

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error('backlog-entry-shape: fatal:', err instanceof Error ? err.message : err);
    process.exit(1);
  });
}
