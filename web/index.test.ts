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

  it('max boundary: renders the full 26 invariant public surface', () => {
    const invariantItems = html.match(/<li><strong>\d+\. /g) ?? [];

    expect(invariantItems).toHaveLength(26);
    expect(html).toContain('26 live invariants on the public surface');
    expect(html).toContain('1. No recursive delete');
    expect(html).toContain('26. Shell execution is allowed by default');
  });

  it('error boundary: exposes deny examples, install command, docs, GitHub, and guarded deploy', () => {
    expect(html).toContain('<span class="deny">deny</span>');
    expect(html).toContain('reason: Force push rewrites shared history');
    expect(html).toContain('<code class="command">npx chitin</code>');
    expect(html).toContain('https://github.com/chitinhq/chitin/tree/main/docs');
    expect(html).toContain('https://github.com/chitinhq/chitin');
    expect(pagesWorkflow).toContain('path: web');
    expect(pagesWorkflow).toContain(
      "if: github.event_name == 'workflow_dispatch' || github.ref_name == github.event.repository.default_branch",
    );
  });
});
