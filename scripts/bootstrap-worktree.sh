#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/bootstrap-worktree.sh [--store-dir PATH] [--offline] [--no-verify]

Hydrate a fresh git worktree with local node_modules while reusing one shared
pnpm content store. Do not symlink node_modules between worktrees.

Options:
  --store-dir PATH  Shared pnpm store path. Defaults to PNPM_STORE_DIR or ~/.pnpm-store.
  --offline         Install from the existing pnpm store only.
  --no-verify       Skip the final Nx availability check.
USAGE
}

offline=0
verify=1
store_dir="${PNPM_STORE_DIR:-$HOME/.pnpm-store}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --store-dir)
      shift
      if [[ $# -eq 0 ]]; then
        echo "error: --store-dir requires a path" >&2
        exit 2
      fi
      store_dir="$1"
      ;;
    --offline)
      offline=1
      ;;
    --no-verify)
      verify=0
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if ! command -v pnpm >/dev/null 2>&1; then
  echo "error: pnpm is not installed or not on PATH" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "error: not inside a git worktree" >&2
  exit 1
}

cd "$repo_root"

if [[ ! -f pnpm-lock.yaml || ! -f pnpm-workspace.yaml ]]; then
  echo "error: expected pnpm-lock.yaml and pnpm-workspace.yaml at $repo_root" >&2
  exit 1
fi

if [[ -L node_modules ]]; then
  echo "error: node_modules is a symlink; remove it before bootstrapping this worktree" >&2
  exit 1
fi

mkdir -p "$store_dir"

install_args=(install --frozen-lockfile --prefer-offline --store-dir "$store_dir")
if [[ "$offline" -eq 1 ]]; then
  install_args+=(--offline)
fi

echo "repo: $repo_root"
echo "pnpm store: $store_dir"
echo "node_modules: $repo_root/node_modules"
pnpm "${install_args[@]}"

if [[ "$verify" -eq 1 ]]; then
  pnpm exec nx --version >/dev/null
  echo "nx: $(pnpm exec nx --version)"
fi
