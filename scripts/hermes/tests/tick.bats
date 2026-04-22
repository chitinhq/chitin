#!/usr/bin/env bats
# Orchestration tests for scripts/hermes/tick.sh.
# Strategy: prepend scripts/hermes/tests/stubs/ to PATH so all external
# commands (hermes, gh, curl, git, jq) are replaced with shell scripts
# that write deterministic output and log calls to $STUB_LOG.

setup() {
  export TEST_TMPDIR="$(mktemp -d)"
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
  grep -c '^hermes chat --model glm-5.1:cloud' "$STUB_LOG" | grep -qx 1

  # Stage 2 (qwen) never invoked
  ! grep -q 'qwen3-coder' "$STUB_LOG"

  # Stage 3 (prompt-act) never invoked
  ! grep -q 'prompt-act.md' "$STUB_LOG"
}
