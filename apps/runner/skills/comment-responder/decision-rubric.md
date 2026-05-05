# Per-comment decision rubric

For each comment: read the file at the `path`/`line` referenced,
re-read the surrounding context, and decide one of:

- **APPLY**: the comment identifies a real issue; edit the file to fix it.
- **DISMISS**: the comment is wrong (verified against source). Record
  the reason — be specific (cite the file/test that proves the
  comment's premise wrong).
- **ESCALATE**: the comment requires architectural judgment beyond your
  scope. Mark it for human review.
