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
    rawFrontmatter,
    description,
    rawSection: section,
  };
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
