# Board Config Sidecar

Swarm board metadata now lives in a per-board sidecar at:

`~/.hermes/kanban/boards/<slug>/config.json`

For the `chitin` board, the seeded file (with absolute paths so
downstream consumers don't need to expand `~`) is:

```json
{
  "repo": "chitinhq/chitin",
  "default_branch": "main",
  "workspace_root": "/home/operator/workspace/chitin",
  "kernel_bin": "chitin-kernel",
  "chitin_yaml": "chitin.yaml"
}
```

## Schema

- `repo` — required GitHub `owner/name` repository slug.
- `default_branch` — required default git branch for the board repo.
- `workspace_root` — required operator workspace root for that repo.
  A leading `~/` is expanded by the reader; all other consumers should
  expect an absolute path.
- `kernel_bin` — required kernel binary name or path.
- `chitin_yaml` — optional policy file path relative to `workspace_root`; defaults to `chitin.yaml`.

All fields are strings. There is no fallback to chitin hardcodes when a required field is missing.

## Reader Contract

Read board config through:

```bash
chitin-kernel board-config <slug> <field>
```

Behavior:

- Precedence is `env > config.json > error`.
- The helper validates the full required-field set before returning any value.
- Environment overrides use:
  - `KANBAN_BOARD_REPO`
  - `KANBAN_BOARD_DEFAULT_BRANCH`
  - `KANBAN_BOARD_WORKSPACE_ROOT`
  - `KANBAN_BOARD_KERNEL_BIN`
  - `KANBAN_BOARD_CHITIN_YAML`
- `chitin_yaml` defaults to `chitin.yaml` when absent from both env and `config.json`.

### Exit codes and error shapes

Errors are emitted to stderr as a single JSON object
(`{"error": "<kind>", "message": "<detail>"}`) and map to these
exit codes:

| Exit | `error` kind            | When                                                                 |
| ---- | ----------------------- | -------------------------------------------------------------------- |
| `2`  | `board_config_args`     | Argument count is wrong, or flag parsing fails.                      |
| `2`  | `invalid_slug`          | Slug is empty, `.`, `..`, or contains a path separator (path traversal guard). |
| `2`  | `unknown_field`         | Field name is not one of the schema keys.                            |
| `2`  | `missing_field`         | A required field is empty in both env and `config.json`.             |
| `2`  | `missing_config`        | The board directory exists but has no `config.json`.                 |
| `3`  | `no_boards_initialized` | The boards root has no board directories yet.                        |
| `3`  | `unknown_board`         | A different board exists but the requested slug does not.            |

The helper does not synthesize defaults for a missing `config.json`;
the seeder in `scripts/install-swarm.sh` is the canonical bootstrap.

Examples:

```bash
chitin-kernel board-config chitin repo
chitin-kernel board-config chitin kernel_bin
KANBAN_BOARD_REPO=override/example chitin-kernel board-config chitin repo
```
