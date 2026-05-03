#!/usr/bin/env node
// Interactive generator for docs/swarm-backlog.md entries.
//
// Usage (from repo root):
//   pnpm exec tsx tools/generators/backlog-entry/index.ts
//   pnpm exec tsx tools/generators/backlog-entry/index.ts --backlog docs/swarm-backlog.md

import { createInterface } from 'node:readline/promises';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Command } from 'commander';
import { RoleSchema } from '@chitin/contracts';
import { parseBacklog } from '../../../apps/temporal-worker/src/grooming/parse-backlog.js';
import { buildSection, insertEntry, hasDuplicateId } from './generate.js';

const ROLES: string[] = RoleSchema.options;
const TIERS = ['T0', 'T1', 'T2', 'T3', 'T4', 'T5'];
const STATUSES = ['ready', 'in_design', 'needs_human', 'blocked'];

function completer(line: string): [string[], string] {
  const hits = ROLES.filter((r) => r.startsWith(line));
  return [hits.length > 0 ? hits : ROLES, line];
}

async function prompt(rl: Awaited<ReturnType<typeof createInterface>>, question: string): Promise<string> {
  const answer = await rl.question(question);
  return answer.trim();
}

async function promptWithDefault(
  rl: Awaited<ReturnType<typeof createInterface>>,
  question: string,
  defaultVal: string,
): Promise<string> {
  const answer = await prompt(rl, `${question} [${defaultVal}]: `);
  return answer || defaultVal;
}

async function promptEnum(
  rl: Awaited<ReturnType<typeof createInterface>>,
  question: string,
  choices: string[],
  defaultVal?: string,
): Promise<string> {
  const hint = defaultVal ? `[${defaultVal}] ` : '';
  while (true) {
    const raw = await prompt(rl, `${question} (${choices.join('|')}) ${hint}`);
    const val = raw || defaultVal || '';
    if (choices.includes(val)) return val;
    console.error(`  Invalid choice "${val}". Pick one of: ${choices.join(', ')}`);
  }
}

async function run(backlogPath: string): Promise<void> {
  const rl = createInterface({
    input: process.stdin,
    output: process.stdout,
    completer,
  });

  try {
    console.log('\nNew backlog entry generator');
    console.log('---------------------------');
    console.log('(Tab-completes role names)\n');

    const existingEntries = parseBacklog(backlogPath);
    const existingIds = new Set(existingEntries.map((e) => e.id));

    // Prompt for id
    let id = '';
    while (!id) {
      id = await prompt(rl, 'Entry id (slug, e.g. my-new-feature): ');
      if (!id) { console.error('  id is required'); continue; }
      if (!/^[a-z0-9][a-z0-9-]*$/.test(id)) {
        console.error('  id must be lowercase alphanumeric + hyphens');
        id = '';
        continue;
      }
      if (existingIds.has(id)) {
        console.error(`  id "${id}" already exists in the backlog (duplicate ids are rejected)`);
        id = '';
      }
    }

    const tier = await promptEnum(rl, 'Tier', TIERS, 'T1');
    const status = await promptEnum(rl, 'Status', STATUSES, 'ready');

    // Role prompt with tab completion
    let role = '';
    while (!role) {
      role = await prompt(rl, `Role (Tab to complete, choices: ${ROLES.join(', ')})\n  > `);
      if (!ROLES.includes(role)) {
        console.error(`  Unknown role "${role}". Valid: ${ROLES.join(', ')}`);
        role = '';
      }
    }

    const file = await prompt(rl, 'File scope (comma-separated paths, or leave blank): ');
    const blocksRaw = await prompt(rl, 'Blocks (comma-separated entry ids, or leave blank): ');
    const blocks = blocksRaw ? blocksRaw.split(',').map((s) => s.trim()).filter(Boolean) : [];
    const estimatedLoc = await prompt(rl, 'Estimated LOC (optional): ');
    const referencesFinding = await prompt(rl, 'references_finding (optional): ');
    const referencesSpec = await prompt(rl, 'references_spec (optional): ');
    const referencesDesign = await prompt(rl, 'references_design (optional): ');

    console.log('\nDescription (multi-line; enter a single "." on a line to finish):');
    const descLines: string[] = [];
    while (true) {
      const line = await prompt(rl, '  ');
      if (line === '.') break;
      descLines.push(line);
    }
    const description = descLines.join('\n');

    const section = buildSection({
      id,
      tier,
      status,
      role,
      file,
      blocks,
      estimated_loc: estimatedLoc,
      references_finding: referencesFinding,
      references_spec: referencesSpec,
      references_design: referencesDesign,
      description,
    });

    console.log('\n--- Preview ---');
    console.log(section);
    console.log('---------------\n');

    const confirm = await prompt(rl, 'Write to backlog? (y/N): ');
    if (confirm.toLowerCase() !== 'y') {
      console.log('Aborted.');
      return;
    }

    const original = readFileSync(backlogPath, 'utf8');
    const updated = insertEntry(original, section);

    // Round-trip validation: parse the updated text via parseBacklog.
    // Write to a temp file, parse, verify the entry appears with correct id.
    const tmpPath = `${backlogPath}.gen-tmp`;
    writeFileSync(tmpPath, updated, 'utf8');
    try {
      const parsed = parseBacklog(tmpPath);
      if (hasDuplicateId(parsed.filter((e) => e.id === id).slice(1), id)) {
        throw new Error(`duplicate id "${id}" detected after write — aborting`);
      }
      const entry = parsed.find((e) => e.id === id);
      if (!entry) throw new Error(`round-trip failed: id "${id}" not found after parse`);
      // Verify heading id matches frontmatter id
      const fmIdMatch = entry.rawFrontmatter.match(/^id:\s*(\S+)/m);
      if (fmIdMatch && fmIdMatch[1] !== entry.id) {
        throw new Error(`heading id "${entry.id}" ≠ frontmatter id "${fmIdMatch[1]}"`);
      }
    } catch (err) {
      // Remove temp file and abort
      try { writeFileSync(tmpPath, '', 'utf8'); } catch { /* ignore */ }
      const { unlinkSync } = await import('node:fs');
      try { unlinkSync(tmpPath); } catch { /* ignore */ }
      throw err;
    }

    // Validation passed — rename temp to real file
    const { renameSync, unlinkSync } = await import('node:fs');
    try { unlinkSync(`${backlogPath}.gen-bak`); } catch { /* no backup to remove */ }
    // Write backup then overwrite
    writeFileSync(backlogPath, updated, 'utf8');
    unlinkSync(tmpPath);

    console.log(`\nWrote entry "${id}" to ${backlogPath}`);
  } finally {
    rl.close();
  }
}

const program = new Command();
program
  .name('backlog-entry')
  .description('Interactive generator for docs/swarm-backlog.md entries')
  .option('--backlog <path>', 'path to swarm-backlog.md', 'docs/swarm-backlog.md')
  .action(async (opts: { backlog: string }) => {
    const backlogPath = resolve(process.cwd(), opts.backlog);
    try {
      await run(backlogPath);
    } catch (err) {
      console.error('Error:', err instanceof Error ? err.message : err);
      process.exit(1);
    }
  });

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  program.parse();
}
