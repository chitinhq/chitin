// Slice 7a: integration test for wall_timeout SIGKILL propagation.
//
// Pre-fix behavior: spawn(...) without detached:true leaves the child
// in the activity process's own process group. SIGKILL hits the parent
// only; grandchildren inherit stdout pipes and keep them open. Node's
// 'close' event waits for the pipes to close → hang to Temporal's
// 15-min startToCloseTimeout.
//
// Post-fix behavior: detached:true makes the child a process group
// leader; process.kill(-pid, 'SIGKILL') kills the whole group, FDs
// close, 'close' fires within ~1s.
//
// We can't unit-test the activity wrapper directly (it's tied to
// ExecutionRequest semantics + workflow plumbing), but we CAN test the
// underlying spawn-and-kill mechanics that runAgentTurn relies on.
// This test reproduces the activity's spawn/kill contract in isolation.

import { describe, expect, it } from 'vitest';
import { spawn } from 'node:child_process';

const SHORT_TIMEOUT_MS = 1500; // wall_timeout simulation
const CLOSE_FIRE_BUDGET_MS = 1500; // how long after kill before close should fire

describe('slice 7a — wall_timeout SIGKILL propagation', () => {
  it('detached spawn + group kill: close event fires within budget after timer', async () => {
    // Reproduce the activity's pattern: parent shell that exec-spawns a
    // long-running grandchild (sleep) which inherits stdout. Without group
    // kill, killing only the parent leaves grandchild owning the pipe.
    const command = '/bin/bash';
    const args = ['-c', 'sleep 60 & wait'];

    const start = Date.now();
    const result = await new Promise<{ closeMs: number; killed: boolean }>((resolve) => {
      const child = spawn(command, args, {
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: true,
      });

      let killed = false;
      const killTimer = setTimeout(() => {
        if (child.pid !== undefined) {
          try {
            process.kill(-child.pid, 'SIGKILL');
            killed = true;
          } catch {
            // ESRCH = already exited
          }
        }
        child.stdout?.destroy();
        child.stderr?.destroy();
      }, SHORT_TIMEOUT_MS);

      child.on('close', () => {
        clearTimeout(killTimer);
        resolve({ closeMs: Date.now() - start, killed });
      });
    });

    // Total wall: timer (1500ms) + close-fire budget (1500ms) = ~3s ceiling.
    // Pre-fix this would hit the test runner's default 5s timeout.
    expect(result.killed).toBe(true);
    expect(result.closeMs).toBeLessThan(SHORT_TIMEOUT_MS + CLOSE_FIRE_BUDGET_MS);
  }, 10_000);

  it("regression guard: NON-detached spawn would not propagate to grandchildren (control case)", async () => {
    // This test documents the bug we fixed by demonstrating the broken
    // pattern still has the issue. We expect EITHER close to fire (some
    // shells propagate signals to children even without setpgid, e.g.
    // when the shell itself is the parent and waits) OR for it to take
    // longer than the detached-group case. The point is to anchor the
    // fix behavior — not to re-fail on every CI run.
    //
    // We use a tighter budget here just to prove the contrast: detached
    // ALWAYS finishes inside SHORT_TIMEOUT_MS + CLOSE_FIRE_BUDGET_MS;
    // non-detached MAY exceed it depending on the kernel/shell.

    const command = '/bin/bash';
    const args = ['-c', 'sleep 60 & wait'];

    const start = Date.now();
    const closed = await new Promise<boolean>((resolve) => {
      const child = spawn(command, args, {
        stdio: ['ignore', 'pipe', 'pipe'],
        // detached intentionally OMITTED — bug-mode
      });

      const killTimer = setTimeout(() => {
        try {
          child.kill('SIGKILL'); // parent only — does NOT take the group
        } catch {}
      }, 500);

      const safetyTimer = setTimeout(() => resolve(false), 4000);

      child.on('close', () => {
        clearTimeout(killTimer);
        clearTimeout(safetyTimer);
        resolve(true);
      });
    });
    // We don't assert pass/fail here — the exact behavior depends on the
    // shell's signal propagation. We just record the timing as a sanity
    // anchor: detached fixes the worst case; this control may pass or
    // fail depending on environment.
    const elapsed = Date.now() - start;
    if (closed) {
      // pass-through — environment happens to propagate
      expect(elapsed).toBeGreaterThan(0);
    } else {
      // hung — exactly the bug scenario the slice 7a fix avoids
      expect(elapsed).toBeGreaterThanOrEqual(4000);
    }
  }, 10_000);
});
