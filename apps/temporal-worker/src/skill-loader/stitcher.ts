// Skill-folder dispatcher-stitcher.
//
// Reads a `SKILL.md` from a skill folder + any files SKILL.md
// references, substitutes `{{var}}` template tokens with caller-
// provided values, and returns the assembled prompt string.
//
// Tier-shape:
//   - T0-T2 (Copilot CLI, ollama, claude-haiku): the harness has no
//     native skill-discovery, so the dispatcher must inline SKILL.md
//     content into the prompt at dispatch time. The stitcher returns
//     a single rendered string the dispatcher passes via
//     ExecutionRequest.prompt.
//   - T3-T4 (Claude Code headless): the harness CAN discover skills
//     from a known path. The stitcher's `materializePath` helper
//     returns the skill folder so the activity can copy it into the
//     agent's working dir; the harness handles the rest.
//
// SKILL.md format:
//   YAML frontmatter (--- delimited):
//     name: <skill-id>
//     description: <one-line>
//     tier_hint: <T0|T1|T2|T3|T4>
//     activation: <when this skill is relevant — for the agent's
//                  skill-discovery decision; ignored by tiers that
//                  inline the body>
//     tools: [tool, names, agent, can, call]
//   Markdown body — the prompt content. Reference companion files
//   via `[label](./companion.md)`; the stitcher pulls them in
//   verbatim during inline rendering.
//
// Variable substitution:
//   `{{var.path}}` resolves against the `vars` object passed to
//   render(). Missing vars throw — explicit failure beats silent
//   stub-text rendering.
//
// Pure functions exposed for testing; one I/O wrapper (loadSkill)
// reads from disk.

import { readFileSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';

// ── Types ──────────────────────────────────────────────────────────

export interface SkillFrontmatter {
  name: string;
  description: string;
  tier_hint?: string;
  activation?: string;
  tools?: string[];
  /** Allow extra fields without strict-schema rejection — frontmatter
   *  conventions evolve and the stitcher shouldn't fail-closed on a
   *  new field. The linter (#208 entry 2) is the strict gate. */
  [key: string]: unknown;
}

export interface ParsedSkill {
  /** Path to the skill folder (the dir containing SKILL.md). */
  folder: string;
  frontmatter: SkillFrontmatter;
  /** Markdown body of SKILL.md, post-frontmatter. */
  body: string;
}

// ── Pure logic ─────────────────────────────────────────────────────

/**
 * Pure: split a SKILL.md text into (frontmatter, body). The
 * frontmatter is YAML between `---` delimiters at the top of the
 * file. SplitFrontmatter is tolerant — missing or unclosed
 * frontmatter is treated as body-only — but `loadSkill` later
 * requires a `name:` field via `parseSimpleYaml`. SKILL.md files
 * without a frontmatter block (or without `name:`) will fail at
 * `loadSkill`, not here.
 *
 * Newline handling: strips one leading newline (\n OR \r\n) from
 * the body so a clean body starts on its own line regardless of
 * the original file's line endings.
 */
export function splitFrontmatter(text: string): {
  frontmatterText: string;
  body: string;
} {
  if (!text.startsWith('---\n') && !text.startsWith('---\r\n')) {
    return { frontmatterText: '', body: text };
  }
  const afterFirst = text.slice(text.indexOf('\n') + 1);
  const closeIdx = afterFirst.indexOf('\n---');
  if (closeIdx < 0) {
    // No closing fence — treat the whole thing as body (loose
    // parsing; the linter catches malformed cases).
    return { frontmatterText: '', body: text };
  }
  const frontmatterText = afterFirst.slice(0, closeIdx);
  // Skip past the closing `---` line, plus its newline.
  let afterClose = afterFirst.slice(closeIdx + '\n---'.length);
  // Normalize CRLF → LF for the leading-newline strip so files
  // authored on Windows produce identical bodies to LF files.
  if (afterClose.startsWith('\r\n')) {
    afterClose = afterClose.slice(2);
  } else if (afterClose.startsWith('\n')) {
    afterClose = afterClose.slice(1);
  }
  return { frontmatterText, body: afterClose };
}

/**
 * Pure: minimal YAML parser sufficient for the flat key:value-and-
 * inline-list frontmatter we use. Inspired by the parser in
 * apps/temporal-worker/src/grooming/parse-backlog.ts (and consistent
 * with its quoted/unquoted scalar handling), but extends it with
 * inline arrays and indented block arrays needed for SKILL.md
 * frontmatter (`tools: [a, b]` and the multiline list shape).
 * Avoids pulling a yaml lib in.
 *
 * Recognized shapes:
 *   key: value
 *   key: "value with spaces"
 *   key: [a, b, c]
 *   key:                 # then indented list items follow on next lines
 *     - a
 *     - b
 */
export function parseSimpleYaml(text: string): SkillFrontmatter {
  const fields: Record<string, unknown> = {};
  const lines = text.split('\n');
  let i = 0;
  while (i < lines.length) {
    const rawLine = lines[i] ?? '';
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) {
      i += 1;
      continue;
    }
    const colonIdx = line.indexOf(':');
    if (colonIdx <= 0) {
      i += 1;
      continue;
    }
    const key = line.slice(0, colonIdx).trim();
    let val = line.slice(colonIdx + 1).trim();

    // Inline array: `key: [a, b, c]`
    if (val.startsWith('[') && val.endsWith(']')) {
      fields[key] = val
        .slice(1, -1)
        .split(',')
        .map((s) => s.trim().replace(/^["']|["']$/g, ''))
        .filter((s) => s.length > 0);
      i += 1;
      continue;
    }

    // Block array: empty value, then indented `- item` lines
    if (val === '') {
      const items: string[] = [];
      let j = i + 1;
      while (j < lines.length) {
        const next = lines[j] ?? '';
        if (!next.startsWith('  -') && !next.startsWith('  ') && !next.startsWith('-')) break;
        const m = next.match(/^\s*-\s*(.*)$/);
        if (!m) break;
        items.push(m[1].replace(/^["']|["']$/g, ''));
        j += 1;
      }
      if (items.length > 0) {
        fields[key] = items;
        i = j;
        continue;
      }
    }

    // Quoted scalar
    if (val.startsWith('"') && val.endsWith('"')) val = val.slice(1, -1);
    if (val.startsWith("'") && val.endsWith("'")) val = val.slice(1, -1);
    fields[key] = val;
    i += 1;
  }

  // Validate the required fields. Non-strict on extras.
  if (typeof fields.name !== 'string' || !fields.name) {
    throw new Error('SKILL.md frontmatter requires a non-empty `name` field');
  }
  if (typeof fields.description !== 'string') {
    fields.description = '';
  }
  return fields as SkillFrontmatter;
}

/**
 * Pure: substitute `{{var.path}}` tokens in `text` with values from
 * `vars`. Dot-paths supported (`{{entry.id}}`). Missing vars throw —
 * fail-closed beats silently rendering a stub literal.
 */
export function substituteVars(
  text: string,
  vars: Record<string, unknown>,
): string {
  return text.replace(/\{\{([^}]+)\}\}/g, (match, expr: string) => {
    const path = expr.trim().split('.');
    let cursor: unknown = vars;
    for (const seg of path) {
      if (cursor === null || typeof cursor !== 'object') {
        throw new Error(
          `stitcher: cannot resolve {{${expr}}} — segment "${seg}" reached non-object`,
        );
      }
      cursor = (cursor as Record<string, unknown>)[seg];
      if (cursor === undefined) {
        throw new Error(`stitcher: variable {{${expr}}} not provided in vars`);
      }
    }
    if (cursor === null) {
      throw new Error(`stitcher: variable {{${expr}}} resolved to null`);
    }
    return typeof cursor === 'string' ? cursor : JSON.stringify(cursor);
  });
}

/**
 * Pure: inline the contents of any `[label](./path/to/file)` link in
 * `body` into the rendered output. Each linked file is fenced with
 * its label as the heading, so the agent sees the structure clearly.
 *
 * Only same-folder relative paths are inlined (./foo, foo). Absolute
 * paths and parent-directory traversals are rejected — the linter
 * catches these earlier, but the stitcher fails-closed in case it
 * sees one.
 */
export function inlineCompanions(
  body: string,
  loadCompanion: (relativePath: string) => string,
): string {
  return body.replace(
    /\[([^\]]+)\]\((\.\/[^)\s]+|[^)\s/]+\.md)\)/g,
    (match, label: string, path: string) => {
      // Reject parent traversal explicitly.
      if (path.includes('..')) {
        throw new Error(`stitcher: companion path must not include ".."  — got ${path}`);
      }
      const cleanPath = path.startsWith('./') ? path.slice(2) : path;
      const content = loadCompanion(cleanPath);
      return `\n--- ${label} (${cleanPath}) ---\n\n${content}\n\n--- end of ${cleanPath} ---\n`;
    },
  );
}

/**
 * Pure: assemble the final prompt string. Composition:
 *   1. Substitute {{var}} tokens in the body
 *   2. Inline any `[label](./companion.md)` links
 *   3. Strip leading/trailing whitespace
 *
 * Companion inlining happens AFTER variable substitution so that
 * companion paths can themselves be variable-driven (`{{role}}`).
 */
export function renderSkillBody(
  body: string,
  vars: Record<string, unknown>,
  loadCompanion: (relativePath: string) => string,
): string {
  const substituted = substituteVars(body, vars);
  const inlined = inlineCompanions(substituted, loadCompanion);
  return inlined.trim();
}

// ── I/O wrappers ───────────────────────────────────────────────────

/**
 * Load + parse a SKILL.md file at the given folder. Returns a
 * ParsedSkill object the stitcher's render fns consume.
 */
export function loadSkill(folder: string): ParsedSkill {
  const skillPath = join(folder, 'SKILL.md');
  let text: string;
  try {
    text = readFileSync(skillPath, 'utf8');
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`stitcher: cannot read SKILL.md at ${skillPath}: ${msg}`);
  }
  const { frontmatterText, body } = splitFrontmatter(text);
  const frontmatter = parseSimpleYaml(frontmatterText);
  return {
    folder: resolve(folder),
    frontmatter,
    body,
  };
}

/**
 * Render a skill folder's prompt string, with companion files
 * inlined. Convenience wrapper around loadSkill + renderSkillBody.
 */
export function renderSkill(
  folder: string,
  vars: Record<string, unknown>,
): string {
  const skill = loadSkill(folder);
  return renderSkillBody(skill.body, vars, (relativePath) => {
    const fullPath = join(skill.folder, relativePath);
    try {
      return readFileSync(fullPath, 'utf8');
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      throw new Error(`stitcher: cannot read companion ${relativePath}: ${msg}`);
    }
  });
}

/**
 * For tier=T3-T4 (Claude Code headless) — return the skill folder
 * path so the activity can copy it into the agent's working dir.
 * The harness then handles native skill discovery.
 */
export function materializePath(folder: string): string {
  // Confirm the folder exists AND is a directory; the activity
  // does the actual copy. Pointing materializePath at a regular
  // file would silently succeed under a bare `statSync` and then
  // fail at copy-time with a confusing error, so reject up-front.
  let stat;
  try {
    stat = statSync(folder);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`stitcher: skill folder ${folder} does not exist: ${msg}`);
  }
  if (!stat.isDirectory()) {
    throw new Error(`stitcher: skill folder ${folder} is not a directory`);
  }
  return resolve(folder);
}

/**
 * Where chitin's skill folders live. Conventionally:
 * `apps/temporal-worker/skills/<role>/`. The dispatcher resolves
 * by role name; future skills (cross-role helpers) live alongside.
 */
export const SKILLS_ROOT = 'apps/temporal-worker/skills';

export function skillFolderForRole(rootDir: string, role: string): string {
  return join(rootDir, SKILLS_ROOT, role);
}

