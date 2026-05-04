#!/usr/bin/env -S pnpm exec tsx
// chitin-investigate — one-shot CLI that gathers context for an
// issue/PR + invokes a chitin-governed driver to produce an
// investigation markdown.
//
// Usage:
//   pnpm exec tsx tools/generators/investigate/generate.ts <issue-or-pr-number>
//
// Note on naming: the original POC was filed as
// `nx-generator-with-sync-driver-poc` (#291) but Nx generators are
// supposed to operate on a `Tree` of file edits, not spawn driver
// subprocesses. We kept the directory name for now so the swarm
// branch + PR don't have to be re-cut, but the implementation is a
// straight CLI script. Wiring it as a real `nx run-commands`
// executor is a follow-up.
//
// Pipeline:
// 1. Fetch the issue/PR via `gh api` (typed args, no shell injection).
// 2. Read recent gov-decisions chain rows for the affected files.
// 3. Format a prompt and dispatch to a driver.
//    Default driver: `claude -p <prompt> --model claude-haiku-4-5`
//    (Anthropic Max plan — cheap haiku-class). Operator can override
//    with CHITIN_INVESTIGATE_DRIVER=openclaw|claude.
// 4. Write the result as
//    docs/observations/investigate-<id>-<YYYY-MM-DD>.md
//
// All driver tool calls are gated by chitin's PreToolUse hook the
// same way every other Claude Code session is — no governance bypass.

