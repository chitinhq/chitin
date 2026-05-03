import type { Tree } from '../shared.ts';
import { isoDate, idToTitle } from '../shared.ts';

export interface SpecOptions {
  id: string;
  title?: string;
  lens?: string;
  status?: string;
  supersedes?: string;
  tldr?: string;
}

export function specTemplate(opts: Required<SpecOptions>, date: string): string {
  return `# ${opts.title} — Design Spec

**Date:** ${date}
**Status:** ${opts.status}
**Active soul during design:** ${opts.lens}
**Supersedes:** ${opts.supersedes}

## TL;DR

${opts.tldr || '<!-- one-sentence thesis goes here -->'}

## Background

## Scope

**In scope:**
-

**Out of scope:**
-

## Architecture sketch

## Open questions
`;
}

export default function specGenerator(tree: Tree, options: SpecOptions): void {
  const date = isoDate();
  const resolved: Required<SpecOptions> = {
    id: options.id,
    title: options.title ?? idToTitle(options.id),
    lens: options.lens ?? 'da Vinci',
    status: options.status ?? 'draft',
    supersedes: options.supersedes ?? '—',
    tldr: options.tldr ?? '',
  };

  const outputPath = `docs/superpowers/specs/${date}-${resolved.id}-design.md`;
  tree.write(outputPath, specTemplate(resolved, date));
  console.log(`created: ${outputPath}`);
}
