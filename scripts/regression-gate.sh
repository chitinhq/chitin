#!/usr/bin/env bash
# regression-gate — run every registered invariant against the current tree.
# Exit 0 iff every check-*.{sh,py} passes; exit 1 on any failure.
# warn-*.{sh,py} run informationally and never affect the exit code.
#
# Spec: docs/superpowers/specs/2026-05-13-regression-gate.md
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mapfile -t gates < <(find scripts -maxdepth 1 -type f \
    \( -name 'check-*.sh' -o -name 'check-*.py' \) | sort)
mapfile -t warns < <(find scripts -maxdepth 1 -type f \
    \( -name 'warn-*.sh'  -o -name 'warn-*.py'  \) | sort)

PER_INVARIANT_TIMEOUT="${REGRESSION_GATE_TIMEOUT:-30}"

run_one() {
    local s="$1"
    echo "── $s ──"
    case "$s" in
        *.py) timeout "$PER_INVARIANT_TIMEOUT" python3 "$s" ;;
        *)    timeout "$PER_INVARIANT_TIMEOUT" bash    "$s" ;;
    esac
}

declare -A rc
fails=0
for s in "${gates[@]}"; do
    run_one "$s"; r=$?
    rc["$s"]=$r
    [ "$r" -eq 0 ] || fails=$((fails+1))
done
for s in "${warns[@]}"; do run_one "$s" || true; done

echo
echo "═══ regression-gate summary ═══"
for s in "${gates[@]}"; do
    [ "${rc[$s]}" -eq 0 ] && tag=PASS || tag=FAIL
    printf "  %-5s  %s\n" "$tag" "$s"
done

if [ "$fails" -gt 0 ]; then
    echo
    echo "$fails/${#gates[@]} invariant(s) broken."
    echo "False positive? Add an entry to scripts/<name>.allow with a # reason."
    echo "Spec: docs/superpowers/specs/2026-05-13-regression-gate.md"
    exit 1
fi
echo "All ${#gates[@]} invariants preserved."
exit 0
