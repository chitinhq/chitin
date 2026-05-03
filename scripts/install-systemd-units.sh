#!/usr/bin/env bash
# Symlink all chitin-* systemd units from infra/systemd/ into the user unit
# directory, then reload and enable timers.
#
# Usage:
#   bash scripts/install-systemd-units.sh [--enable] [--dry-run]
#
# Options:
#   --enable   Enable (and start) all timer units after symlinking (default: off)
#   --dry-run  Print what would be done without changing anything

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC_DIR="$REPO_ROOT/infra/systemd"
UNIT_DST_DIR="${SYSTEMD_USER_DIR:-$HOME/.config/systemd/user}"

ENABLE=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --enable)  ENABLE=true;  shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    *) echo "error: unknown option: $1" >&2; exit 1 ;;
  esac
done

if [[ ! -d "$UNIT_SRC_DIR" ]]; then
  echo "error: $UNIT_SRC_DIR does not exist" >&2
  exit 1
fi

units=("$UNIT_SRC_DIR"/chitin-*.{service,timer})
if [[ ${#units[@]} -eq 0 ]]; then
  echo "no chitin-* units found in $UNIT_SRC_DIR" >&2
  exit 1
fi

if [[ "$DRY_RUN" == "false" ]]; then
  mkdir -p "$UNIT_DST_DIR"
fi

linked=()
for src in "${units[@]}"; do
  [[ -f "$src" ]] || continue
  name="$(basename "$src")"
  dst="$UNIT_DST_DIR/$name"
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "would symlink $dst -> $src"
  else
    if [[ -e "$dst" && ! -L "$dst" ]]; then
      echo "warning: $dst exists and is not a symlink — skipping" >&2
      continue
    fi
    ln -sf "$src" "$dst"
    echo "symlinked $dst -> $src"
    linked+=("$name")
  fi
done

if [[ "$DRY_RUN" == "true" ]]; then
  exit 0
fi

systemctl --user daemon-reload
echo "daemon-reload done"

if [[ "$ENABLE" == "true" ]]; then
  for name in "${linked[@]}"; do
    if [[ "$name" == *.timer ]]; then
      systemctl --user enable --now "$name"
      echo "enabled $name"
    fi
  done
fi

echo ""
echo "Done. Verify with: systemctl --user list-timers"
