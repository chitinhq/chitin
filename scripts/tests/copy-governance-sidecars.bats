#!/usr/bin/env bats
# Regression tests for scripts/copy-governance-sidecars.sh — the
# dispatch-path fix for the worktree `policy_signature_missing`
# deadlock (ticket t_5b665efe).

setup() {
  export TEST_TMPDIR
  TEST_TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t chitin-copy-sidecars)"
  SRC_REPO="$TEST_TMPDIR/src"
  WORKTREE="$TEST_TMPDIR/worktree"
  mkdir -p "$SRC_REPO" "$WORKTREE"
  SCRIPT="$BATS_TEST_DIRNAME/../copy-governance-sidecars.sh"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "copies chitin.yaml.sig from source into worktree" {
  printf 'operator-sig-bytes\n' > "$SRC_REPO/chitin.yaml.sig"

  run "$SCRIPT" "$SRC_REPO" "$WORKTREE"
  [ "$status" -eq 0 ]
  [ -f "$WORKTREE/chitin.yaml.sig" ]
  [ "$(cat "$WORKTREE/chitin.yaml.sig")" = "operator-sig-bytes" ]
}

@test "is idempotent — a pre-existing worktree sidecar is left untouched" {
  printf 'operator-sig\n' > "$SRC_REPO/chitin.yaml.sig"
  printf 'already-here\n' > "$WORKTREE/chitin.yaml.sig"

  run "$SCRIPT" "$SRC_REPO" "$WORKTREE"
  [ "$status" -eq 0 ]
  # the existing sidecar must NOT be overwritten by the source copy
  [ "$(cat "$WORKTREE/chitin.yaml.sig")" = "already-here" ]
}

@test "silently skips when the source repo has no sidecar" {
  run "$SCRIPT" "$SRC_REPO" "$WORKTREE"
  [ "$status" -eq 0 ]
  [ ! -e "$WORKTREE/chitin.yaml.sig" ]
}

@test "exits 2 on wrong argument count" {
  run "$SCRIPT" "$SRC_REPO"
  [ "$status" -eq 2 ]
}

@test "exits 1 when the worktree directory does not exist" {
  printf 'sig\n' > "$SRC_REPO/chitin.yaml.sig"

  run "$SCRIPT" "$SRC_REPO" "$TEST_TMPDIR/does-not-exist"
  [ "$status" -eq 1 ]
}
