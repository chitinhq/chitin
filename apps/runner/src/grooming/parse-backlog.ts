// Parser for docs/swarm-backlog.md.
//
// Each entry has a single YAML frontmatter block in a fenced ```yaml
// region directly under a ### heading, and free-form description prose
// beneath. We extract the heading, the YAML block, and the prose so the
// groomer agent has the same context a human reviewer would.
//
// MVP guarantees:
//   - Order-preserving (so we can rewrite the file with the same layout)
//   - status field is always present (defaults to "ready" if missing)
//   - Unknown YAML keys are preserved as opaque strings (no schema check)

import { readFileSync } from 'node:fs';

export type EntryStatus = 'ready' | 'in_design' | 'needs_human';

export interface BacklogEntry {
  id: string;          // from heading: ### `<id>`
  status: EntryStatus;
  tier?: string;       // T0..T4 if present
  estimatedLoc?: string;
  blocks?: string[];
  file?: string;
  referencesIssue?: string;
  referencesFinding?: string;
  referencesSpec?: string;
  // Phase 1 of swarm-as-software-factory (see
  // docs/design/2026-05-02-swarm-as-software-factory.md §3): the role
  // this entry's worker plays on the assembly line. Absent = generic
  // programmer (the slice-7b dispatcher's pre-Phase-1 default).
  // Vocabulary matches RoleSchema in @chitin/contracts.
  role?: string;
  // Test scenarios extracted from the description's `Tests:` /
  // `Test plan:` / `Test cases:` section, when present. The prompt
  // builder treats a non-empty test_plan as acceptance criteria the
  // PR must satisfy — driven by 2026-05-06 /land batch finding that
  // entries naming explicit test scenarios were shipping with zero
  // tests because the worker treated them as commentary.
  test_plan?: string[];
  rawFrontmatter: string;  // the original ```yaml block, preserved verbatim
  description: string;     // prose below the frontmatter, before next ### or ##
  rawSection: string;      // entire ### section for in-place replacement
}

export function parseBacklog(path: string): BacklogEntry[] {
  const text = readFileSync(path, 'utf8');
  const sections = splitH3Sections(text);
  const entries: BacklogEntry[] = [];
  for (const section of sections) {
    const entry = parseSection(section);
    if (entry) entries.push(entry);
  }
  return entries;
}

/**
 * Returns true if any of `entry.file`'s comma-separated paths matches
 * a path in `debtFiles` (typically the `file:` field of every
 * debt-ledger entry with `status: open` or `status: claimed`). Used
 * by the GROOM stage to surface "this entry touches a known-debt
 * file" — a signal the groomer should bump the tier or call out
 * cross-cutting implications when sizing.
 *
 * Matching is exact-equality on the trimmed path strings. The
 * caller is responsible for resolving `cross-cutting` debt entries
 * (which match no specific file) — a future revision could special-
 * case that token, but for now those don't fire here.
 *
 * Loaded via `python/analysis/debt.load_ledger` on the analysis
 * side; the typescript caller is expected to pre-load the file
 * list (e.g., via a small CLI shim or by re-parsing the markdown
 * directly) and pass it in.
 */
export function crossesDebtLedger(entry: BacklogEntry, debtFiles: string[]): boolean {
  if (!entry.file || debtFiles.length === 0) return false;
  const entryFiles = entry.file.split(',').map((f) => f.trim()).filter(Boolean);
  if (entryFiles.length === 0) return false;
  const debtSet = new Set(debtFiles.map((f) => f.trim()).filter(Boolean));
  return entryFiles.some((f) => debtSet.has(f));
}

export const __test__ = {
  crossesDebtLedger,
  parseTestPlan,
};

