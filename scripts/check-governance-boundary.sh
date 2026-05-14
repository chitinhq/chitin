#!/usr/bin/env bash
set -euo pipefail

SPEC="docs/superpowers/specs/2026-05-13-go-only-governance-authority.md"
ALLOWLIST="scripts/governance-boundary.allow"
SELF="scripts/check-governance-boundary.sh"

cd "$(git rev-parse --show-toplevel)"

if ! command -v rg >/dev/null 2>&1; then
  echo "governance-boundary: ripgrep (rg) is required" >&2
  exit 2
fi

if [[ ! -f "$ALLOWLIST" ]]; then
  echo "governance-boundary: missing $ALLOWLIST" >&2
  echo "see $SPEC" >&2
  exit 1
fi

declare -A allowed=()
allow_errors=0
line_no=0
while IFS= read -r raw || [[ -n "$raw" ]]; do
  line_no=$((line_no + 1))
  line="${raw#"${raw%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  [[ -z "$line" || "$line" == \#* ]] && continue

  if [[ "$line" != *"#"* ]]; then
    echo "governance-boundary: $ALLOWLIST:$line_no missing '# reason'" >&2
    allow_errors=1
    continue
  fi

  path="${line%%#*}"
  reason="${line#*#}"
  path="${path%"${path##*[![:space:]]}"}"
  reason="${reason#"${reason%%[![:space:]]*}"}"
  reason="${reason%"${reason##*[![:space:]]}"}"

  if [[ -z "$path" || -z "$reason" ]]; then
    echo "governance-boundary: $ALLOWLIST:$line_no must be '<path> # reason'" >&2
    allow_errors=1
    continue
  fi

  if [[ "$path" = /* || "$path" == *".."* ]]; then
    echo "governance-boundary: $ALLOWLIST:$line_no path must be repo-relative: $path" >&2
    allow_errors=1
    continue
  fi

  if [[ ! -f "$path" ]]; then
    echo "governance-boundary: $ALLOWLIST:$line_no allowlisted path does not exist: $path" >&2
    allow_errors=1
    continue
  fi

  allowed["$path"]=1
done < "$ALLOWLIST"

if [[ "$allow_errors" -ne 0 ]]; then
  echo "governance-boundary: fix $ALLOWLIST; every entry needs a one-line reason" >&2
  exit 1
fi

roots=()
for root in apps libs python scripts swarm bin tools examples infra web; do
  [[ -e "$root" ]] && roots+=("$root")
done

mapfile -t hits < <(
  rg --no-config -H -n --color never \
    -g "!go/**" \
    -g "!docs/**" \
    -g "!scratch/**" \
    -g "!graphify-out/**" \
    -g "!**/*test*" \
    -g "!**/*.md" \
    -g "!$SELF" \
    -g "!$ALLOWLIST" \
    -e '\b(Allow|Deny|Decision|Verdict)\b\s*[:=({\[]' \
    -e '\b(is_allowed|should_(allow|deny)|evaluate_(rule|gate|policy))\b\s*[:=({\[]' \
    -e '\b(isAllowed|should(Allow|Deny)|[Ee]valuate(Rule|Gate|Policy))\b\s*[:=({\[]' \
    -e '\bnew\s+(Gate|Policy)Decision\b' \
    "${roots[@]}" || true
)

violations=()
for hit in "${hits[@]}"; do
  path="${hit%%:*}"
  path="${path#./}"
  if [[ -n "${allowed[$path]:-}" ]]; then
    continue
  fi
  violations+=("$hit")
done

if [[ "${#violations[@]}" -gt 0 ]]; then
  echo "governance-boundary: violation"
  printf '  %s\n' "${violations[@]}"
  cat <<EOF
fix: move policy/gate decision logic into go/execution-kernel/internal/gov/
     or add the path to $ALLOWLIST with a one-line reason.
see: $SPEC
EOF
  exit 1
fi

echo "governance-boundary: ok"
