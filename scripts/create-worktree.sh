#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/create-worktree.sh --agent NAME --task SLUG [options]
       scripts/create-worktree.sh --branch BRANCH [options]

Create a linked git worktree for agent work and optionally hydrate pnpm
dependencies with scripts/bootstrap-worktree.sh.

Examples:
  scripts/create-worktree.sh --agent codex --task worktree-helper
  scripts/create-worktree.sh --branch openclaw/coverage-router --offline
  pnpm worktree -- --agent hermes --task issue-123

Options:
  --agent NAME      Agent/owner prefix for the branch. Used with --task.
  --task SLUG      Task slug. Used with --agent to form NAME/SLUG.
  --branch BRANCH  Exact branch name to create or reuse.
  --base REF       Base ref for new worktrees. Defaults to origin/main.
  --path PATH      Worktree path. Defaults to ../<repo>-<branch-slug>.
  --no-bootstrap   Skip pnpm dependency hydration.
  --offline        Pass --offline to scripts/bootstrap-worktree.sh.
  --store-dir DIR  Pass a shared pnpm store dir to bootstrap-worktree.sh.
  --no-verify      Pass --no-verify to bootstrap-worktree.sh.
USAGE
}

agent=""
task=""
branch=""
base_ref="${CHITIN_WORKTREE_BASE_REF:-origin/main}"
target_path=""
bootstrap=1
offline=0
verify=1
store_dir="${PNPM_STORE_DIR:-$HOME/.pnpm-store}"
created=0

slugify() {
  printf '%s' "$1" |
    tr '[:upper:]' '[:lower:]' |
    sed -E 's#[^a-z0-9._/-]+#-#g; s#^-+##; s#-+$##; s#/{2,}#/#g'
}

require_nonempty() {
  local label="$1"
  local value="$2"
  if [[ -z "$value" ]]; then
    echo "error: $label slug is empty after normalization" >&2
    exit 2
  fi
}

is_registered_worktree() {
  local candidate
  candidate="$(realpath -m "$1")"
  local registered
  while IFS= read -r registered; do
    [[ "$(realpath -m "$registered")" == "$candidate" ]] && return 0
  done < <(git -C "$repo_root" worktree list --porcelain | sed -n 's/^worktree //p')
  return 1
}

has_gitdir_file() {
  local git_file="$1/.git"
  [[ -f "$git_file" ]] || return 1
  head -n 1 "$git_file" 2>/dev/null | grep -Eq '^gitdir:'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --)
      ;;
    --agent)
      shift
      [[ $# -gt 0 ]] || { echo "error: --agent requires a value" >&2; exit 2; }
      agent="$(slugify "$1")"
      require_nonempty "--agent" "$agent"
      ;;
    --task)
      shift
      [[ $# -gt 0 ]] || { echo "error: --task requires a value" >&2; exit 2; }
      task="$(slugify "$1")"
      require_nonempty "--task" "$task"
      ;;
    --branch)
      shift
      [[ $# -gt 0 ]] || { echo "error: --branch requires a value" >&2; exit 2; }
      branch="$(slugify "$1")"
      require_nonempty "--branch" "$branch"
      ;;
    --base)
      shift
      [[ $# -gt 0 ]] || { echo "error: --base requires a value" >&2; exit 2; }
      base_ref="$1"
      ;;
    --path)
      shift
      [[ $# -gt 0 ]] || { echo "error: --path requires a value" >&2; exit 2; }
      target_path="$1"
      ;;
    --no-bootstrap)
      bootstrap=0
      ;;
    --offline)
      offline=1
      ;;
    --store-dir)
      shift
      [[ $# -gt 0 ]] || { echo "error: --store-dir requires a path" >&2; exit 2; }
      store_dir="$1"
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

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "error: not inside the chitin git repository" >&2
  exit 1
}
repo_name="$(basename "$repo_root")"

if [[ -z "$branch" ]]; then
  if [[ -z "$agent" || -z "$task" ]]; then
    echo "error: provide --branch, or both --agent and --task" >&2
    usage >&2
    exit 2
  fi
  branch="$agent/$task"
fi

if ! git check-ref-format --branch "$branch" >/dev/null 2>&1; then
  echo "error: invalid branch name after normalization: $branch" >&2
  exit 2
fi

if [[ -z "$target_path" ]]; then
  path_slug="$(printf '%s' "$branch" | sed -E 's#[^a-zA-Z0-9._-]+#-#g; s#^-+##; s#-+$##')"
  target_path="$(dirname "$repo_root")/$repo_name-$path_slug"
fi

if [[ -e "$target_path" && -d "$target_path/.git" ]]; then
  echo "error: target path is a standalone checkout, not a linked worktree: $target_path" >&2
  exit 1
fi

if [[ -e "$target_path" ]] && ! has_gitdir_file "$target_path" && ! is_registered_worktree "$target_path"; then
  echo "error: target path exists but is not a registered linked worktree for $repo_root: $target_path" >&2
  exit 1
fi

if [[ -e "$target_path" ]]; then
  existing_branch="$(git -C "$target_path" branch --show-current 2>/dev/null || true)"
  if [[ "$existing_branch" != "$branch" ]]; then
    echo "error: existing worktree is on branch '$existing_branch', expected '$branch': $target_path" >&2
    exit 1
  fi
  echo "worktree: reusing $target_path"
else
  if git -C "$repo_root" show-ref --verify --quiet "refs/heads/$branch"; then
    git -C "$repo_root" worktree add "$target_path" "$branch"
  else
    git -C "$repo_root" worktree add "$target_path" -b "$branch" "$base_ref"
  fi
  created=1
fi

if [[ "$bootstrap" -eq 1 ]]; then
  bootstrap_args=(--store-dir "$store_dir")
  [[ "$offline" -eq 1 ]] && bootstrap_args+=(--offline)
  [[ "$verify" -eq 0 ]] && bootstrap_args+=(--no-verify)
  (cd "$target_path" && bash scripts/bootstrap-worktree.sh "${bootstrap_args[@]}")
fi

cat <<EOF
ready: $target_path
branch: $branch
base: $([[ "$created" -eq 1 ]] && printf '%s' "$base_ref" || printf '(reused)')

Next:
  cd "$target_path"
EOF
