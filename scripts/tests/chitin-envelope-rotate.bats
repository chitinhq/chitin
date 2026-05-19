#!/usr/bin/env bats

setup() {
  export TEST_TMPDIR
  TEST_TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t chitin-envelope-rotate)"
  export HOME="$TEST_TMPDIR/home"
  export CHITIN_HOME="$TEST_TMPDIR/chitin-home"
  export CHITIN_ENVELOPE_ROTATE_LOG="$TEST_TMPDIR/envelope-rotate.jsonl"
  export STUB_LOG="$TEST_TMPDIR/stub-calls.log"

  mkdir -p "$HOME" "$CHITIN_HOME"
  : > "$STUB_LOG"

  export PATH="$BATS_TEST_DIRNAME/stubs:$PATH"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "open current envelope exits without rotation" {
  printf 'ENVOPEN1\n' > "$CHITIN_HOME/current-envelope"
  export STUB_CHITIN_ENVELOPE_LIST_OUTPUT='[{"id":"ENVOPEN1","closed_at":null}]'

  run "$BATS_TEST_DIRNAME/../chitin-envelope-rotate.sh"
  [ "$status" -eq 0 ]

  [ "$(tr -d '[:space:]' < "$CHITIN_HOME/current-envelope")" = "ENVOPEN1" ]
  grep -q '^envelope list --limit=0$' "$STUB_LOG"
  ! grep -q '^envelope create ' "$STUB_LOG"
  ! grep -q '^envelope use ' "$STUB_LOG"
  grep -q '"kind":"noop"' "$CHITIN_ENVELOPE_ROTATE_LOG"
}

@test "list failure preserves pointer and skips rotation" {
  printf 'ENVKEEP1\n' > "$CHITIN_HOME/current-envelope"
  export STUB_CHITIN_ENVELOPE_LIST_RC=1
  export STUB_CHITIN_ENVELOPE_LIST_STDERR='sqlite busy'

  run "$BATS_TEST_DIRNAME/../chitin-envelope-rotate.sh"
  [ "$status" -eq 0 ]

  [ "$(tr -d '[:space:]' < "$CHITIN_HOME/current-envelope")" = "ENVKEEP1" ]
  grep -q '^envelope list --limit=0$' "$STUB_LOG"
  ! grep -q '^envelope create ' "$STUB_LOG"
  ! grep -q '^envelope use ' "$STUB_LOG"
  grep -q '"kind":"skip"' "$CHITIN_ENVELOPE_ROTATE_LOG"
}

@test "closed current envelope rotates and updates pointer through envelope use" {
  printf 'OLDENV1\n' > "$CHITIN_HOME/current-envelope"
  export STUB_CHITIN_ENVELOPE_LIST_OUTPUT='[{"id":"OLDENV1","closed_at":"2026-05-16T00:00:00Z"}]'
  export STUB_CHITIN_ENVELOPE_CREATE_OUTPUT='NEWENV1'
  export CHITIN_ENVELOPE_CALLS=77
  export CHITIN_ENVELOPE_BYTES=2048
  export CHITIN_ENVELOPE_USD=4.25

  run "$BATS_TEST_DIRNAME/../chitin-envelope-rotate.sh"
  [ "$status" -eq 0 ]

  [ "$(tr -d '[:space:]' < "$CHITIN_HOME/current-envelope")" = "NEWENV1" ]
  grep -q '^envelope list --limit=0$' "$STUB_LOG"
  grep -q '^envelope create --calls=77 --bytes=2048 --usd=4.25$' "$STUB_LOG"
  grep -q '^envelope use NEWENV1$' "$STUB_LOG"
  grep -q '"kind":"ok"' "$CHITIN_ENVELOPE_ROTATE_LOG"
}

@test "malformed create output fails loudly and does not update pointer" {
  printf 'OLDENV2\n' > "$CHITIN_HOME/current-envelope"
  export STUB_CHITIN_ENVELOPE_LIST_OUTPUT='[{"id":"OLDENV2","closed_at":"2026-05-16T00:00:00Z"}]'
  export STUB_CHITIN_ENVELOPE_CREATE_OUTPUT='not-an-id'

  run "$BATS_TEST_DIRNAME/../chitin-envelope-rotate.sh"
  [ "$status" -eq 3 ]

  [ "$(tr -d '[:space:]' < "$CHITIN_HOME/current-envelope")" = "OLDENV2" ]
  grep -q '^envelope create --calls=10000 --bytes=33554432 --usd=2.0$' "$STUB_LOG"
  ! grep -q '^envelope use ' "$STUB_LOG"
  grep -q '"kind":"fail"' "$CHITIN_ENVELOPE_ROTATE_LOG"
  grep -q 'envelope-create-failed' "$CHITIN_ENVELOPE_ROTATE_LOG"
}
