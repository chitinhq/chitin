import { readFileSync, readdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

export interface DecisionsRecentArgs {
  dir?: string;
  windowHours?: number;
  limit?: number;
}

export interface DecisionEntry {
  allowed: boolean;
  mode: string;
  rule_id: string;
  reason?: string;
  suggestion?: string;
  agent?: string;
  action_type: string;
  action_target: string;
  ts: string;
  envelope_id?: string;
  tier?: number;
  cost_usd?: number;
  input_bytes?: number;
  tool_calls?: number;
}

function resolveDir(dir?: string): string {
  if (dir) return dir;
  // The Go kernel resolves the chitin state dir via $CHITIN_HOME; using
  // any other env name here would let the MCP server read from a
  // different directory than `chitin-kernel` writes to. Stay aligned.
  const fromEnv = process.env['CHITIN_HOME'];
  if (fromEnv) return fromEnv;
  return join(homedir(), '.chitin');
}

export function decisionsRecentTool(args: DecisionsRecentArgs): DecisionEntry[] {
  const dir = resolveDir(args.dir);
  const windowHours = args.windowHours ?? 24;
  const limit = args.limit ?? 100;
  const cutoffMs = Date.now() - windowHours * 3_600_000;

  let files: string[];
  try {
    files = readdirSync(dir)
      .filter((f) => f.startsWith('gov-decisions-') && f.endsWith('.jsonl'))
      .sort()
      .reverse();
  } catch {
    return [];
  }

  const results: DecisionEntry[] = [];
  for (const file of files) {
    let content: string;
    try {
      content = readFileSync(join(dir, file), 'utf8');
    } catch {
      continue;
    }
    const lines = content.trimEnd().split('\n').filter(Boolean).reverse();
    for (const line of lines) {
      try {
        const d = JSON.parse(line) as DecisionEntry;
        if (new Date(d.ts).getTime() < cutoffMs) {
          // Daily file predates the window — stop scanning earlier files too.
          if (results.length > 0) return results;
          break;
        }
        results.push(d);
        if (results.length >= limit) return results;
      } catch {
        // skip malformed lines
      }
    }
  }
  return results;
}
