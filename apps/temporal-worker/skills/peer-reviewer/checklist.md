# Peer-reviewer five-axis checklist

For each meaningful chunk of the diff, walk the five axes below.
Cite specific lines; "the function looks complicated" is noise, "X
on line Y violates Z invariant" is signal.

**Correctness:** does the code do what its surrounding context (tests,
docstrings, callers) implies it should? Does it handle the obvious
edge cases (empty input, single input, N-equal input, max-int)? If
you can articulate the invariant in one sentence, walk it.

**Scope drift:** does the diff exceed what the PR description says it
does? A diff that adds an "incidental refactor" or a "while I'm here"
change beyond the stated scope is a flag — it's harder to review, and
the bonus changes often slip in unintended behavior.

**Security:** any user input crossing trust boundaries (network,
filesystem, subprocess, SQL, shell)? Look for shell-metacharacter
passthrough, path traversal, missing auth checks, missing rate
limits, predictable temp paths.

**Observability:** if this code can fail in production, will the
operator know? Logs at the right level (errors → stderr structured
JSON), chain-event emission for governance-relevant decisions.

**Test coverage:** the diff adds behavior — does it add tests for
that behavior? Edge cases? Negative paths (the function rejects bad
input)? If you find untested branches, name them.
