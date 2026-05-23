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
#   1. fetch origin/main into a DEDICATED worktree (~/.cache/chitin/redeploy-worktree
#      by default). The operator's primary checkout at $REPO is NEVER touched.
#   2. compare the new origin/main sha against the sha stamped next to the
#      installed binary ($BIN.sha). If either (a) the new commits touch go/ or
#      chitin.yaml OR (b) the binary is older than tracked sources → rebuild.
#   3. build from the worktree; smoke-test; on failure roll back to the prior
#      binary (saved aside in $BIN.prev) and exit non-zero so the systemd unit
#      reports failure. Stamp the new commit sha in $BIN.sha on success.
#   4. log structured JSON to ~/.cache/chitin/install-kernel.jsonl (one line
#      per run) for grep-ability.
#
# Constitution §2 compliance (2026-05-23):
#   The pre-2026-05-23 version ran `git checkout main` inside the operator's
#   primary checkout to ensure it was on the right branch before pulling. That
#   violated §2 (operator's primary checkout is sacred — automation runs in
#   dedicated worktrees). The fix moves all git state mutation into a dedicated
#   worktree; $REPO is now read-only-from-this-script. See PR #937 for the
#   git-ops recorder that detected the hijack pattern.
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

# Dedicated worktree for the redeploy build. Detached HEAD on origin/main;
# persistent across runs for speed (avoids re-cloning on every timer fire).
# Per constitution §2: automation NEVER touches the operator's primary
# checkout. All git state mutations happen in this worktree.
#
# Override via CHITIN_KERNEL_REDEPLOY_WORKTREE. Override sparingly — the
# default location is intentional (under ~/.cache so it's reclaimable).
WORKTREE="${CHITIN_KERNEL_REDEPLOY_WORKTREE:-$HOME/.cache/chitin/redeploy-worktree}"

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

