#!/usr/bin/env node
/**
 * Claude Code adapter entrypoint for user-level hook install.
 *
 * Reads hook JSON from stdin, resolves .chitin/ via walk-up (or orphan
 * fallback at $HOME/.chitin/), builds an AdapterContext preserving the
 * `session_id` provided by Claude Code, calls runHook(), and exits.
 *
 * Hook failure is non-fatal to Claude Code — chitin must never break the
 * user's session.
 */
import { readFileSync } from 'node:fs';
import { resolveChitinDir } from '@chitin/contracts';
import { runHook, type HookInput } from '../src/hook-runner';
import { buildHookContext } from '../src/hook-context';

function readStdinSync(): string {
  try {
    return readFileSync(0, 'utf8');
  } catch {
    return '';
  }
}

function main(): void {
  const raw = readStdinSync();
  if (!raw.trim()) {
    process.exit(0);
  }
  let input: HookInput;
  try {
    input = JSON.parse(raw) as HookInput;
  } catch (err) {
    console.error('chitin-adapter: invalid hook JSON on stdin', err);
    process.exit(0);
  }

  const sessionID = typeof input.session_id === 'string' ? input.session_id : '';
  if (!sessionID) {
    console.error('chitin-adapter: input has no session_id; skipping emit');
    process.exit(0);
  }

  const hookCwd =
    typeof input['cwd'] === 'string' ? (input['cwd'] as string) : process.cwd();
  const chitinDir = resolveChitinDir(hookCwd, '');

  const ctx = buildHookContext(sessionID, chitinDir);

  try {
    runHook(input, ctx);
  } catch (err) {
    console.error('chitin-adapter: runHook failed (non-fatal)', err);
  }
}

main();
