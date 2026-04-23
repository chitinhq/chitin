#!/usr/bin/env bats
# Orchestration tests for scripts/hermes/tick.sh.
# Strategy: prepend scripts/hermes/tests/stubs/ to PATH so all external
# commands (hermes, gh, curl, git, jq) are replaced with shell scripts
# that write deterministic output and log calls to $STUB_LOG.

setup() {
  export TEST_TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t hermes-tick)"
  export STUB_LOG="$TEST_TMPDIR/stub-calls.log"
  export CHITIN_SINK_ROOT="$TEST_TMPDIR/chitin-sink"
  export HERMES_TICK_TS="20260422T000000Z"         # deterministic tick dir name
  export HERMES_TICK_DATE="2026-04-22"
  # Isolate tick-level state that now has env overrides (lock file,
  # worktree base, repo root). Keeps tests from touching the real /tmp
  # lock or ~/workspace. The repo root is where stage 2 runs
  # `git apply --check` — bare dir is enough since `git` is stubbed.
  export HERMES_TICK_LOCK_FILE="$TEST_TMPDIR/hermes-tick.lock"
  export HERMES_TICK_WORKTREE_BASE="$TEST_TMPDIR/worktrees"
  export HERMES_TICK_REPO_ROOT="$TEST_TMPDIR/repo"
  mkdir -p "$CHITIN_SINK_ROOT/ticks" "$HERMES_TICK_WORKTREE_BASE" "$HERMES_TICK_REPO_ROOT"
  : > "$STUB_LOG"

  STUBS="$BATS_TEST_DIRNAME/stubs"
  export PATH="$STUBS:$PATH"

  # Defaults; individual tests can override by exporting STUB_* before run
  export STUB_HERMES_PLAN_OUTPUT='{"action":"skip","issue_number":0,"reason":"no viable targets"}'
  export STUB_CURL_OLLAMA_OK=1
  export STUB_GH_ISSUE_LIST_OUTPUT='[]'
  export STUB_GH_PR_LIST_OUTPUT='[]'
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "skip path: Stage 1 runs, emits skip plan, Stages 2 and 3 do not run" {
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/queue.json" ]
  [ ! -f "$tick_dir/diff.patch" ]
  [ ! -f "$tick_dir/act-log.txt" ]

  # Stage 1 invoked exactly once
  grep -c '^hermes chat -Q --model glm-5.1:cloud' "$STUB_LOG" | grep -qx 1

  # Stage 2 (qwen) never invoked
  ! grep -q 'qwen3-coder' "$STUB_LOG"

  # Stage 3 (ACT) never invoked
  ! grep -q 'Stage 3 (ACT)' "$STUB_LOG"
}

@test "external path: Stage 1 + Stage 3 run, Stage 2 does not" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"external","issue_number":10,"reason":"groom","external_action":{"kind":"label","body_or_label":"hermes-autonomous","linked_issue":10}}'

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/act-log.txt" ]
  [ ! -f "$tick_dir/diff.patch" ]

  grep -q 'Stage 1 (PLAN)' "$STUB_LOG"
  grep -q 'Stage 3 (ACT)'  "$STUB_LOG"
  ! grep -q 'Stage 2 (CODE)' "$STUB_LOG"
  ! grep -q 'qwen3-coder'     "$STUB_LOG"
}

@test "code path + ollama ok: all three stages run; diff.patch written" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix ESM import","diff_request":{"files":["apps/cli/src/telemetry/jsonl-tailer.ts"],"intent":"append .js extension"}}'
  export STUB_HERMES_CODE_OUTPUT='--- a/apps/cli/src/telemetry/jsonl-tailer.ts
+++ b/apps/cli/src/telemetry/jsonl-tailer.ts
@@ -1 +1 @@
-import { foo } from "./event-parser";
+import { foo } from "./event-parser.js";'
  export STUB_CURL_OLLAMA_OK=1

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/diff.patch" ]
  [ -f "$tick_dir/act-log.txt" ]
  [ "$(cat "$tick_dir/ollama-probe.txt")" = "ok" ]

  grep -q 'Stage 1 (PLAN)' "$STUB_LOG"
  grep -q 'Stage 2 (CODE)' "$STUB_LOG"
  grep -q 'Stage 3 (ACT)'  "$STUB_LOG"
  grep -q 'qwen3-coder'    "$STUB_LOG"
}

@test "code path + ollama unreachable: Stage 1 runs; Stages 2 & 3 skipped" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix ESM import","diff_request":{"files":["apps/cli/src/telemetry/jsonl-tailer.ts"],"intent":"append .js extension"}}'
  export STUB_CURL_OLLAMA_OK=0

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ ! -f "$tick_dir/diff.patch" ]
  [ ! -f "$tick_dir/act-log.txt" ]
  [ "$(cat "$tick_dir/ollama-probe.txt")" = "unreachable" ]

  ! grep -q 'qwen3-coder' "$STUB_LOG"
  ! grep -q 'Stage 3 (ACT)' "$STUB_LOG"
}

