#!/usr/bin/env bash
# Idempotent rebuild + reinstall of chitin-kernel. Designed to be
# triggered by chitin-kernel-redeploy.timer every 15 minutes, but
# safe to run manually at any time.
#
# Closes the deploy-lag pattern documented in
# docs/observations/2026-05-03-low-success-alarm-investigation.md:
# kernel/policy fixes can sit in `main` for hours-to-days because
# nobody manually rebuilds, and the swarm runs against a stale
# binary. This script narrows the window to ~15 minutes.
#
# (Re-shipping after PR #222's merge dropped the implementation
# files; only the docs landed. This PR ships the actual code.)
#
# Decision tree:
#   1. fetch origin/main; if behind, pull.
#   2. if either (a) the new commits touch go/ or chitin.yaml OR
#      (b) the binary is older than tracked sources → rebuild.
#   3. smoke-test the freshly-built binary; on failure, roll back
#      to the prior binary (saved aside in $BIN.prev) and exit
#      non-zero so the systemd unit reports failure.
#   4. log structured JSON to ~/.cache/chitin/install-kernel.jsonl
#      (one line per run) for grep-ability.
#
# Exit codes:
#   0  no-op (no relevant changes since last run)
#   0  successful rebuild + smoke pass
#   1  pull conflict — operator must resolve manually
#   2  build failure — operator must investigate; rollback attempted
#   3  smoke failure, rollback succeeded — fix in main is bad; revert
#   4  smoke failure AND rollback failed — binary in undefined state

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="${CHITIN_REPO:-$(python3 "$REPO_ROOT/swarm/bin/board_resolver.py" workspace)}"
BIN="${CHITIN_KERNEL_BIN:-$HOME/.local/bin/chitin-kernel}"
PREV="$BIN.prev"
LOG="${CHITIN_INSTALL_KERNEL_LOG:-$HOME/.cache/chitin/install-kernel.jsonl}"

mkdir -p "$(dirname "$LOG")" "$(dirname "$BIN")"

# Ensure `go` is on PATH. Under systemd --user (the production caller),
# the inherited PATH often misses /usr/local/go/bin and ~/go/bin even
# though the operator's interactive shell has them. The result was
# 13h of failed redeploys logged as "build-failed" on 2026-05-04 with
# the build actually fine — `go` simply wasn't reachable. Probe in
# order: existing PATH → /usr/local/go/bin → $HOME/go/bin. Fail with
# a structured chain log if none works so the caller sees the cause
# instead of a generic exit-2.
ensure_go_on_path() {
  if command -v go >/dev/null 2>&1; then return 0; fi
  local candidate_dirs="${CHITIN_GO_CANDIDATES:-/usr/local/go/bin:$HOME/go/bin}"
  local IFS=:
  local candidates=()
  read -r -a candidates <<< "$candidate_dirs"
  for candidate in "${candidates[@]}"; do
    if [[ -x "$candidate/go" ]]; then
      export PATH="$candidate:$PATH"
      return 0
    fi
  done
  emit fail go-not-found "searched=PATH,$candidate_dirs"
  return 1
}
ensure_go_on_path || exit 2

