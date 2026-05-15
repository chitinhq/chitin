## Clawta Boundary Coverage Finding

The boundary coverage finding is invalid against the current PR diff.
`python/argus/tests/test_beliefs.py` adds matching coverage for all named
boundaries:

- `test_clawta_empty_boundary_missing_default_paths_skips_without_alert`
- `test_openclaw_memory_max_boundary_truncates_claim_to_800_chars`
- `test_openclaw_memory_error_boundary_invalid_store_alerts`
