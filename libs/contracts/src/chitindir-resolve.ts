import { existsSync, statSync, mkdirSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { homedir } from 'node:os';

/**
 * Resolve the .chitin state dir for a given cwd. TS mirror of the Go
 * resolver at go/execution-kernel/internal/chitindir/resolve.go — outputs
 * must match byte-for-byte on the same filesystem state.
 *
 * Walks up from cwd looking for an existing `.chitin/` directory. Stops at
 * workspaceBoundary (inspected; ancestors not crossed). Falls back to
 * `$HOME/.chitin/` (creating it on demand) when no enclosing dir is found.
 *
 * fs errors (EACCES, EIO, etc.) during walk-up are treated as "candidate
 * not usable" and the walk continues. A failure to create the orphan
 * directory returns the orphan path regardless — the caller (the adapter
 * bin) catches downstream I/O failures at the top level and exits 0 so
 * Claude Code's session never breaks. Go's resolver propagates errors;
 * TS chooses best-effort because its single caller is a session-safety
 * boundary.
 */
export function resolveChitinDir(cwd: string, workspaceBoundary: string): string {
  const absCwd = resolve(cwd);
  const absBoundary = workspaceBoundary ? resolve(workspaceBoundary) : '';

  let dir = absCwd;
  while (true) {
    const candidate = join(dir, '.chitin');
    if (isExistingDir(candidate)) {
      return candidate;
    }
    if (absBoundary && dir === absBoundary) break;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  const orphan = join(homedir(), '.chitin');
  if (!isExistingDir(orphan)) {
    try {
      mkdirSync(orphan, { recursive: true });
    } catch {
      // best-effort: return the orphan path even if creation failed;
      // the caller's try/catch catches downstream failures.
    }
  }
  return orphan;
}

function isExistingDir(path: string): boolean {
  try {
    return existsSync(path) && statSync(path).isDirectory();
  } catch {
    return false;
  }
}
