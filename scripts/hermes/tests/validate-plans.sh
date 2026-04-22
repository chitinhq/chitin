#!/usr/bin/env bash
# Validates every fixture file against plan-schema.json.
# Files named plan-valid-*.json must validate; plan-invalid-*.json must fail.
# Exits 0 iff all fixtures behave as expected.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA="$SCRIPT_DIR/../plan-schema.json"
FIXTURES_DIR="$SCRIPT_DIR/fixtures"

if ! command -v ajv >/dev/null; then
  echo "SKIP: ajv not installed (install via: npm i -g ajv-cli)"
  exit 0
fi

failures=0
for f in "$FIXTURES_DIR"/plan-*.json; do
  name="$(basename "$f")"
  if ajv validate -s "$SCHEMA" -d "$f" --spec=draft2020 >/dev/null 2>&1; then
    outcome=valid
  else
    outcome=invalid
  fi
  case "$name" in
    plan-valid-*)
      if [[ "$outcome" != valid ]]; then
        echo "FAIL: $name should be valid but schema rejected it"
        failures=$((failures+1))
      else
        echo "OK:   $name -> valid (expected)"
      fi
      ;;
    plan-invalid-*)
      if [[ "$outcome" != invalid ]]; then
        echo "FAIL: $name should be invalid but schema accepted it"
        failures=$((failures+1))
      else
        echo "OK:   $name -> invalid (expected)"
      fi
      ;;
  esac
done

if [[ $failures -gt 0 ]]; then
  echo ""
  echo "$failures fixture(s) failed"
  exit 1
fi
