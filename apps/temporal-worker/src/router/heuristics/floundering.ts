// Floundering heuristic: detects when an agent is stuck in a loop,
// stalled without progress, or burning budget without effect.
//
// Borrows AutoGen v0.4's termination-condition vocabulary
// (MaxMessage / Stall / Timeout / TokenBudget) — the agent's
// chain events are the substrate; this is pure analysis over
// the chain.
//
// MVP signals:
//   - looping: same tool_name + same target attempted N times in a row
//   - stalled: no commits + no file_writes for max_stall_seconds
//   - over-budget: deferred (needs cost-tracking primitive)
//
// Integration: takes chain events for ONE session as input;
// returns score+reason. Pure function — caller fetches events.

import type { HeuristicScore } from '../types.ts';

/** Minimal chain event shape for floundering analysis. */
export interface ChainEventLite {
  ts: string;
  event_type: string;
  payload?: {
    decision?: string;
    rule_id?: string;
    tool_name?: string;
    action_target?: string;
    action_type?: string;
  };
}

export interface FlounderingThresholds {
  max_loop_count: number;
  max_stall_seconds: number;
}

/**
 * Pure: detect a tool-call loop. "Same tool_name with same target,
 * N times in a row, in the most recent events" → loop.
 */
function detectLoop(
  events: ChainEventLite[],
  maxLoopCount: number,
): { detected: boolean; reason: string } {
  if (events.length < maxLoopCount) {
    return { detected: false, reason: '' };
  }
  // Loop = same tool_name AND same NON-EMPTY action_target N times in
  // a row. Requiring a non-empty target avoids false positives when
  // the agent makes multiple distinct calls of the same tool that
  // happen to lack a target field (e.g., shell commands without
  // target normalization).
  const recent = events
    .filter(
      (e) =>
        e.event_type === 'decision' &&
        e.payload?.tool_name &&
        typeof e.payload?.action_target === 'string' &&
        e.payload.action_target.length > 0,
    )
    .slice(-maxLoopCount);
  if (recent.length < maxLoopCount) {
    return { detected: false, reason: '' };
  }
  const sig = (e: ChainEventLite): string =>
    `${e.payload?.tool_name ?? ''}|${e.payload?.action_target ?? ''}`;
  const firstSig = sig(recent[0]);
  const allSame = recent.every((e) => sig(e) === firstSig);
  if (allSame) {
    return {
      detected: true,
      reason: `looping-tool-call:${firstSig.slice(0, 80)}-x${maxLoopCount}`,
    };
  }
  return { detected: false, reason: '' };
}

/**
 * Pure: detect a stall — no file-write decisions in the last
 * maxStallSeconds wall-clock window.
 */
function detectStall(
  events: ChainEventLite[],
  maxStallSeconds: number,
  now: Date,
): { detected: boolean; reason: string } {
  const writeEvents = events.filter(
    (e) =>
      e.event_type === 'decision' &&
      e.payload?.decision === 'allow' &&
      (e.payload?.action_type === 'file.write' ||
        e.payload?.action_type === 'git.commit' ||
        e.payload?.action_type === 'git.push'),
  );
  if (writeEvents.length === 0) {
    // No writes ever — only flag stall if the session has been
    // going long enough to warrant concern
    if (events.length === 0) return { detected: false, reason: '' };
    const firstTs = new Date(events[0].ts);
    const elapsed = (now.getTime() - firstTs.getTime()) / 1000;
    if (elapsed > maxStallSeconds) {
      return {
        detected: true,
        reason: `no-writes-in-${Math.round(elapsed)}s`,
      };
    }
    return { detected: false, reason: '' };
  }
  const lastWrite = new Date(writeEvents[writeEvents.length - 1].ts);
  const elapsed = (now.getTime() - lastWrite.getTime()) / 1000;
  if (elapsed > maxStallSeconds) {
    return {
      detected: true,
      reason: `no-writes-since-${Math.round(elapsed)}s-ago`,
    };
  }
  return { detected: false, reason: '' };
}

/**
 * Pure: detect cascading permission denials — agent keeps trying
 * blocked actions; sign of confusion or floundering.
 */
function detectDenialCascade(
  events: ChainEventLite[],
): { detected: boolean; reason: string } {
  // Last 5 decisions: 4+ denials = cascade
  const recent = events
    .filter((e) => e.event_type === 'decision')
    .slice(-5);
  if (recent.length < 5) return { detected: false, reason: '' };
  const denials = recent.filter((e) => e.payload?.decision === 'deny').length;
  if (denials >= 4) {
    return {
      detected: true,
      reason: `denial-cascade:${denials}-of-last-5`,
    };
  }
  return { detected: false, reason: '' };
}

/**
 * Pure: combined floundering detector. Returns the FIRST signal
 * that fires (priority: loop > stall > denial-cascade).
 */
export function detectFloundering(
  events: ChainEventLite[],
  thresholds: FlounderingThresholds,
  now: Date = new Date(),
): HeuristicScore {
  const loop = detectLoop(events, thresholds.max_loop_count);
  if (loop.detected) {
    return { score: 1.0, fired: true, reason: loop.reason };
  }
  const stall = detectStall(events, thresholds.max_stall_seconds, now);
  if (stall.detected) {
    return { score: 0.85, fired: true, reason: stall.reason };
  }
  const cascade = detectDenialCascade(events);
  if (cascade.detected) {
    return { score: 0.9, fired: true, reason: cascade.reason };
  }
  return { score: 0.0, fired: false, reason: 'no-signals' };
}
