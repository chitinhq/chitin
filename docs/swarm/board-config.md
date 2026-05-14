# Board Config Sidecar

Swarm board metadata now lives in a per-board sidecar at:

`~/.hermes/kanban/boards/<slug>/config.json`

For the `chitin` board, the seeded file is:

```json
{
  "repo": "chitinhq/chitin",
  "default_branch": "main",
  "workspace_root": "~/workspace/chitin",
  "kernel_bin": "chitin-kernel",
  "chitin_yaml": "chitin.yaml"
}
```

## Schema

- `repo` — required GitHub `owner/name` repository slug.
- `default_branch` — required default git branch for the board repo.
- `workspace_root` — required operator workspace root for that repo.
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
- Environment overrides use:
  - `KANBAN_BOARD_REPO`
  - `KANBAN_BOARD_DEFAULT_BRANCH`
  - `KANBAN_BOARD_WORKSPACE_ROOT`
  - `KANBAN_BOARD_KERNEL_BIN`
  - `KANBAN_BOARD_CHITIN_YAML`
- `chitin_yaml` defaults to `chitin.yaml` when absent from both env and `config.json`.
- Unknown board exits `3` with `unknown board: <slug>`.
- No boards initialized exits `3` with `no boards initialized`.
- Missing required field exits `2` with `missing field: <name>`.
- A board directory without `config.json` is an error; the helper does not synthesize defaults for that board.

Examples:

```bash
chitin-kernel board-config chitin repo
chitin-kernel board-config chitin kernel_bin
KANBAN_BOARD_REPO=override/example chitin-kernel board-config chitin repo
```
