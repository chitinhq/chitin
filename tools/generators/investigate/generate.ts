// nx-generator-with-sync-driver-poc: POC for synchronous-driver-from-Nx-generator pattern
// Usage: nx generate @chitin:investigate <issue-or-pr>
//
// This generator:
// - Reads the issue/PR via gh API
// - Reads relevant chain events for the affected component
// - Synchronously spawns an analyst-role driver (default: openclaw-glm-flash, env-overridable) with the gathered context
// - Awaits the driver's structured output
// - Writes a markdown investigation doc at docs/observations/investigate-<id>-<YYYY-MM-DD>.md
//
// Failure modes (timeout, denial, env unset) produce clear errors and exit non-zero
// Cost is bounded: max 1 driver dispatch per generator run, with a wall-clock cap (default 60s)
// All driver tool calls go through chitin-governance plugin (no bypass)

import { execSync } from 'child_process';
import { writeFileSync, mkdirSync } from 'fs';
import { join } from 'path';

const [,, id] = process.argv;
if (!id) {
  console.error('Usage: nx generate @chitin:investigate <issue-or-pr>');
  process.exit(1);
}

const DRIVER = process.env.CHITIN_INVESTIGATE_DRIVER || 'openclaw-glm-flash';
const TIMEOUT = parseInt(process.env.CHITIN_INVESTIGATE_TIMEOUT || '60', 10);

function runGhApi(id: string): any {
  try {
    const out = execSync(`gh api repos/:owner/:repo/pulls/${id} --jq '.'`, { encoding: 'utf-8' });
    return JSON.parse(out);
  } catch (e) {
    try {
      const out = execSync(`gh api repos/:owner/:repo/issues/${id} --jq '.'`, { encoding: 'utf-8' });
      return JSON.parse(out);
    } catch (err) {
      console.error('Failed to fetch issue or PR from GitHub:', err.message);
      process.exit(2);
    }
  }
}

function getChainEvents(files: string[]): string[] {
  // Placeholder: replace with actual chain event fetch logic
  // For POC, just return dummy events
  return files.map(f => `Event for ${f}`);
}

function runDriver(context: any): string {
  // Synchronously invoke the analyst-role driver via chitin-governance plugin
  // For POC, simulate with a shell call
  try {
    const env = { ...process.env, CHITIN_DRIVER: DRIVER };
    const input = JSON.stringify(context);
    const cmd = `timeout ${TIMEOUT}s chitin-governance driver --input '${input.replace(/'/g, "'\''")}'`;
    return execSync(cmd, { encoding: 'utf-8', env });
  } catch (e) {
    console.error('Driver invocation failed:', e.message);
    process.exit(3);
  }
}

function main() {
  const pr = runGhApi(id);
  const files = pr.files ? pr.files.map((f: any) => f.filename) : [];
  const events = getChainEvents(files);
  const context = { pr, files, events };
  const driverOutput = runDriver(context);

  const date = new Date().toISOString().slice(0, 10);
  const outDir = join(process.cwd(), 'docs', 'observations');
  mkdirSync(outDir, { recursive: true });
  const outPath = join(outDir, `investigate-${id}-${date}.md`);

  const md = `# Investigation for ${id}\n\n` +
    `## PR/Issue Summary\n\n${pr.title || pr.body}\n\n` +
    `## Affected Files\n\n${files.join('\n')}\n\n` +
    `## Related Chain Events\n\n${events.join('\n')}\n\n` +
    `## Analyst Driver Observations\n\n${driverOutput}\n`;

  writeFileSync(outPath, md);
  console.log(`Investigation written to ${outPath}`);
}

main();
