# Safe Temporary Work

Chitin intentionally blocks recursive filesystem deletion, including
temporary-directory cleanup such as `rm -rf "$tmp"`. The gate cannot prove a
variable still points at the directory the worker created, so recursive delete
stays denied even under `/tmp`.

Use one of these patterns instead:

- Prefer language or test-framework temp helpers such as Go's `t.TempDir()`.
  The harness owns cleanup outside the agent's shell command.
- If a shell script creates known files, delete those files by exact path and
  then remove the empty directory with `rmdir "$tmp"`.
- If the temp tree is large or dynamic, leave it under `/tmp` with a unique
  `mktemp -d` name and let the host's normal temp cleanup handle it.

Example worker-safe shell cleanup:

```sh
tmp=$(mktemp -d)
printf '%s\n' example > "$tmp/output.txt"

# ...use "$tmp/output.txt"...

rm "$tmp/output.txt"
rmdir "$tmp"
```

Do not use recursive cleanup as a convenience step in shared commands. If a
task genuinely requires deleting a populated tree, hand it to the operator or
remove only the specific files that the task created.
