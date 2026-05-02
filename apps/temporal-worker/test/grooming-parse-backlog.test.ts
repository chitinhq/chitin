import { describe, expect, it, beforeEach, afterEach } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { parseBacklog } from '../src/grooming/parse-backlog.ts';

let scratch: string;

beforeEach(() => {
  scratch = mkdtempSync(join(tmpdir(), 'groom-parse-test-'));
});

afterEach(() => {
  rmSync(scratch, { recursive: true, force: true });
});

function write(name: string, content: string): string {
  const path = join(scratch, name);
  writeFileSync(path, content);
  return path;
}

describe('parseBacklog', () => {
  it('extracts a single ready entry with full frontmatter', () => {
    const path = write(
      'b.md',
      `# Title

## Ready

### \`my-entry\`

\`\`\`yaml
id: my-entry
tier: T0
status: ready
estimated_loc: 5
blocks: []
file: chitin.yaml
\`\`\`

A short description that explains the work to be done.
`,
    );
    const entries = parseBacklog(path);
    expect(entries).toHaveLength(1);
    const e = entries[0];
    expect(e.id).toBe('my-entry');
    expect(e.status).toBe('ready');
    expect(e.tier).toBe('T0');
    expect(e.estimatedLoc).toBe('5');
    expect(e.file).toBe('chitin.yaml');
    expect(e.blocks).toEqual([]);
    expect(e.description).toContain('A short description');
    expect(e.rawFrontmatter).toContain('id: my-entry');
  });

  it('parses multiple entries in order', () => {
    const path = write(
      'b.md',
      `## Ready

### \`alpha\`

\`\`\`yaml
id: alpha
status: ready
\`\`\`

alpha description.

### \`beta\`

\`\`\`yaml
id: beta
status: in_design
\`\`\`

beta description.

### \`gamma\`

\`\`\`yaml
id: gamma
status: needs_human
\`\`\`

gamma description.
`,
    );
    const entries = parseBacklog(path);
    expect(entries.map((e) => e.id)).toEqual(['alpha', 'beta', 'gamma']);
    expect(entries.map((e) => e.status)).toEqual(['ready', 'in_design', 'needs_human']);
  });

  it('treats an entry without status as ready (default)', () => {
    const path = write(
      'b.md',
      `## section

### \`no-status\`

\`\`\`yaml
id: no-status
tier: T1
\`\`\`

description.
`,
    );
    const entries = parseBacklog(path);
    expect(entries[0].status).toBe('ready');
  });

  it('skips H3 sections without an id heading', () => {
    const path = write(
      'b.md',
      `## section

### Tier counts (snapshot)

just text, not a backlog entry.

### \`real-entry\`

\`\`\`yaml
id: real-entry
status: ready
\`\`\`

real one.
`,
    );
    const entries = parseBacklog(path);
    expect(entries).toHaveLength(1);
    expect(entries[0].id).toBe('real-entry');
  });

  it('skips H3 sections without a yaml block', () => {
    const path = write(
      'b.md',
      `## section

### \`bare-heading-only\`

no yaml here.

### \`real-entry\`

\`\`\`yaml
id: real-entry
status: ready
\`\`\`

real one.
`,
    );
    const entries = parseBacklog(path);
    expect(entries).toHaveLength(1);
    expect(entries[0].id).toBe('real-entry');
  });

  it('does not collect entries from a strategic ## section if entries are inside H3 only', () => {
    const path = write(
      'b.md',
      `## Strategic

text only, no h3 with entries here.

## Ready

### \`x\`

\`\`\`yaml
id: x
status: ready
\`\`\`

real.
`,
    );
    const entries = parseBacklog(path);
    expect(entries).toHaveLength(1);
    expect(entries[0].id).toBe('x');
  });

  it('preserves rawFrontmatter verbatim for round-tripping', () => {
    const yaml = `id: round
tier: T2
status: in_design
estimated_loc: 30-60
file: apps/foo.ts`;
    const path = write(
      'b.md',
      `## section

### \`round\`

\`\`\`yaml
${yaml}
\`\`\`

prose.
`,
    );
    const entries = parseBacklog(path);
    expect(entries[0].rawFrontmatter).toBe(yaml);
  });

  it('parses the real swarm-backlog.md and finds in_design entries', () => {
    // Smoke test against the actual file (relies on cwd = repo root).
    const repoBacklog = join(process.cwd(), 'docs/swarm-backlog.md');
    const entries = parseBacklog(repoBacklog);
    expect(entries.length).toBeGreaterThan(5);
    const inDesign = entries.filter((e) => e.status === 'in_design');
    expect(inDesign.length).toBeGreaterThan(0);
    // Spot-check: known entry from swarm-backlog.md
    const wallTimeout = entries.find((e) => e.id === 'wall-timeout-sigkill-propagation');
    expect(wallTimeout).toBeDefined();
    expect(wallTimeout?.status).toBe('in_design');
    expect(wallTimeout?.tier).toBe('T2');
  });
});