# Ensure the dedicated redeploy worktree exists, is healthy, and is reset
# to origin/main. Idempotent — safe to call on every run.
#
# Why a dedicated worktree (constitution §2):
#   The operator's primary checkout at $REPO is sacred. Hijacking it
#   (mid-session `git checkout main` from a timer-triggered script,
#   silently `git stash`-ing the operator's in-progress work) breaks the
#   agent / operator's mental model of "what branch am I on" and burrows
#   load-bearing changes into stash refs. The pre-2026-05-23 version of
#   this script attempted both, was caught by the git-ops recorder at
#   2026-05-23 14:53:14, and is fixed in this commit. See PR #937 for
#   the recorder + investigation, this PR for the canonical §2 fix.
#
# Algorithm:
#   1. If $WORKTREE doesn't exist as a directory → `git worktree add --detach`
#      it from $REPO at origin/main (after a fresh fetch).
#   2. If $WORKTREE exists but isn't recognized by $REPO's git worktree list
#      → remove the stale directory and add it fresh.
#   3. If $WORKTREE exists and IS a healthy worktree → fetch + hard-reset to
#      origin/main inside it.
#
# Returns non-zero on any failure; caller exits 1.
ensure_redeploy_worktree() {
  # Fetch into $REPO's gitdir — but do NOT touch $REPO's working tree or
  # branch refs. `git fetch` is read-only against the working tree; it
  # only updates remote-tracking refs in .git/.
  if ! git -C "$REPO" fetch --quiet origin main; then
    emit fail fetch-failed "phase=ensure-worktree"
    return 1
  fi

  local known_worktree=""
  if [[ -d "$WORKTREE/.git" ]] || [[ -f "$WORKTREE/.git" ]]; then
    # $WORKTREE looks like a git worktree. Verify $REPO knows about it.
    if known_worktree=$(git -C "$REPO" worktree list --porcelain 2>/dev/null \
                        | awk -v w="$WORKTREE" '$1=="worktree" && $2==w {print $2}'); then
      :
    fi
  fi

  if [[ -z "$known_worktree" ]]; then
    # No healthy worktree present. If the directory exists, remove it.
    # SAFETY: require $WORKTREE to be a long enough absolute path
    # containing "chitin" and under $HOME/.cache (or a temp dir) before
    # rm. Defends against an operator override pointing at $HOME or /.
    if [[ -e "$WORKTREE" ]]; then
      case "$WORKTREE" in
        */chitin/*|*/.cache/chitin/*|/tmp/chitin-*)
          : # path looks like a chitin-managed worktree, OK to rm
          ;;
        *)
          emit fail worktree-rm-refused "refusing to rm path that does not match chitin worktree pattern: $WORKTREE"
          return 1
          ;;
      esac
      rm -rf "$WORKTREE" || {
        emit fail worktree-cleanup-failed "path=$WORKTREE"
        return 1
      }
    fi
    mkdir -p "$(dirname "$WORKTREE")"
    if ! git -C "$REPO" worktree add --detach --quiet "$WORKTREE" origin/main 2>/dev/null; then
      emit fail worktree-add-failed "path=$WORKTREE"
      return 1
    fi
    emit ok worktree-created "path=$WORKTREE"
    return 0
  fi

  # Existing healthy worktree: refresh it.
  if ! git -C "$WORKTREE" reset --hard --quiet origin/main; then
    emit fail worktree-reset-failed "path=$WORKTREE"
    return 1
  fi
  return 0
}

if ! cd "$REPO" 2>/dev/null; then
  emit fail chdir-failed "repo=$REPO"
  exit 1
fi

# Record the current branch for telemetry only — we no longer mutate it.
operator_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "(detached)")

if ! ensure_redeploy_worktree; then
  exit 1
fi

# Read state from the dedicated worktree. $WORKTREE is at origin/main
# (just reset/added). old_sha = whatever the kernel binary was built from
# (best-effort: read from the binary's accompanying .sha file, otherwise
# treat as "unknown" and force a rebuild).
new_sha=$(git -C "$WORKTREE" rev-parse HEAD)
old_sha_file="$BIN.sha"
if [[ -r "$old_sha_file" ]]; then
  old_sha=$(cat "$old_sha_file" 2>/dev/null || echo "")
else
  old_sha=""
fi

need_rebuild=0
relevant_changes_since_last="(none)"

if [[ -z "$old_sha" ]]; then
  need_rebuild=1
  relevant_changes_since_last="no-prior-sha-record"
elif [[ "$old_sha" != "$new_sha" ]]; then
  # Compare in the WORKTREE — old_sha may not even be reachable from
  # the operator's REPO (e.g., they're on a feature branch).
  if git -C "$WORKTREE" cat-file -e "$old_sha^{commit}" 2>/dev/null; then
    if git -C "$WORKTREE" diff --quiet "$old_sha" "$new_sha" -- go/ chitin.yaml; then
      emit noop "no kernel-relevant changes" "old_sha=$old_sha" "new_sha=$new_sha" "operator_branch=$operator_branch"
      exit 0
    fi
    relevant_changes_since_last=$(git -C "$WORKTREE" diff --name-only "$old_sha" "$new_sha" -- go/ chitin.yaml | tr '\n' ',' | sed 's/,$//')
  else
    relevant_changes_since_last="old-sha-unreachable"
  fi
  need_rebuild=1
fi

if [[ $need_rebuild -eq 0 ]]; then
  if [[ ! -x "$BIN" ]]; then
    need_rebuild=1
    relevant_changes_since_last="binary-missing"
  else
    # Check the worktree's tracked sources (not $REPO's — operator's REPO
    # may have local modifications irrelevant to the kernel build).
    if find "$WORKTREE/go" "$WORKTREE/chitin.yaml" -newer "$BIN" -print -quit 2>/dev/null | grep -q .; then
      need_rebuild=1
      relevant_changes_since_last="binary-stale-relative-to-source"
    fi
  fi
fi

if [[ $need_rebuild -eq 0 ]]; then
  emit noop "no rebuild needed" "old_sha=$old_sha" "new_sha=$new_sha"
  exit 0
fi

# Save prev binary for rollback (and its sha record if we have one).
if [[ -x "$BIN" ]]; then
  cp -a "$BIN" "$PREV"
fi

# Build inside the dedicated worktree. Chitin's go module is nested at
# go/execution-kernel/go.mod (no top-level go.mod), so `go build` runs
# from inside that module. The operator's $REPO is untouched throughout.
build_start_ns=$(date +%s%N)
if ! ( cd "$WORKTREE/go/execution-kernel" && go build -o "$BIN" ./cmd/chitin-kernel ) 2>&1; then
  emit fail build-failed "old_sha=$old_sha" "new_sha=$new_sha"
  if [[ -x "$PREV" ]]; then
    cp -a "$PREV" "$BIN"
    emit rollback build-fail-rollback-success "restored_from=$PREV"
  fi
  exit 2
fi
build_dur_ms=$(( ($(date +%s%N) - build_start_ns) / 1000000 ))

# Stamp the build's commit sha next to the binary so the NEXT run knows
# which commit the current binary was built from. Avoids re-using $REPO's
# HEAD as the proxy (which it never was reliably — $REPO can be on any
# branch — but now isn't read at all).
printf '%s\n' "$new_sha" > "$BIN.sha"

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

emit ok redeploy-success "old_sha=$old_sha" "new_sha=$new_sha" "build_dur_ms=$build_dur_ms" "changed=$relevant_changes_since_last" "operator_branch=$operator_branch"

# (apps/runner/src/ TS worker restart logic removed 2026-05-08 — the
# TS dispatcher/worker source was culled in the orchestration deletion
# wave. The Go kernel rebuild above is the only artifact this script
# ships now; hermes/openclaw drivers manage their own process lifecycle.)

exit 0
