import type { Tree } from '../shared.ts';
import { isoDate, idToTitle } from '../shared.ts';

export interface PlanOptions {
  id: string;
  title?: string;
  lens?: string;
  status?: string;
  spec?: string;
  tldr?: string;
}

export function planTemplate(opts: Required<PlanOptions>, date: string): string {
  const specLine = opts.spec
    ? `**Spec:** ${opts.spec}\n\n`
    : '';
  return `# ${opts.title} — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (\`- [ ]\`) syntax for tracking.

**Date:** ${date}
**Status:** ${opts.status}
**Goal:** ${opts.tldr || '<!-- one-sentence goal goes here -->'}

${specLine}**Active soul during planning:** ${opts.lens}

---

## File Structure

### New files

### Modified files

---

## Tasks

- [ ] <!-- first task -->

---

## Definition of done

- [ ] All tasks checked off
- [ ] Tests pass
`;
}

export default function planGenerator(tree: Tree, options: PlanOptions): void {
  const date = isoDate();
  const resolved: Required<PlanOptions> = {
    id: options.id,
    title: options.title ?? idToTitle(options.id),
    lens: options.lens ?? 'da Vinci',
    status: options.status ?? 'draft',
    spec: options.spec ?? '',
    tldr: options.tldr ?? '',
  };

  const outputPath = `docs/superpowers/plans/${date}-${resolved.id}.md`;
  tree.write(outputPath, planTemplate(resolved, date));
  console.log(`created: ${outputPath}`);
}
