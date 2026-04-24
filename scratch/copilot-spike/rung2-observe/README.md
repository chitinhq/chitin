# Rung 2: Observe JSON-RPC stream

## Protocol facts from SDK docs + source

- **Transport:** stdio pipe (default). The SDK spawns the `copilot` CLI as a
  subprocess and communicates over the child's stdin/stdout. TCP is available
  via `CLIUrl` or `Port` options, but stdio is the default and was used here.
- **Framing:** newline-delimited JSON objects (the `json.NewEncoder` in the SDK
  writes each message as one line; the internal `jsonrpc2` layer reads until
  `\n`). The `jsonrpc2` package is internal to the SDK.
- **Message schema:** JSON-RPC 2.0 underneath, but the SDK fully parses that
  layer before surfacing events. Consumer-visible types are in
  `generated_session_events.go` (auto-generated from `session-events.schema.json`).
- **Tool-call message type:** `SessionEventType = "tool.execution_start"` —
  Go type `ToolExecutionStartData{ToolName string, Arguments any, ToolCallID string}`.
  A second representation exists in `AssistantMessageData.ToolRequests` (pre-execution
  intent, type `"function"`) which fires just before `tool.execution_start`.
- **Protocol version:** from `sdk_protocol_version.go` — v0.2.2 of the Go SDK;
  the internal JSON-RPC negotiation version is not surfaced in session events.

## Probe strategy

The SDK surface exposes a `session.On(handler func(SessionEvent))` callback that
delivers every session event as a typed, fully-parsed Go struct. No transport
wrapping or env-var debug flags are required.

The probe:
1. Creates a `copilot.Client` (stdio, `CLIPath` from env or default)
2. Calls `session.On(handler)` — this is the stream tap
3. Creates a session with `OnPermissionRequest: ApproveAll`
4. Sends the prompt: "List the files in /tmp using the shell tool. Just run the command; do not explain."
5. Each event is serialized to `captured-stream.jsonl` via `json.Encoder`
6. Breaks on first `tool.execution_start` event OR 30-second timeout
7. Disconnects cleanly

The `session.On` approach (Approach 3 in the task spec) worked on the first attempt.
No transport wrapping was needed — the SDK delivers fully-parsed typed structs,
not raw JSON-RPC bytes.

## Expected tool call shape (from schema)

From `ToolExecutionStartData` in `generated_session_events.go`:

```json
{
  "direction": "inbound",
  "capturedAt": "<ISO-8601>",
  "eventType": "tool.execution_start",
  "eventId": "<uuid>",
  "parentId": "<uuid>",
  "data": {
    "toolCallId": "<string>",
    "toolName": "<string>",
    "arguments": { "<key>": "<value>" }
  }
}
```

The `arguments` field is typed `any` in Go and serializes to a structured JSON
object when the tool uses named parameters. For the `bash` tool:
`{"command": "<shell command>", "description": "<intent summary>"}`.