emit() {
  local kind="$1" msg="$2"
  shift 2
  local extras=""
  while (($#)); do
    local k="${1%%=*}" v="${1#*=}"
    v="${v//\\/\\\\}"
    v="${v//\"/\\\"}"
    extras+=",\"${k}\":\"${v}\""
    shift
  done
  local line
  line=$(printf '{"ts":"%s","kind":"%s","msg":"%s"%s}' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$kind" "$msg" "$extras")
  printf '%s\n' "$line" | tee -a "$LOG" >&2
}

# Fast-forward the current branch to the already-fetched origin/main.
#
# Replaces `git pull --ff-only --autostash origin main` (research Decision 5,
# spec 083 US2). `git pull origin main` can abort with "Cannot fast-forward to
# multiple branches" when FETCH_HEAD carries more than one merge candidate;
# `git merge --ff-only origin/main` takes exactly one explicit ref and cannot.
#
# The autostash that protected service-written uncommitted docs (roadmap.md,
# swarm-lessons.md — tracked files those services overwrite without committing)
# is reproduced explicitly: a stash-pop conflict is surfaced as `emit fail`
# instead of being silently swallowed the way `git pull --autostash` hides it.
# The stash is left in place on any pop failure so the operator can recover it.
#
# Emits its own failure reason and returns non-zero; the caller just exits 1.
ff_merge_origin_main() {
  local stashed=0
  if ! git diff --quiet HEAD 2>/dev/null; then
    if git stash push --quiet --message "install-kernel autostash $(date -u +%Y-%m-%dT%H:%M:%SZ)"; then
      stashed=1
    else
      emit fail autostash-failed "old_sha=$old_sha" "new_sha=$new_sha"
      return 1
    fi
  fi
  if ! git merge --ff-only --quiet origin/main; then
    emit fail pull-conflict "old_sha=$old_sha" "new_sha=$new_sha"
    if [[ $stashed -eq 1 ]]; then
      if git stash pop --quiet; then
        emit ok autostash-restored-after-conflict
      else
        emit fail autostash-pop-failed-after-conflict "stash kept — operator must resolve"
      fi
    fi
    return 1
  fi
  if [[ $stashed -eq 1 ]] && ! git stash pop --quiet; then
    emit fail autostash-pop-conflict "merge succeeded but stash pop conflicts — stash kept, operator must resolve"
    return 1
  fi
  return 0
}

if ! cd "$REPO" 2>/dev/null; then
  emit fail chdir-failed "repo=$REPO"
  exit 1
fi

# Ensure we're on `main` before pulling. Cron scripts (e.g. the
# chitin-shipped-entry-flipper) can leave the working tree on a branch
# they created; `git pull --ff-only` against an unrelated branch fails.
#
# A *clean* switch to main is safe — it means the prior branch carried
# no uncommitted work, so nothing is lost. But if the switch is blocked
# by uncommitted changes, an operator or an agent is actively working
# in this checkout. Do NOT stash their work out from under them: that
# silently moves HEAD and buries an entire feature branch's worth of
# in-progress work in a stash. Constitution §2 makes the operator's
# primary checkout sacred — automation runs in dedicated worktrees,
# never by hijacking the working tree. Defer the redeploy instead; the
# timer retries in ~15 min and proceeds once the tree is back on a
# clean main. (Uncommitted service-written docs on main itself are
# still handled below by `git pull --autostash`.)
current_branch=$(git rev-parse --abbrev-ref HEAD)
if [[ "$current_branch" != "main" ]]; then
  if git -c advice.detachedHead=false checkout main 2>/dev/null; then
    emit ok auto-switched-to-main "from=$current_branch"
  else
    emit deferred operator-working-tree-active "branch=$current_branch"
    exit 0
  fi
fi

old_sha=$(git rev-parse HEAD)
if ! git fetch --quiet origin main; then
  emit fail fetch-failed "old_sha=$old_sha"
  exit 1
fi
new_sha=$(git rev-parse origin/main)

need_rebuild=0
relevant_changes_since_last="(none)"

if [[ "$old_sha" != "$new_sha" ]]; then
  if git diff --quiet "$old_sha" "$new_sha" -- go/ chitin.yaml; then
    # No kernel-relevant changes — fast-forward to origin/main and stop.
    if ! ff_merge_origin_main; then
      exit 1
    fi
    emit noop "no kernel-relevant changes" "old_sha=$old_sha" "new_sha=$new_sha"
    exit 0
  else
    # Kernel-relevant changes — fast-forward to origin/main, then rebuild.
    if ! ff_merge_origin_main; then
      exit 1
    fi
    need_rebuild=1
    relevant_changes_since_last=$(git diff --name-only "$old_sha" "$new_sha" -- go/ chitin.yaml | tr '\n' ',' | sed 's/,$//')
  fi
fi

if [[ $need_rebuild -eq 0 ]]; then
  if [[ ! -x "$BIN" ]]; then
    need_rebuild=1
    relevant_changes_since_last="binary-missing"
  elif find go chitin.yaml -newer "$BIN" -print -quit 2>/dev/null | grep -q .; then
    need_rebuild=1
    relevant_changes_since_last="binary-stale-relative-to-source"
  fi
fi

if [[ $need_rebuild -eq 0 ]]; then
  emit noop "no rebuild needed" "old_sha=$old_sha"
  exit 0
fi

# Save prev binary for rollback
if [[ -x "$BIN" ]]; then
  cp -a "$BIN" "$PREV"
fi

# Build. Chitin's go module is nested at go/execution-kernel/go.mod
# (no top-level go.mod), so `go build` runs from inside that module.
build_start_ns=$(date +%s%N)
if ! ( cd "$REPO/go/execution-kernel" && go build -o "$BIN" ./cmd/chitin-kernel ) 2>&1; then
  emit fail build-failed "old_sha=$old_sha" "new_sha=$new_sha"
  if [[ -x "$PREV" ]]; then
    cp -a "$PREV" "$BIN"
    emit rollback build-fail-rollback-success "restored_from=$PREV"
  fi
  exit 2
fi
build_dur_ms=$(( ($(date +%s%N) - build_start_ns) / 1000000 ))

# Smoke-test: a `Task` PreToolUse evaluate must exit 0.
smoke_payload=$(printf '{"hook_event_name":"PreToolUse","tool_name":"Task","tool_input":{"description":"redeploy-smoke"},"cwd":"%s","session_id":"redeploy-smoke"}' "$REPO")
if ! echo "$smoke_payload" | timeout 2 "$BIN" gate evaluate --hook-stdin --agent=redeploy-smoke >/dev/null 2>&1; then
  emit fail smoke-failed "new_sha=$new_sha" "build_dur_ms=$build_dur_ms"
  if [[ -x "$PREV" ]] && cp -a "$PREV" "$BIN"; then
    emit rollback smoke-rollback-success "restored_from=$PREV"
    exit 3
  else
    emit fail smoke-rollback-failed-binary-undefined "expected_prev=$PREV"
    exit 4
  fi
fi

# Refresh per-agent hook wiring after a successful kernel install.
# Each installer is idempotent and falls open on missing deps;
# failures are logged but don't abort the redeploy. Stdout/stderr
# captured so failure mode is actually inspectable in the
# structured log.
for installer in \
  "$REPO/scripts/install-claude-code-hook.sh" \
  "$REPO/scripts/install-gemini-hook.sh" \
  "$REPO/scripts/install-codex-hook.sh" \
  "$REPO/scripts/install-hermes-hook.sh"; do
  [[ -x "$installer" ]] || continue
  hook_log=$(mktemp)
  if "$installer" >"$hook_log" 2>&1; then
    emit ok hook-installed "installer=$(basename "$installer")"
  else
    tail=$(tail -c 500 "$hook_log" | tr '\n' ' ' | tr -d '"' || true)
    emit warn hook-install-failed "installer=$(basename "$installer") tail=$tail"
  fi
  rm -f "$hook_log"
done

# Sync systemd user units with infra/systemd/. New units (e.g. a
# freshly-merged chitin-foo.timer) get auto-enabled here so the
# install step matches the merge step — closes the 2026-05-04
# pattern where PR #282 shipped chitin-agent-unlock.{service,timer}
# but the operator never `cp`'d + `enable`'d them, missing a 20.5h
# auto-recovery window. Idempotent; existing timers' enable state
# is preserved (operator-disabled units stay disabled).
INSTALL_UNITS="$REPO/scripts/install-systemd-units.sh"
if [[ -x "$INSTALL_UNITS" ]]; then
  units_log=$(mktemp)
  if "$INSTALL_UNITS" >"$units_log" 2>&1; then
    emit ok systemd-units-synced
  else
    tail=$(tail -c 500 "$units_log" | tr '\n' ' ' | tr -d '"' || true)
    emit warn systemd-units-sync-failed "tail=$tail"
  fi
  rm -f "$units_log"
fi

# Rotate the budget envelope if the active one closed. Without
# this, a sticky-closed envelope deny-cascades every tool call
# until manual rotation. Idempotent; no-op when the active
# envelope is open. See scripts/chitin-envelope-rotate.sh and
# the chitin-envelope-rotate.timer for the periodic mechanism.
ROTATOR="$REPO/scripts/chitin-envelope-rotate.sh"
if [[ -x "$ROTATOR" ]]; then
  rotator_log=$(mktemp)
  if "$ROTATOR" >"$rotator_log" 2>&1; then
    emit ok envelope-rotate-checked
  else
    tail=$(tail -c 500 "$rotator_log" | tr '\n' ' ' | tr -d '"' || true)
    emit warn envelope-rotate-failed "tail=$tail"
  fi
  rm -f "$rotator_log"
fi

emit ok redeploy-success "old_sha=$old_sha" "new_sha=$new_sha" "build_dur_ms=$build_dur_ms" "changed=$relevant_changes_since_last"

# (apps/runner/src/ TS worker restart logic removed 2026-05-08 — the
# TS dispatcher/worker source was culled in the orchestration deletion
# wave. The Go kernel rebuild above is the only artifact this script
# ships now; hermes/openclaw drivers manage their own process lifecycle.)

exit 0
