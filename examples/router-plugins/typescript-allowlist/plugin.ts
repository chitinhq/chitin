#!/usr/bin/env node
/**
 * Example chitin router heuristic plugin (TypeScript via node
 * --experimental-strip-types).
 *
 * Reads a single JSON line from stdin:
 *   { "hook_input": {...}, "config": {...} }
 *
 * Writes a single JSON line to stdout:
 *   { "score": 0.0-1.0, "fired": bool, "reason": "...", "axis": {...} }
 *
 * This example: a "tool-call-rate-limit" heuristic. Fires when the
 * agent has made more than `max_calls_per_minute` tool calls in
 * the last 60 seconds. Operator caps tool-call rate per session.
 *
 * Operator chitin.yaml:
 *
 *   router:
 *     plugins:
 *       - name: tool-call-rate-limit
 *         type: heuristic
 *         runtime: node
 *         module: examples/router-plugins/typescript-allowlist/plugin.ts
 *         config:
 *           max_calls_per_minute: 30
 *         timeout_ms: 2000
 *
 * Cold start: ~500ms (Node). For latency-sensitive heuristics,
 * prefer Python (~50-100ms) or in-tree Go (~10ms via the GoSDK).
 */
import { readFileSync, existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

interface Input {
  hook_input: {
    tool_name?: string;
    session_id?: string;
  };
  config: {
    max_calls_per_minute?: number;
  };
}

interface Output {
  score: number;
  fired: boolean;
  reason: string;
  axis?: Record<string, unknown>;
}

function emit(o: Output): void {
  console.log(JSON.stringify(o));
}

function main(): void {
  const raw = readFileSync(0, 'utf8');
  let payload: Input;
  try {
    payload = JSON.parse(raw);
  } catch (e) {
    emit({ score: 0, fired: false, reason: `plugin-bad-stdin:${(e as Error).message}` });
    return;
  }

  const cap = payload.config.max_calls_per_minute ?? 30;
  const sid = payload.hook_input.session_id;
  if (!sid) {
    emit({ score: 0, fired: false, reason: 'no-session-id' });
    return;
  }

  // Count tool-call decisions in the last 60s for this session
  const path = join(homedir(), '.chitin', `events-${sid}.jsonl`);
  if (!existsSync(path)) {
    emit({ score: 0, fired: false, reason: 'no-chain-yet' });
    return;
  }
  const cutoff = Date.now() - 60_000;
  let count = 0;
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    if (!line.trim()) continue;
    try {
      const ev = JSON.parse(line) as { ts?: string; event_type?: string; payload?: { tool_name?: string } };
      if (ev.event_type !== 'decision') continue;
      if (!ev.payload?.tool_name) continue;
      if (!ev.ts) continue;
      const ts = new Date(ev.ts).getTime();
      if (ts >= cutoff) count++;
    } catch {
      /* skip malformed line */
    }
  }

  const fired = count > cap;
  const score = fired ? Math.min(1.0, count / (cap * 2)) : count / cap;
  emit({
    score: Number(score.toFixed(3)),
    fired,
    reason: fired
      ? `rate-limit-exceeded:${count}-calls-in-60s-cap-${cap}`
      : `under-rate-limit:${count}-calls-in-60s-cap-${cap}`,
    axis: { count_60s: count, cap },
  });
}

main();
