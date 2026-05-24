# Contract — `~/.chitin/ingestion-sources.yml`

Operator-managed YAML at `$HOME/.chitin/ingestion-sources.yml`. Chitin reads only.

## Top-level shape

```yaml
sources:
  - <SourceEntry>
  - <SourceEntry>
```

## SourceEntry fields

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | yes | Unique within file. Becomes the queue row's `source` column. |
| `type` | string | yes | One of `obsidian-vault`, `sentinel-findings`, or custom (treated as opaque). |
| `root` | string | yes | Absolute filesystem path. `~` expansion supported. |
| `patterns` | []string | yes | Glob(s) relative to root (Go `filepath.Match` syntax + `**` recursion). |
| `watch` | bool | yes | `true` = register fsnotify watch; `false` = poll-on-demand only (via future `chitin-orchestrator ingest run <name>` if added). |
| `extract` | object | no | Per-column extraction rules. Keys are queue column names; values are dot-paths like `frontmatter.topic` or `payload.confidence`. |
| `tag_default` | string | no | Default `tag` for rows from this source if `extract.tag` not set. |

## Extract syntax

`extract` keys map to columns in the `findings` table (per `contracts/queue-schema.md`):
- `topic`, `tag`, `agent_attribution`, `confidence_signal`, `novelty_signal`, `affects_core_infra`, `estimated_loc_range`, `triggered_by_chain_event`

Values are dot-paths. Two roots supported:
- `frontmatter.<key>` — for markdown files with YAML frontmatter
- `payload.<key>` — for JSON files where the file IS the payload

Resolution rules:
- Missing path → column is NULL
- Type mismatch (e.g., extract says number, file has string) → column is NULL + warning log
- Nested paths supported: `frontmatter.meta.confidence`

## Default sources (v1)

Recommend operator initialize with:

```yaml
sources:
  - name: obsidian-vault
    type: obsidian-vault
    root: /home/red/Documents/Obsidian Vault/Research
    patterns: ["**/sources/*.md", "**/index.md"]
    watch: true
    extract:
      topic: frontmatter.topic
      tag: frontmatter.tag
    tag_default: research

  - name: sentinel-mined
    type: sentinel-findings
    root: /home/red/.chitin/sentinel-findings
    patterns: ["swarm-mined-*.json"]
    watch: true
    extract:
      agent_attribution: payload.agent
      confidence_signal: payload.confidence
      triggered_by_chain_event: payload.originated_from_chain_event
```

## Validation

Chitin validates on load:
- `name` unique and non-empty
- `type` non-empty (no enum; opaque types accepted)
- `root` absolute (after `~` expansion) and exists
- `patterns` non-empty list of non-empty strings
- `watch` is bool
- `extract` keys map to known queue columns (warn-only)

## Reload semantics

Same idempotent pattern as schedules: editing the file and re-running `chitin-orchestrator ensure-swarm` reconciles the live fsnotify watchers (registers added, deregisters removed).

## Edge cases

- **Source path doesn't exist:** fail loud on load. Operator typo'd or vault hasn't been created.
- **Pattern matches a directory:** skipped with debug log.
- **Pattern matches symlink to outside root:** followed once; recursive symlinks detected and refused (fsnotify-level).
- **fsnotify watcher overflow:** chitin logs `swarm: fsnotify overflow on source=<name>` and emits a chain warning event. Operator should reduce vault size or split into multiple sources.
