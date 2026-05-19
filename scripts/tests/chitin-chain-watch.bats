#!/usr/bin/env bats

setup() {
  export TEST_TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t chain-watch)"
  export HOME="$TEST_TMPDIR/home"
  export CHITIN_HOME="$TEST_TMPDIR/chitin"
  export CHITIN_CHAIN_WATCH_LOG="$TEST_TMPDIR/chain-watch.jsonl"
  export CHITIN_KERNEL_BIN="$TEST_TMPDIR/stubs/chitin-kernel"
  export STUB_KERNEL_LOG="$TEST_TMPDIR/kernel-calls.log"
  export STUB_EMIT_CAPTURE="$TEST_TMPDIR/emitted-event.json"
  export STUB_DATE_NOW_TS="2026-05-16T12:00:00Z"
  export STUB_DATE_TODAY="2026-05-16"
  export STUB_DATE_CUTOFF_TS="2026-05-16T11:58:30Z"
  export REAL_JQ="$(command -v jq)"

  mkdir -p "$HOME" "$CHITIN_HOME" "$TEST_TMPDIR/stubs"
  : > "$STUB_KERNEL_LOG"

  cp "$BATS_TEST_DIRNAME/stubs/chitin-kernel" "$TEST_TMPDIR/stubs/chitin-kernel"
  cp "$BATS_TEST_DIRNAME/stubs/date" "$TEST_TMPDIR/stubs/date"
  chmod +x "$TEST_TMPDIR/stubs/chitin-kernel" "$TEST_TMPDIR/stubs/date"

  export PATH="$TEST_TMPDIR/stubs:$PATH"
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

write_chain_log() {
  cat > "$CHITIN_HOME/gov-decisions-${STUB_DATE_TODAY}.jsonl"
}

@test "no chain log emits noop and exits 0" {
  run "$BATS_TEST_DIRNAME/../chitin-chain-watch.sh"
  [ "$status" -eq 0 ]

  grep -q '"kind":"noop"' "$CHITIN_CHAIN_WATCH_LOG"
  grep -q 'no-chain-log' "$CHITIN_CHAIN_WATCH_LOG"
  [ ! -s "$STUB_KERNEL_LOG" ]
}

@test "warn path emits one alert and one chain event for the breached agent" {
  write_chain_log <<'EOF'
{"rule_id":"lockdown","ts":"2026-05-16T11:59:00Z","agent":"codex"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:20Z","agent":"codex"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:40Z","agent":"codex"}
{"rule_id":"lockdown","ts":"2026-05-16T11:57:00Z","agent":"codex"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:10Z","agent":"gemini"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:30Z","agent":"gemini"}
{"rule_id":"other","ts":"2026-05-16T11:59:50Z","agent":"codex"}
EOF

  run "$BATS_TEST_DIRNAME/../chitin-chain-watch.sh"
  [ "$status" -eq 0 ]

  grep -q '^emit --dir ' "$STUB_KERNEL_LOG"
  ! grep -q 'gate reset' "$STUB_KERNEL_LOG"
  grep -q 'lockdown-burst-detected' "$CHITIN_CHAIN_WATCH_LOG"
  grep -q '"agent":"codex"' "$CHITIN_CHAIN_WATCH_LOG"
  grep -q '"count":"3"' "$CHITIN_CHAIN_WATCH_LOG"
  grep -q '"action":"warned"' "$CHITIN_CHAIN_WATCH_LOG"
  "$REAL_JQ" -e '.event_type == "lockdown_loop_detected"' "$STUB_EMIT_CAPTURE" >/dev/null
  "$REAL_JQ" -e '.payload.agent == "codex" and .payload.lockdown_count == 3 and .payload.action == "warned"' "$STUB_EMIT_CAPTURE" >/dev/null
}

@test "reset path calls gate reset and records reset action" {
  export CHITIN_CHAIN_WATCH_ACTION="reset"
  write_chain_log <<'EOF'
{"rule_id":"lockdown","ts":"2026-05-16T11:59:00Z","agent":"hermes"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:20Z","agent":"hermes"}
{"rule_id":"lockdown","ts":"2026-05-16T11:59:40Z","agent":"hermes"}
EOF

  run "$BATS_TEST_DIRNAME/../chitin-chain-watch.sh"
  [ "$status" -eq 0 ]

  grep -q '^gate reset --agent=hermes$' "$STUB_KERNEL_LOG"
  grep -q '"action":"reset"' "$CHITIN_CHAIN_WATCH_LOG"
  "$REAL_JQ" -e '.payload.action == "reset"' "$STUB_EMIT_CAPTURE" >/dev/null
}
