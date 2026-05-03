import type { Tree } from '../shared.ts';
import { isoDate, idToTitle } from '../shared.ts';

export interface ObservationOptions {
  id: string;
  title?: string;
  lens?: string;
  status?: string;
  type?: string;
}

export function observationTemplate(opts: Required<ObservationOptions>, date: string): string {
  return `---
date: ${date}
status: ${opts.status}
type: ${opts.type}
lens: ${opts.lens}
---

# ${opts.title}

## Why this exists

## Findings

## Next steps
`;
}

export default function observationGenerator(tree: Tree, options: ObservationOptions): void {
  const date = isoDate();
  const resolved: Required<ObservationOptions> = {
    id: options.id,
    title: options.title ?? idToTitle(options.id),
    lens: options.lens ?? 'da Vinci',
    status: options.status ?? 'in-progress',
    type: options.type ?? 'observation',
  };

  const outputPath = `docs/observations/${date}-${resolved.id}.md`;
  tree.write(outputPath, observationTemplate(resolved, date));
  console.log(`created: ${outputPath}`);
}