@test "streak: counter increments on each unreachable, resets on reachable" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix","diff_request":{"files":["x.ts"],"intent":"fix"}}'
  streak_file="$CHITIN_SINK_ROOT/ollama-unreachable-streak.txt"

  # Run 1 — unreachable
  export STUB_CURL_OLLAMA_OK=0
  export HERMES_TICK_TS="20260422T000000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]
  [ "$(cat "$streak_file")" = "1" ]

  # Run 2 — unreachable
  export HERMES_TICK_TS="20260422T001000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "2" ]

  # Run 3 — unreachable
  export HERMES_TICK_TS="20260422T002000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "3" ]

  # Run 4 — reachable → reset
  export STUB_CURL_OLLAMA_OK=1
  export HERMES_TICK_TS="20260422T003000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "0" ]
}

@test "dry-run: external action path — stage 3 invoked with HERMES_TICK_DRY_RUN=1 in env" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"external","issue_number":10,"reason":"groom","external_action":{"kind":"label","body_or_label":"hermes-autonomous","linked_issue":10}}'

  run "$BATS_TEST_DIRNAME/../tick.sh" --dry-run
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/act-log.txt" ]
  grep -q 'HERMES_TICK_DRY_RUN=1' "$STUB_LOG"
}

@test "concurrency guard: second tick exits cleanly when lock is held" {
  # Hold the lock in an inherited fd from a subshell that blocks until we
  # let it go. If tick.sh sees the lock busy, it must exit 0 without
  # running any stages.
  ready_file="$TEST_TMPDIR/lock-holder.ready"
  ( flock -x 200; touch "$ready_file"; sleep 5 ) 200>"$HERMES_TICK_LOCK_FILE" &
  lock_holder=$!
  # Wait (up to ~2.5s) for the holder to signal it has the lock.
  for _ in $(seq 1 50); do
    [[ -f "$ready_file" ]] && break
    sleep 0.05
  done
  [[ -f "$ready_file" ]]

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  # No stage 1 invocation, no tick dir contents — the run short-circuited
  # before queue fetch.
  ! grep -q '^hermes chat' "$STUB_LOG"
  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ ! -f "$tick_dir/plan.json" ]

  # The skip message goes to stderr so cron-tick.log captures it.
  echo "$output" | grep -q 'lock held by another run'

  kill "$lock_holder" 2>/dev/null || true
  wait "$lock_holder" 2>/dev/null || true
}

@test "fence strip: stage 2 output wrapped in code fences is stripped before git apply --check" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix ESM","diff_request":{"files":["x.ts"],"intent":"append .js"}}'
  # qwen3-coder:30b emits its diff inside a fenced block despite prompt
  # instructions. The fence lines must be stripped before stage 3.
  export STUB_HERMES_CODE_OUTPUT='```diff
--- a/x.ts
+++ b/x.ts
@@ -1 +1 @@
-import { foo } from "./bar";
+import { foo } from "./bar.js";
```'

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  # Fences gone, diff body preserved.
  ! grep -q '^```' "$tick_dir/diff.patch"
  grep -q '^--- a/x.ts' "$tick_dir/diff.patch"
  grep -q '^+++ b/x.ts' "$tick_dir/diff.patch"
  # Stage 2 logged as done (validation passed via git stub apply --check).
  grep -q 'stage 2 done' "$tick_dir/tick.log"
}

@test "invalid diff: git apply --check failure short-circuits before stage 3" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix","diff_request":{"files":["x.ts"],"intent":"fix"}}'
  export STUB_HERMES_CODE_OUTPUT='not a valid diff at all'
  export STUB_GIT_APPLY_CHECK_FAIL=1

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  grep -q 'stage 2 invalid diff' "$tick_dir/tick.log"
  # Stage 3 never ran — no act-log.
  [ ! -f "$tick_dir/act-log.txt" ]
  ! grep -q 'Stage 3 (ACT)' "$STUB_LOG"
}

@test "in-flight worktree: pre-existing chitin-N dir causes code action to skip" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix","diff_request":{"files":["x.ts"],"intent":"fix"}}'
  # A worktree already exists for issue #10 — likely from an earlier tick
  # still finishing. Skip rather than duplicate the attempt.
  mkdir -p "$HERMES_TICK_WORKTREE_BASE/chitin-10"

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  grep -q 'in_flight_local' "$tick_dir/tick.log"
  # Stage 2 never ran.
  [ ! -f "$tick_dir/diff.patch" ]
  ! grep -q 'qwen3-coder' "$STUB_LOG"
}
