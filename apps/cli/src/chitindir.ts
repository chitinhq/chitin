import { existsSync, mkdirSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { homedir } from 'node:os';

// Walk up from cwd looking for an existing `.chitin` dir, stopping at the
// workspace boundary; falls back to an existing `~/.chitin`. Returns null
// when nothing exists — pure lookup, no filesystem side effects.
export function findChitinDir(cwd: string, workspaceBoundary: string): string | null {
  const absCwd = resolve(cwd);
  const absBoundary = workspaceBoundary ? resolve(workspaceBoundary) : '';

  let dir = absCwd;
  while (true) {
    const candidate = join(dir, '.chitin');
    if (isExistingDir(candidate)) return candidate;
    if (absBoundary && dir === absBoundary) break;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  const orphan = join(homedir(), '.chitin');
  return isExistingDir(orphan) ? orphan : null;
}

// Like findChitinDir, but guarantees a usable directory: when nothing is
// found it creates (and returns) the home-level `~/.chitin`. Use this for
// commands that need to *write* state — never for read-only status checks,
// which must not have the side effect of creating an orphan state dir.
export function resolveChitinDir(cwd: string, workspaceBoundary: string): string {
  const found = findChitinDir(cwd, workspaceBoundary);
  if (found) return found;

  const orphan = join(homedir(), '.chitin');
  try {
    mkdirSync(orphan, { recursive: true });
  } catch {
    // best effort
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