// Splits at ### but keeps surrounding ## headings out — only ### sections
// are entries. We stop a section at the next ### or ##.
function splitH3Sections(text: string): string[] {
  const lines = text.split('\n');
  const out: string[] = [];
  let buf: string[] = [];
  let inEntry = false;
  for (const line of lines) {
    if (line.startsWith('### ')) {
      if (inEntry) out.push(buf.join('\n'));
      buf = [line];
      inEntry = true;
    } else if (line.startsWith('## ')) {
      if (inEntry) out.push(buf.join('\n'));
      buf = [];
      inEntry = false;
    } else if (inEntry) {
      buf.push(line);
    }
  }
  if (inEntry) out.push(buf.join('\n'));
  return out;
}

function parseSection(section: string): BacklogEntry | null {
  const headingMatch = section.match(/^### `([^`]+)`/);
  if (!headingMatch) return null;
  const id = headingMatch[1];

  const yamlMatch = section.match(/```yaml\n([\s\S]*?)\n```/);
  if (!yamlMatch) return null;
  const rawFrontmatter = yamlMatch[1];

  const fields = parseSimpleYaml(rawFrontmatter);
  const status = (fields.status ?? 'ready') as EntryStatus;

  // Description: everything after the closing ``` of the yaml block,
  // up to the end of the section. Trim leading separators.
  const afterYaml = section.slice((yamlMatch.index ?? 0) + yamlMatch[0].length);
  const description = afterYaml.replace(/^\s*---\s*\n/, '').trim();

  return {
    id,
    status,
    tier: fields.tier,
    estimatedLoc: fields.estimated_loc,
    blocks: parseList(fields.blocks),
    file: fields.file,
    referencesIssue: fields.references_issue,
    referencesFinding: fields.references_finding,
    referencesSpec: fields.references_spec,
    role: fields.role,
    test_plan: parseTestPlan(description),
    rawFrontmatter,
    description,
    rawSection: section,
  };
}

// Pulls explicit test scenarios out of the description body. We look
// for a line whose leading content is `Tests:`, `Test plan:`, or
// `Test cases:` (case-insensitive, optionally indented or bulleted),
// then collect the remainder of that line plus any subsequent bullet
// items as individual scenarios. A blank line ends the block.
//
// Why look here at all: backlog entries have been shipping with named
// test scenarios in prose ("Tests: 3 fixture entries (clean, missing,
// partial)") that swarm workers were treating as commentary. Surfacing
// them as a structured list lets the prompt builder echo them as
// acceptance criteria.
export function parseTestPlan(description: string): string[] | undefined {
  const lines = description.split('\n');
  let startIdx = -1;
  let inlineRest = '';
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(/^\s*(?:[-*]|\d+\.)?\s*(?:Tests|Test plan|Test cases)\s*:\s*(.*)$/i);
    if (m) {
      startIdx = i;
      inlineRest = m[1];
      break;
    }
  }
  if (startIdx === -1) return undefined;

  const items: string[] = [];
  if (inlineRest.trim()) {
    for (const part of inlineRest.split(/[;]/)) {
      const t = part.trim().replace(/[.,]+$/, '');
      if (t) items.push(t);
    }
  }
  for (let i = startIdx + 1; i < lines.length; i++) {
    const line = lines[i];
    const bm = line.match(/^\s*(?:[-*]|\d+\.)\s+(.+)$/);
    if (bm) {
      items.push(bm[1].trim());
      continue;
    }
    if (line.trim() === '') {
      if (items.length > 0) break;
      continue;
    }
    // Indented continuation of the previous bullet — fold in.
    if (/^\s+\S/.test(line) && items.length > 0) {
      items[items.length - 1] += ' ' + line.trim();
      continue;
    }
    break;
  }
  return items.length > 0 ? items : undefined;
}

// Minimal YAML parser sufficient for our flat key:value-and-list frontmatter.
// Won't handle nested objects (we don't use them) and treats `[a, b]`-style
// inline lists explicitly. Multi-line blocks are out of scope.
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

function parseList(field?: string): string[] | undefined {
  if (!field) return undefined;
  const m = field.match(/^\[(.*)\]$/);
  if (!m) return undefined;
  return m[1]
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}
