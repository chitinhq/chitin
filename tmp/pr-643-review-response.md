# PR 643 review response

## Boundary coverage gate

The boundary coverage finding is invalid for the original docs-only
publication. `SPEC/chitin-protocol.md` does not contain an
`invariants_and_boundaries:` section naming `empty`, `max`, or `error`.

Those boundary names appear in other implemented specs and test files,
but the protocol publication did not add or change the implementation
slice that owns those boundaries. The follow-up schema alignment in this
commit does add explicit `empty`, `max`, and `error` envelope tests.
