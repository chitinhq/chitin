#!/usr/bin/env bats

setup() {
  export TEST_TMPDIR
  TEST_TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t chitin-agent-unlock)"
  export HOME="$TEST_TMPDIR/home"
  export CHITIN_HOME="$TEST_TMPDIR/chitin"
  export CHITIN_UNLOCK_LOG="$TEST_TMPDIR/agent-unlock.log"
  export STUB_LOG="$TEST_TMPDIR/stub-calls.log"
  export CHITIN_KERNEL_BIN="chitin-kernel"
  export PATH="$BATS_TEST_DIRNAME/stubs:$PATH"

  mkdir -p "$HOME" "$CHITIN_HOME"
  : > "$STUB_LOG"
  : > "$CHITIN_HOME/gov.db"
  : > "$CHITIN_HOME/gov-decisions-$(date -u +%Y-%m-%d).jsonl"

  export STUB_LOCKED_ROWS=""
  export STUB_TOTAL_POLICY_DENIALS=0
  export STUB_JQ_POLICY_COUNT=0
  export STUB_JQ_INFRA_COUNT=0
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "eligible infra-only lock resets agent and emits audit event" {
  export STUB_LOCKED_ROWS="agent-1|2000-01-01T00:00:00Z"
  export STUB_TOTAL_POLICY_DENIALS=0
  export STUB_JQ_POLICY_COUNT=0
  export STUB_JQ_INFRA_COUNT=2

  run "$BATS_TEST_DIRNAME/../chitin-agent-unlock.sh"
  [ "$status" -eq 0 ]

  grep -q 'chitin-kernel gate reset --agent=agent-1' "$STUB_LOG"
  grep -q 'chitin-kernel emit --dir '"$CHITIN_HOME"' --event-file ' "$STUB_LOG"
  grep -q '"kind":"ok"' "$CHITIN_UNLOCK_LOG"
  grep -q '"msg":"agent-unlocked-auto"' "$CHITIN_UNLOCK_LOG"
}

@test "recent policy denials keep the agent locked" {
  export STUB_LOCKED_ROWS="agent-2|2000-01-01T00:00:00Z"
  export STUB_TOTAL_POLICY_DENIALS=0
  export STUB_JQ_POLICY_COUNT=1
  export STUB_JQ_INFRA_COUNT=0

  run "$BATS_TEST_DIRNAME/../chitin-agent-unlock.sh"
  [ "$status" -eq 0 ]

  ! grep -q 'chitin-kernel gate reset --agent=agent-2' "$STUB_LOG"
  ! grep -q 'chitin-kernel emit --dir '"$CHITIN_HOME"' --event-file ' "$STUB_LOG"
  grep -q '"kind":"skip"' "$CHITIN_UNLOCK_LOG"
  grep -q '"msg":"recent-policy-denials"' "$CHITIN_UNLOCK_LOG"
}

@test "lifetime policy denials block auto-reset even after a quiet window" {
  export STUB_LOCKED_ROWS="agent-3|2000-01-01T00:00:00Z"
  export STUB_TOTAL_POLICY_DENIALS=4
  export STUB_JQ_POLICY_COUNT=0
  export STUB_JQ_INFRA_COUNT=0

  run "$BATS_TEST_DIRNAME/../chitin-agent-unlock.sh"
  [ "$status" -eq 0 ]

  ! grep -q 'chitin-kernel gate reset --agent=agent-3' "$STUB_LOG"
  ! grep -q 'chitin-kernel emit --dir '"$CHITIN_HOME"' --event-file ' "$STUB_LOG"
  grep -q '"kind":"skip"' "$CHITIN_UNLOCK_LOG"
  grep -q '"msg":"policy-denials-in-lifetime"' "$CHITIN_UNLOCK_LOG"
}
