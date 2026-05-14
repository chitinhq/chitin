import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const html = readFileSync(join(root, 'web', 'index.html'), 'utf8');
const pagesWorkflow = readFileSync(join(root, '.github', 'workflows', 'pages.yml'), 'utf8');

describe('static GitHub Pages site boundary coverage', () => {
  it('empty boundary: ships without a build script or runtime asset fetches', () => {
    expect(html).not.toMatch(/<script\b/i);
    expect(html).not.toMatch(/<(?:img|iframe|video|audio|source)\b/i);
    expect(html).not.toMatch(/<link\b[^>]+\bhref=["'](?:https?:)?\/\//i);
    expect(html).toContain('No site generator. No build artifact. No runtime dependencies.');
  });

  it('max boundary: renders the full 26 invariant public surface, numbered 1..26', () => {
    // Pull the actual leading numbers out of each invariant <li> and assert
    // they are exactly the sequence 1..26 — a plain \d+ count would still
    // pass if an item were renumbered, duplicated, or skipped.
    const numbers = [...html.matchAll(/<li><strong>(\d+)\. /g)].map((m) => Number(m[1]));
    expect(numbers).toEqual(Array.from({ length: 26 }, (_, i) => i + 1));
    expect(html).toContain('26 live invariants on the public surface');
    expect(html).toContain('1. No recursive delete');
    expect(html).toContain('26. Shell execution is allowed by default');
  });

  it('error boundary: exposes deny examples, install command, docs, GitHub, and guarded deploy', () => {
    expect(html).toContain('<span class="deny">deny</span>');
    expect(html).toContain('reason: Force push rewrites shared history');
    // The install command must name the package that actually publishes
    // (apps/cli is @chitin/cli); a bare `chitin` package does not exist on npm.
    expect(html).toContain('<code class="command">npx @chitin/cli</code>');
    expect(html).toContain('https://github.com/chitinhq/chitin/tree/main/docs');
    expect(html).toContain('https://github.com/chitinhq/chitin');
    expect(pagesWorkflow).toContain('path: web');
    // Assert the deploy is guarded without pinning the exact `if:` string:
    // the workflow only triggers on default-branch pushes, and the job only
    // runs on the default branch or a manual dispatch.
    expect(pagesWorkflow).toMatch(/push:\s*\n\s*branches:\s*\[\s*main\s*\]/);
    expect(pagesWorkflow).toContain('workflow_dispatch');
    expect(pagesWorkflow).toContain('github.event.repository.default_branch');
  });
});
