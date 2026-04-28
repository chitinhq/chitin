# Rung 2 Result

**Pass/Fail:** pass

**Date/Time:** 2026-04-24T00:58:41Z

## Evidence

- Stream captured: `captured-stream.jsonl` (12 lines)
- Tap mechanism that worked: `session.On(func(event copilot.SessionEvent))` —
  the SDK delivers every session event as a typed, fully-parsed Go struct;
  no transport wrapping or debug env vars needed.

### Example tool-call message (redacted)

```json
{
  "direction": "inbound",
  "capturedAt": "2026-04-24T00:58:41.254274266Z",
  "eventType": "tool.execution_start",
  "eventId": "ed4caa2b-6511-4449-b182-dc5ceb24dbd9",
  "parentId": "fba3f6cf-6717-443a-9209-80215f14bd64",
  "data": {
    "toolCallId": "call_2ImD5lp3GnbQV74zPcpgHzqy",
    "toolName": "bash",
    "arguments": {
      "command": "ls -lh /tmp",
      "description": "List files in /tmp directory"
    }
  }
}
```

## Protocol observations

- **Transport confirmed:** stdio pipe (SDK spawns `copilot` CLI subprocess, communicates on stdin/stdout)
- **Framing confirmed:** newline-delimited JSON (one JSON object per line, written by `json.NewEncoder`)
- **Message types seen in the capture:**
  - `pending_messages.modified` (×2)
  - `session.custom_agents_updated`
  - `session.skills_loaded`
  - `system.message`
  - `session.tools_updated`
  - `user.message`
  - `assistant.turn_start`
  - `session.usage_info`
  - `assistant.usage`
  - `assistant.message` (contains `toolRequests` array — pre-execution intent)
  - `tool.execution_start` ← structured tool call with name + args
- **Protocol version:** not surfaced in session events; SDK version is `v0.2.2`

## Normalization feasibility

**yes**

The `tool.execution_start` event provides `toolName` (string) and `arguments`
(structured JSON object). The mapping to chitin's canonical `Action` is
straightforward:

**For the `bash` tool:**
- Copilot tool message: `{"toolName": "bash", "arguments": {"command": "ls -lh /tmp", "description": "..."}}`
- Chitin Action: `Action{Type: "shell.exec", Target: "ls -lh /tmp", Path: "<session cwd>"}`

**General mapping rules:**
| Copilot `toolName`    | Chitin `ActionType`       | `Target` source                     |
|-----------------------|---------------------------|-------------------------------------|
| `bash`                | `shell.exec`              | `arguments["command"]`              |
| `read_file`           | `file.read`               | `arguments["path"]`                 |
| `write_file`          | `file.write`              | `arguments["path"]`                 |
| `delete_file`         | `file.delete`             | `arguments["path"]`                 |
| `create_file`         | `file.write`              | `arguments["path"]`                 |
| `move_file`           | `file.move`               | `arguments["source"]`               |
| any MCP tool          | `mcp.call`                | `arguments["tool_name"]`            |
| (unrecognized)        | `unknown`                 | `toolName`                          |

The `ToolExecutionStartData.Arguments` field is typed `any` and arrives as a
Go `map[string]interface{}` after JSON decode — directly usable as `Action.Params`.

**Important pre-execution intercept point:**
`OnPermissionRequest` fires *before* `tool.execution_start`, and `AssistantMessageData.ToolRequests`
(event type `assistant.message`) fires even earlier (pre-execution intent). Both contain
`toolName` + structured `arguments`. For Rung 3 governance, `OnPermissionRequest` is the
synchronous intercept boundary — the tool is blocked until the handler returns.
The `PermissionRequest.Kind` enum (`shell`, `write`, `read`, `mcp`, `url`, `memory`,
`custom-tool`, `hook`) maps cleanly to chitin `ActionType` without re-parsing the command.

## Time taken

Start: 2026-04-24T00:58:36Z
End:   2026-04-24T00:58:41Z
Wall:  ~5 minutes (including SDK source research + README + this file)

## Surprises

1. **The SDK does not expose raw JSON-RPC bytes.** The `jsonrpc2` package is in
   `go/internal/` — deliberately unexported. The `session.On` callback is the
   only event tap. This is actually better for chitin: the SDK handles framing,
   deserialization, and reconnect; governance code works with typed structs.

2. **`system.message` delivers the full system prompt** (with embedded newlines
   in string values — valid per RFC 8259 but trips Python's `strict=True` JSON
   parser). The system prompt embeds copilot's `<environment_context>` including
   cwd, git repo, and available tools. This is chitin-visible context.

3. **`AssistantMessageData.ToolRequests` fires before `tool.execution_start`.**
   Two observation points exist: pre-execution intent (in `assistant.message`)
   and execution-start (in `tool.execution_start`). `OnPermissionRequest` fires
   between these two and is the governance intercept point for Rung 3.

4. **Tool args are structured JSON objects, not prose.** `bash` args:
   `{"command": "...", "description": "..."}`. The `command` key is the literal
   shell string — trivially normalizable to `Action{Type: "shell.exec", Target: command}`.
