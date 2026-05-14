import { existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

export function findWorkspaceChitinDir(workspacePaths: readonly string[]): string | null {
  for (const workspacePath of workspacePaths) {
    const candidate = join(workspacePath, '.chitin');
    try {
      if (existsSync(candidate) && statSync(candidate).isDirectory()) {
        return candidate;
      }
    } catch {
      // Ignore broken workspaces and continue.
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
