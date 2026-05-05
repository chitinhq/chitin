#!/usr/bin/env bash
# Symlink all chitin-* systemd units from infra/systemd/ into the user unit
# directory, daemon-reload, and auto-enable any *newly-linked* timers.
#
# Usage:
#   bash scripts/install-systemd-units.sh [--enable-all] [--dry-run]
#
# Options:
#   --enable-all  Force-enable every timer (override new-only logic).
#                 Use after a fresh checkout where prior enable state is gone.
#   --dry-run     Print what would be done without changing anything.
#
# Default behavior: link all units, auto-enable timers that didn't exist
# before this run, leave already-symlinked timers alone (preserves the
# operator's intentional `systemctl --user disable <unit>` actions).
#
# Closes the recurring "shipped a unit, forgot to enable" gap. 2026-05-04
# incident: PR #282 shipped chitin-agent-unlock.{service,timer}, the
# operator never copied + enabled them, so when copilot-cli locked down
# at 03:27 UTC the auto-recovery never ran (manual unlock at ~21:30 UTC,
# after a 20.5h outage). Auto-enabling new timers makes the install step
# match the merge step.
#
# Designed to be invoked:
#   - Manually post-pull / post-merge
#   - From install-kernel.sh after a successful rebuild (every 15 min)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC_DIR="$REPO_ROOT/infra/systemd"
UNIT_DST_DIR="${SYSTEMD_USER_DIR:-$HOME/.config/systemd/user}"

ENABLE_ALL=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --enable-all) ENABLE_ALL=true; shift ;;
    --enable)     ENABLE_ALL=true; shift ;;  # legacy alias
    --dry-run)    DRY_RUN=true;    shift ;;
    *) echo "error: unknown option: $1" >&2; exit 1 ;;
  esac
done

if [[ ! -d "$UNIT_SRC_DIR" ]]; then
  echo "error: $UNIT_SRC_DIR does not exist" >&2
  exit 1
fi

# Brace-expand THEN walk; bash's `[[ -e ... ]]` discards literal
# unmatched glob patterns. Without nullglob the array would still
# contain the literal pattern strings on no-match, so the
# length-zero check would never fire. Use shopt + a real existence
# check instead.
shopt -s nullglob
units=("$UNIT_SRC_DIR"/chitin-*.service "$UNIT_SRC_DIR"/chitin-*.timer)
shopt -u nullglob
if [[ ${#units[@]} -eq 0 ]]; then
  echo "no chitin-* units found in $UNIT_SRC_DIR" >&2
  exit 1
fi

if [[ "$DRY_RUN" == "false" ]]; then
  mkdir -p "$UNIT_DST_DIR"
fi

linked=()
newly_linked=()  # subset that didn't exist before this run
for src in "${units[@]}"; do
  [[ -f "$src" ]] || continue
  name="$(basename "$src")"
  dst="$UNIT_DST_DIR/$name"
  if [[ "$DRY_RUN" == "true" ]]; then
    if [[ -e "$dst" ]]; then
      echo "would re-link $dst -> $src"
    else
      echo "would symlink (new) $dst -> $src"
    fi
  else
    if [[ -e "$dst" && ! -L "$dst" ]]; then
      echo "warning: $dst exists and is not a symlink — skipping" >&2
      continue
    fi
    pre_existed=false
    [[ -L "$dst" ]] && pre_existed=true
    ln -sf "$src" "$dst"
    echo "symlinked $dst -> $src"
    linked+=("$name")
    [[ "$pre_existed" == "false" ]] && newly_linked+=("$name")
  fi
done

if [[ "$DRY_RUN" == "true" ]]; then
  exit 0
fi

systemctl --user daemon-reload
echo "daemon-reload done"

# Decide which timers to enable. Default: only NEW links (preserves
# operator's manual `disable` actions on existing timers). --enable-all
# overrides for explicit operator intent (e.g. fresh checkout).
to_enable=()
if [[ "$ENABLE_ALL" == "true" ]]; then
  to_enable=("${linked[@]}")
else
  to_enable=("${newly_linked[@]}")
fi

for name in "${to_enable[@]}"; do
  if [[ "$name" == *.timer ]]; then
    if systemctl --user enable --now "$name" 2>/dev/null; then
      echo "enabled $name"
    else
      echo "warning: failed to enable $name (manual enable may be needed)" >&2
    fi
  fi
done

echo ""
echo "Done. Verify with: systemctl --user list-timers"
