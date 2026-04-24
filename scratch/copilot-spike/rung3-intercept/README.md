# Rung 3: Intercept + synchronous block

## Intercept mechanism found

- **API surface:** `copilot.PermissionHandlerFunc` — a function type set as the
  `OnPermissionRequest` field of `copilot.SessionConfig` when calling
  `client.CreateSession`.
- **Registration:** `SessionConfig.OnPermissionRequest` field (required; SDK
  returns an error if nil). Wired internally via
  `session.registerPermissionHandler(config.OnPermissionRequest)` at session
  creation time.
- **Callback signature:**
  ```go
  type PermissionHandlerFunc func(request PermissionRequest, invocation PermissionInvocation) (PermissionRequestResult, error)
  ```
- **Refusal semantics:** Return `(PermissionRequestResult{}, error)` — any
  non-nil error causes the SDK to send
  `PermissionDecisionKindDeniedNoApprovalRuleAndCouldNotRequestFromUser` back to
  the CLI subprocess. Alternatively, return
  `PermissionRequestResult{Kind: PermissionRequestResultKindDeniedByRules}` for
  a named denial without an error.
- **Synchronicity:** The handler is called inside `executePermissionAndRespond`,
  which runs in a dedicated goroutine spawned by `dispatchEvent`. The key is:
  `HandlePendingPermissionRequest` (the RPC call that tells the CLI to proceed
  or abort) is only sent **after** the handler returns. Source: `session.go`
  lines 1042–1054. The CLI subprocess holds the tool execution pending that RPC
  reply, so the tool cannot execute until our callback has returned and the deny
  decision has been sent.

## PermissionRequest fields

```go
type PermissionRequest struct {
	// Kind discriminator
	Kind PermissionRequestKind `json:"kind"`
	// Whether this is a store or vote memory operation
	Action *PermissionRequestMemoryAction `json:"action,omitempty"`
	// Arguments to pass to the MCP tool
	Args any `json:"args,omitempty"`
	// Whether the UI can offer session-wide approval for this command pattern
	CanOfferSessionApproval *bool `json:"canOfferSessionApproval,omitempty"`
	// Source references for the stored fact (store only)
	Citations *string `json:"citations,omitempty"`
	// Parsed command identifiers found in the command text
	Commands []PermissionRequestShellCommand `json:"commands,omitempty"`
	// Unified diff showing the proposed changes
	Diff *string `json:"diff,omitempty"`
	// Vote direction (vote only)
	Direction *PermissionRequestMemoryDirection `json:"direction,omitempty"`
	// The fact being stored or voted on
	Fact *string `json:"fact,omitempty"`
	// Path of the file being written to
	FileName *string `json:"fileName,omitempty"`
	// The complete shell command text to be executed
	FullCommandText *string `json:"fullCommandText,omitempty"`
	// Whether the command includes a file write redirection (e.g., > or >>)
	HasWriteFileRedirection *bool `json:"hasWriteFileRedirection,omitempty"`
	// Optional message from the hook explaining why confirmation is needed
	HookMessage *string `json:"hookMessage,omitempty"`
	// Human-readable description of what the command intends to do
	Intention *string `json:"intention,omitempty"`
	// Complete new file contents for newly created files
	NewFileContents *string `json:"newFileContents,omitempty"`
	// Path of the file or directory being read
	Path *string `json:"path,omitempty"`
	// File paths that may be read or written by the command
	PossiblePaths []string `json:"possiblePaths,omitempty"`
	// URLs that may be accessed by the command
	PossibleUrls []PermissionRequestShellPossibleURL `json:"possibleUrls,omitempty"`
	// Whether this MCP tool is read-only (no side effects)
	ReadOnly *bool `json:"readOnly,omitempty"`
	// Reason for the vote (vote only)
	Reason *string `json:"reason,omitempty"`
	// Name of the MCP server providing the tool
	ServerName *string `json:"serverName,omitempty"`
	// Topic or subject of the memory (store only)
	Subject *string `json:"subject,omitempty"`
	// Arguments of the tool call being gated
	ToolArgs any `json:"toolArgs,omitempty"`
	// Tool call ID that triggered this permission request
	ToolCallID *string `json:"toolCallId,omitempty"`
	// Description of what the custom tool does
	ToolDescription *string `json:"toolDescription,omitempty"`
	// Internal name of the MCP tool
	ToolName *string `json:"toolName,omitempty"`
	// Human-readable title of the MCP tool
	ToolTitle *string `json:"toolTitle,omitempty"`
	// URL to be fetched
	URL *string `json:"url,omitempty"`
	// Optional warning message about risks of running this command
	Warning *string `json:"warning,omitempty"`
}
```

`PermissionRequestKind` values:
- `"shell"` — shell command execution
- `"write"` — file write
- `"read"` — file read
- `"mcp"` — MCP tool call
- `"url"` — URL fetch
- `"memory"` — memory store/vote
- `"custom-tool"` — custom tool
- `"hook"` — hook

## Probe strategy

Register an `OnPermissionRequest` callback that:
1. Matches ANY tool call where `Kind == "shell"` (i.e.,
   `PermissionRequestKindShell`)
2. Returns an error (which the SDK maps to the deny decision and sends to CLI
   before proceeding)
3. Captures (a) whether the hook was called, (b) whether the underlying tool
   executed anyway

Use a concrete side effect (canary file creation at `/tmp/rung3-canary.txt`) to
confirm the tool did not run.
