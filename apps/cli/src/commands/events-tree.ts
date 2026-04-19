import type { Command } from 'commander';

export interface EventRow {
  chain_id: string;
  chain_type: string;
  parent_chain_id: string | null;
  seq: number;
  event_type: string;
  ts: string;
}

export interface ChainNode {
  chainID: string;
  chainType: string;
  events: EventRow[];
  children: ChainNode[];
}

export interface Tree {
  chains: ChainNode[];
}

export function buildTree(events: EventRow[]): Tree {
  const byChain = new Map<string, ChainNode>();
  for (const e of events) {
    let node = byChain.get(e.chain_id);
    if (!node) {
      node = { chainID: e.chain_id, chainType: e.chain_type, events: [], children: [] };
      byChain.set(e.chain_id, node);
    }
    node.events.push(e);
  }
  for (const node of byChain.values()) {
    node.events.sort((a, b) => a.seq - b.seq);
  }
  const roots: ChainNode[] = [];
  for (const e of events) {
    const node = byChain.get(e.chain_id)!;
    if (e.parent_chain_id === null) {
      if (!roots.includes(node)) roots.push(node);
    } else {
      const parent = byChain.get(e.parent_chain_id);
      if (parent && !parent.children.includes(node)) parent.children.push(node);
    }
  }
  return { chains: roots };
}

export function renderTree(tree: Tree): string {
  const lines: string[] = [];
  for (const root of tree.chains) renderChain(root, 0, lines);
  return lines.join('\n');
}

function renderChain(chain: ChainNode, depth: number, out: string[]): void {
  const pad = '  '.repeat(depth);
  out.push(`${pad}chain ${chain.chainID} (${chain.chainType})`);
  for (const e of chain.events) {
    out.push(`${pad}  [${e.seq}] ${e.event_type} @ ${e.ts}`);
  }
  for (const child of chain.children) renderChain(child, depth + 1, out);
}

export function registerEventsTree(program: Command): void {
  program
    .command('events:tree')
    .description('Render a session as a nested chain tree')
    .argument('<session_id>', 'session id to render')
    .option('--chitin-dir <path>', 'chitin state dir', './.chitin')
    .action(async (sessionID: string, opts: { chitinDir: string }) => {
      const { getEventsBySession } = await import('@chitin/telemetry');
      const rows = await getEventsBySession(opts.chitinDir, sessionID);
      const tree = buildTree(rows as EventRow[]);
      process.stdout.write(renderTree(tree) + '\n');
    });
}
