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
  mkdir -p "$CHITIN_SINK_ROOT/ticks"
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
