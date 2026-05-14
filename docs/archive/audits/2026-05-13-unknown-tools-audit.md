# Unknown Tool Normalization Audit - 2026-05-13

Scope: local chain rows in `~/.chitin/gov-decisions-2026-05-{07..13}.jsonl`, excluding `router.signal` rows.

Summary:

- Total decisions audited: 12,590
- `action_type=unknown`: 469 (3.73%)
- Allows: 0
- Denies/lockdowns: 469

## Current Unknown Tool Names

| Driver/agent | Tool | Count | Mapping |
|---|---:|---:|---|
| hermes | `kanban_show` | 286 | `kanban.call`, target `show` |
| hermes | `kanban_block` | 116 | `kanban.call`, target `block` |
| hermes | `kanban_comment` | 18 | `kanban.call`, target `comment` |
| hermes | `clarify` | 8 | `file.read`, target prompt/message |
| hermes | `kanban_complete` | 7 | `kanban.call`, target `complete` |
| hermes | `process` | 5 | `hermes.process`, target action |
| hermes | `skills_list` | 4 | `file.read`, target category |
| hermes | `memory` | 4 | `file.write`, target `memory` |
| hermes | `skill_manage` | 2 | `file.write`, target skill name |
| hermes | `kanban_heartbeat` | 2 | `kanban.call`, target `heartbeat` |
| glm-agent | `exec` | 2 | `tool.custom`, target `claude-code/exec` through default Claude-compatible adapter |
| claude-code | `memory_search` | 2 | `file.read`, target query |
| claude-code | `exec` | 2 | `tool.custom`, target `claude-code/exec` |
| hermes | `todo` | 1 | `file.write`, target `todo` |
| hermes | `session_search` | 1 | `file.read`, target query |
| hermes | `kanban_link` | 1 | `kanban.call`, target `link` |
| hermes | `browser_navigate` | 1 | `http.request`, target URL |
| glm-agent | `read` | 1 | `file.read`, target path/file_path through default Claude-compatible adapter |
| glm-agent | `glob` | 1 | `file.read`, target pattern through default Claude-compatible adapter |
| glm-agent | `Bash` | 1 | `shell.exec` or more-specific shell action via `gov.Normalize` |
| clawta | `Notification` | 1 | `http.request`, target URL/endpoint/tool name |
| claude-code | `session_status` | 1 | `file.read`, target `session_status` |
| claude-code | `read` | 1 | `file.read`, target path/file_path |
| claude-code | `memory_get` | 1 | `file.read`, target path/file |

## Adapter Status

All tool names observed in this audit now have specific normalization coverage in the relevant adapter path or in the default Claude-compatible fallback used by non-specialized agent labels.

Truly novel Hermes modality tools remain intentionally fail-closed and documented in `internal/driver/hermes/normalize.go`: `image_generate`, `text_to_speech`, `vision_analyze`, and `cronjob`. They were not present in the audited unknown rows and should not be catch-all mapped without a separate action-vocabulary decision.
