import { existsSync, readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';

export function findWorkspaceChitinDir(workspacePaths: readonly string[]): string | null {
  // Walk upward from each workspace root, the same way Chitin's own
  // resolver does — a `.chitin` dir often lives at a repo root above a
  // nested VS Code workspace folder, not directly under it.
  for (const workspacePath of workspacePaths) {
    let dir = workspacePath;
    while (true) {
      const candidate = join(dir, '.chitin');
      try {
        if (existsSync(candidate) && statSync(candidate).isDirectory()) {
          return candidate;
        }
      } catch {
        // Ignore broken workspaces and continue.
      }
      const parent = dirname(dir);
      if (parent === dir) {
        break;
      }
      dir = parent;
    }
  }
  return null;
}

export function listEventChainFiles(chitinDir: string): string[] {
  try {
    return readdirSync(chitinDir)
      .filter((entry) => entry === 'events.jsonl' || /^events-.*\.jsonl$/.test(entry))
      .sort();
  } catch {
    return [];
  }
}
