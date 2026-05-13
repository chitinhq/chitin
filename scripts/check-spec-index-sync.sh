#!/usr/bin/env bash
# check-spec-index-sync — invariant: docs/superpowers/specs/INDEX.md is
# in sync with the regen-spec-index.py generator output.
#
# Part of the regression-gate registry — exit 0 = preserved, 1 = drift.
exec python3 "$(dirname "$0")/regen-spec-index.py" --check
