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
 */
export function resolveChitinDir(cwd: string, workspaceBoundary: string): string {
  const absCwd = resolve(cwd);
  const absBoundary = workspaceBoundary ? resolve(workspaceBoundary) : '';

  let dir = absCwd;
  while (true) {
    const candidate = join(dir, '.chitin');
    if (existsSync(candidate) && statSync(candidate).isDirectory()) {
      return candidate;
    }
    if (absBoundary && dir === absBoundary) break;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  const orphan = join(homedir(), '.chitin');
  if (!existsSync(orphan)) {
    mkdirSync(orphan, { recursive: true });
  }
  return orphan;
}
