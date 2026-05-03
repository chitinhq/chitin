# Comment-responder summary template

Post the summary comment with this exact body shape (one bullet per
inline comment evaluated):

```
### comment-responder summary

- [APPLIED] <path>:<line> — <one-line summary of the fix>
- [DISMISSED] <path>:<line> — <reason; cite the source-of-truth>
- [ESCALATED] <path>:<line> — <reason; what kind of judgment is needed>

Tests: `<which tests ran and passed/failed>`
Commit: <commit_sha>
```

The per-thread replies (step 6) are the durable record. This summary
is the operator's overview — both must be posted.
