// Pure generation logic for backlog entries — no I/O. All functions are
// side-effect-free so tests can exercise them without touching the filesystem.

import type { BacklogEntry } from '../../../apps/temporal-worker/src/grooming/parse-backlog.js';

export interface EntryOptions {
  id: string;
  tier: string;
  status: string;
  role: string;
  file: string;
  blocks: string[];
  estimated_loc: string;
  references_finding: string;
  references_spec: string;
  references_design: string;
  description: string;
}

// Builds the full markdown section for a backlog entry.
// Invariant: the generated heading id (`### \`<id>\``) matches the
// frontmatter `id:` field — both come from opts.id.
export function buildSection(opts: EntryOptions): string {
  const yaml: string[] = [
    `id: ${opts.id}`,
    `tier: ${opts.tier}`,
    `status: ${opts.status}`,
  ];
  if (opts.estimated_loc) yaml.push(`estimated_loc: ${opts.estimated_loc}`);
  yaml.push(`blocks: [${opts.blocks.join(', ')}]`);
  if (opts.file) yaml.push(`file: ${opts.file}`);
  if (opts.references_finding) yaml.push(`references_finding: ${opts.references_finding}`);
  if (opts.references_spec) yaml.push(`references_spec: ${opts.references_spec}`);
  if (opts.references_design) yaml.push(`references_design: ${opts.references_design}`);
  yaml.push(`role: ${opts.role}`);

  const description = opts.description.trim() || '(no description)';
  return `### \`${opts.id}\`\n\n\`\`\`yaml\n${yaml.join('\n')}\n\`\`\`\n\n${description}`;
}

// Inserts `section` at the end of `text` (after the last ### entry's
// content), before any trailing ## section that follows. If no ## section
// follows, appends at end of file.
//
// Invariant: the resulting text begins with `text`'s original prefix up
// to the insertion point, followed by `\n\n---\n\n{section}`, followed
// by any original trailing content.
export function insertEntry(text: string, section: string): string {
  // Find the last ### heading
  const h3Matches = [...text.matchAll(/^### /gm)];
  if (h3Matches.length === 0) {
    return `${text.trimEnd()}\n\n---\n\n${section}\n`;
  }

  const lastH3 = h3Matches[h3Matches.length - 1]!;
  const afterLastH3 = text.slice(lastH3.index!);

  // Find a ## heading that follows the last ### (next major section)
  const nextH2Match = afterLastH3.match(/\n(?=## )/);

  let insertAt: number;
  if (nextH2Match?.index !== undefined) {
    insertAt = lastH3.index! + nextH2Match.index;
  } else {
    insertAt = text.length;
  }

  const before = text.slice(0, insertAt).trimEnd();
  const after = text.slice(insertAt).trimStart();

  if (after) {
    return `${before}\n\n---\n\n${section}\n\n${after}`;
  }
  return `${before}\n\n---\n\n${section}\n`;
}

export function hasDuplicateId(entries: readonly BacklogEntry[], id: string): boolean {
  return entries.some((e) => e.id === id);
}
