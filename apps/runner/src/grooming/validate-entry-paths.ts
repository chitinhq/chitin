// validate-entry-paths.ts
// Validates that all file paths referenced in backlog entries exist in the current main worktree.
// Returns a list of missing paths for each entry.

import { parseBacklog, type BacklogEntry } from './parse-backlog.ts';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

// Returns an array of { entryId, missing: string[] } for entries with missing files
export function validateEntryPaths(backlogPath: string): Array<{ entryId: string; missing: string[] }> {
  const entries: BacklogEntry[] = parseBacklog(backlogPath);
  const results: Array<{ entryId: string; missing: string[] }> = [];
  for (const entry of entries) {
    if (!entry.file) continue;
    const files = entry.file.split(',').map(f => f.trim()).filter(Boolean);
    const missing = files.filter(f => !existsSync(resolve(process.cwd(), f)));
    if (missing.length > 0) {
      results.push({ entryId: entry.id, missing });
    }
  }
  return results;
}

if (require.main === module) {
  const backlogPath = resolve(process.cwd(), 'docs/swarm-backlog.md');
  const missing = validateEntryPaths(backlogPath);
  if (missing.length === 0) {
    console.log('All entry file paths exist.');
  } else {
    for (const { entryId, missing: paths } of missing) {
      console.log(`Entry ${entryId} missing paths: ${paths.join(', ')}`);
    }
    process.exit(1);
  }
}
