#!/usr/bin/env node
/**
 * Claude Code adapter entrypoint.
 *
 * Reads hook JSON from stdin, resolves .chitin/ via walk-up (or orphan
 * fallback at $HOME/.chitin/), builds an AdapterContext from env + cwd,
 * calls runHook(), and exits. Hook failure is non-fatal to Claude Code —
 * chitin must never break the user's session.
 */
import { readFileSync } from 'node:fs';
import { runHook } from '../src/hook-runner.js';
import type { AdapterContext } from '../src/hook-runner.js';
import { buildAdapterContext, resolveChitinDir } from '../src/adapter-context.js';

function readStdinSync(): string {
  // process.stdin.fd is 0; reading synchronously is acceptable for hook entry.
  try {
    return readFileSync(0, 'utf8');
  } catch {
    return '';
  }
}

async function main(): Promise<void> {
  const raw = readStdinSync();
  if (!raw.trim()) {
    process.exit(0);
  }
  let input: Record<string, unknown>;
  try {
    input = JSON.parse(raw);
  } catch (err) {
    console.error('chitin-adapter: invalid hook JSON on stdin', err);
    process.exit(0);
  }

  const hookCwd = typeof input['cwd'] === 'string' ? input['cwd'] : process.cwd();
  const chitinDir = resolveChitinDir(hookCwd, '');

  const built = buildAdapterContext({
    surface: 'claude-code',
    chitinDir,
  });

  // Cast surface to the narrow literal type that hook-runner.ts requires.
  const ctx: AdapterContext = { ...built, surface: 'claude-code' };

  try {
    runHook(input as Parameters<typeof runHook>[0], ctx);
  } catch (err) {
    console.error('chitin-adapter: runHook failed (non-fatal)', err);
  }
}

main().catch((err) => {
  console.error('chitin-adapter: top-level error (non-fatal)', err);
  process.exit(0);
});