import { spawnSync } from 'node:child_process';
import { writeFileSync, mkdirSync, readdirSync, readFileSync, existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const [, , id] = process.argv;
if (!id) {
  console.error('Usage: tools/generators/investigate/generate.ts <issue-or-pr-number>');
  process.exit(1);
}

if (!/^\d+$/.test(id)) {
  console.error(`investigate: id must be a positive integer, got ${JSON.stringify(id)}`);
  process.exit(1);
}

const DRIVER = (process.env.CHITIN_INVESTIGATE_DRIVER ?? 'claude').trim();
const MODEL = (process.env.CHITIN_INVESTIGATE_MODEL ?? 'claude-haiku-4-5').trim();
const TIMEOUT_MS = Number(process.env.CHITIN_INVESTIGATE_TIMEOUT_S ?? '60') * 1000;

interface PrLike {
  number: number;
  title: string;
  body: string | null;
  files?: { filename: string }[];
  state?: string;
  user?: { login: string };
  html_url?: string;
}

// ─── 1. Fetch issue/PR via gh ─────────────────────────────────────────────

function runGhJson(...args: string[]): unknown | null {
  const result = spawnSync('gh', args, {
    encoding: 'utf-8',
    timeout: 30_000,
    maxBuffer: 10 * 1024 * 1024,
  });
  if (result.status !== 0) {
    return null;
  }
  try {
    return JSON.parse(result.stdout);
  } catch {
    return null;
  }
}

function fetchPrOrIssue(id: string): PrLike | null {
  // Try PR first; fall back to issue. The two endpoints have
  // overlapping but not identical shapes — both have number/title/body,
  // PRs additionally have files/state/user.
  const pr = runGhJson('api', `repos/{owner}/{repo}/pulls/${id}`);
  if (pr && typeof pr === 'object') {
    return pr as PrLike;
  }
  const issue = runGhJson('api', `repos/{owner}/{repo}/issues/${id}`);
  if (issue && typeof issue === 'object') {
    return issue as PrLike;
  }
  return null;
}

function fetchPrFiles(id: string): string[] {
  const files = runGhJson('api', `repos/{owner}/{repo}/pulls/${id}/files`);
  if (!Array.isArray(files)) return [];
  return files
    .map((f) => (typeof f === 'object' && f && 'filename' in f ? String(f.filename) : ''))
    .filter(Boolean);
}

// ─── 2. Read recent chain decisions ───────────────────────────────────────

interface DecisionRow {
  ts: string;
  rule_id?: string;
  agent?: string;
  action_type?: string;
  action_target?: string;
  workflow_id?: string;
  fingerprint?: string;
}

function readRecentChainRows(
  affectedFiles: string[],
  maxRows = 30,
): DecisionRow[] {
  const chitinDir = process.env.CHITIN_HOME || join(homedir(), '.chitin');
  if (!existsSync(chitinDir)) return [];

  // Read recent gov-decisions logs, scan for action_target intersecting
  // any affected file. Conservative: small N, recent only — operator
  // gets a slice, not the full chain.
  const candidates: string[] = [];
  try {
    for (const name of readdirSync(chitinDir)) {
      if (/^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$/.test(name)) {
        candidates.push(join(chitinDir, name));
      }
    }
  } catch {
    return [];
  }
  candidates.sort().reverse(); // newest first

  const results: DecisionRow[] = [];
  for (const path of candidates.slice(0, 3)) {
    let body: string;
    try {
      body = readFileSync(path, 'utf-8');
    } catch {
      continue;
    }
    for (const line of body.split('\n')) {
      if (!line.trim()) continue;
      let row: DecisionRow;
      try {
        row = JSON.parse(line);
      } catch {
        continue;
      }
      // Filter: row touches one of the affected files. When no files
      // are known (issue case), skip the row — we don't want to flood
      // the prompt with unrelated chain noise.
      if (affectedFiles.length === 0) continue;
      const target = row.action_target ?? '';
      if (!affectedFiles.some((f) => target.includes(f))) continue;
      results.push(row);
      if (results.length >= maxRows) return results;
    }
  }
  return results;
}

// ─── 3. Driver dispatch ───────────────────────────────────────────────────

function buildPrompt(pr: PrLike, files: string[], chainRows: DecisionRow[]): string {
  const chainBlock = chainRows.length
    ? chainRows
        .slice(0, 20)
        .map(
          (r) =>
            `- ${r.ts} ${r.rule_id ?? '?'} ${r.action_type ?? '?'} ` +
            `target=${(r.action_target ?? '').slice(0, 80)}`,
        )
        .join('\n')
    : '(no recent chain rows touch these files)';

  return [
    `# Investigation request: chitin issue/PR #${pr.number}`,
    '',
    `## Title`,
    pr.title,
    '',
    `## Body`,
    pr.body || '(no body)',
    '',
    `## Affected files`,
    files.length ? files.map((f) => `- ${f}`).join('\n') : '(none)',
    '',
    `## Recent chain rows for affected files`,
    chainBlock,
    '',
    `## Your task`,
    `Write a 3-5 paragraph investigation. Focus on root cause, related ` +
      `chain signals, and the recommended next action. Be specific about ` +
      `which files / functions / commits are load-bearing. Do NOT speculate ` +
      `beyond what the data above supports.`,
  ].join('\n');
}

function runDriver(prompt: string): string {
  switch (DRIVER) {
    case 'claude': {
      const result = spawnSync(
        'claude',
        ['-p', prompt, '--model', MODEL, '--dangerously-skip-permissions'],
        {
          encoding: 'utf-8',
          timeout: TIMEOUT_MS,
          maxBuffer: 10 * 1024 * 1024,
        },
      );
      if (result.error) {
        throw new Error(`claude spawn failed: ${result.error.message}`);
      }
      if (result.status !== 0) {
        throw new Error(
          `claude exited ${result.status}: ${(result.stderr || '').slice(0, 500)}`,
        );
      }
      return result.stdout;
    }
    case 'openclaw': {
      const agent = process.env.CHITIN_INVESTIGATE_AGENT ?? 'glm-flash-agent';
      const result = spawnSync(
        'openclaw',
        [
          'agent',
          '--local',
          '--agent',
          agent,
          '--message',
          prompt,
          '--timeout',
          String(Math.floor(TIMEOUT_MS / 1000)),
        ],
        {
          encoding: 'utf-8',
          timeout: TIMEOUT_MS + 5_000,
          maxBuffer: 10 * 1024 * 1024,
        },
      );
      if (result.error) {
        throw new Error(`openclaw spawn failed: ${result.error.message}`);
      }
      if (result.status !== 0) {
        throw new Error(
          `openclaw exited ${result.status}: ${(result.stderr || '').slice(0, 500)}`,
        );
      }
      return result.stdout;
    }
    default:
      throw new Error(
        `unknown CHITIN_INVESTIGATE_DRIVER=${JSON.stringify(DRIVER)} ` +
          `(supported: claude, openclaw)`,
      );
  }
}

// ─── 4. Main ──────────────────────────────────────────────────────────────

function main(): void {
  const pr = fetchPrOrIssue(id);
  if (!pr) {
    console.error(`investigate: failed to fetch issue or PR #${id}`);
    process.exit(2);
  }

  const files = pr.files
    ? pr.files.map((f) => f.filename).filter(Boolean)
    : fetchPrFiles(id);
  const chainRows = readRecentChainRows(files);
  const prompt = buildPrompt(pr, files, chainRows);

  let driverOutput: string;
  try {
    driverOutput = runDriver(prompt);
  } catch (e) {
    console.error(`investigate: ${e instanceof Error ? e.message : String(e)}`);
    process.exit(3);
  }

  const date = new Date().toISOString().slice(0, 10);
  const outDir = join(process.cwd(), 'docs', 'observations');
  mkdirSync(outDir, { recursive: true });
  const outPath = join(outDir, `investigate-${id}-${date}.md`);

  const md = [
    `# Investigation: #${pr.number}`,
    '',
    `**Generated**: ${new Date().toISOString()}`,
    `**Driver**: ${DRIVER} (${MODEL})`,
    pr.html_url ? `**Source**: ${pr.html_url}` : '',
    '',
    `## Title`,
    '',
    pr.title,
    '',
    `## Affected files`,
    '',
    files.length ? files.map((f) => `- \`${f}\``).join('\n') : '_(none)_',
    '',
    `## Investigation`,
    '',
    driverOutput.trim(),
    '',
    '---',
    '',
    `_Generated by tools/generators/investigate/generate.ts._`,
  ].join('\n');

  writeFileSync(outPath, md);
  console.log(outPath);
}

main();
