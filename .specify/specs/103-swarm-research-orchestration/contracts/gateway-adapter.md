# Contract — Gateway adapters (OpenClaw CLI + Hermes MCP)

Two adapters at `go/orchestrator/internal/gateway/`. Both wrap an external binary via `os/exec`. Both enforce the **security boundary** invariant (FR-008).

## Common interface

```go
package gateway

type GatewayClient interface {
    Send(ctx context.Context, in SendInput) (SendOutput, error)
}

type SendInput struct {
    Session       string         // gateway session id
    Message       string         // literal message; chitin prepends "skills:..." prefix if applicable
    Timeout       time.Duration  // hard ceiling; SIGKILL on exceed
    WaitForReply  bool           // currently honored only by Hermes MCP
}

type SendOutput struct {
    Sent      bool      // true iff the binary returned exit 0
    Stdout    string    // captured; truncated to 64 KiB
    Stderr    string    // captured; truncated to 64 KiB
    ExitCode  int
    Elapsed   time.Duration
    Reply     *string   // populated iff WaitForReply succeeded
}
```

## OpenClawClient (FR-006)

```bash
$BIN sessions send --session <S> --message <M>
```

- Binary: `openclaw` (`os.LookPath("openclaw")`); injectable for tests
- No env modifications — relies on operator's existing CLI auth
- Stdout/stderr captured; non-zero exit code is NOT an error from the adapter's perspective — it's a `SendOutput` with `Sent=false, ExitCode=N`. Workflow decides retry policy.
- Context cancellation: SIGTERM, 5s grace, SIGKILL

## HermesMCPClient (FR-007)

```bash
$BIN mcp serve
```

Spawns the binary with stdin/stdout attached. JSON-RPC envelope:

```json
// → request
{"jsonrpc": "2.0", "id": 1, "method": "messages_send", "params": {"channel": "<S>", "message": "<M>"}}
// ← response
{"jsonrpc": "2.0", "id": 1, "result": {"sent": true, "message_id": "<id>"}}

// → optional follow-up if WaitForReply
{"jsonrpc": "2.0", "id": 2, "method": "events_wait", "params": {"channel": "<S>", "timeout_ms": <ms>}}
// ← optional follow-up response
{"jsonrpc": "2.0", "id": 2, "result": {"reply": "<text>"}}
```

- Binary: `hermes`; injectable for tests
- Env: `HERMES_HOME=/home/red/.hermes` (configurable via `HermesMCPClient.HomeDir`)
- Subprocess gets SIGTERM → 5s grace → SIGKILL on timeout/cancel
- Subprocess MUST NOT be reused across activities (one-shot)

## Security boundary (FR-008) — load-bearing

Both adapters MUST NOT:
- Open a listener port
- Set up a tunnel, reverse proxy, or remote-callable wrapper
- Forward gateway traffic over HTTP / WebSocket / gRPC
- Expose the subprocess stdio over a network socket

Enforcement: code review. Any PR touching `internal/gateway/` that introduces `net.Listen`, `http.Server`, `httptest.NewServer`, or similar gets rejected unless the FR-008 implications are explicitly argued.

The Go compiler can't enforce this. The invariant lives in this contract.

## Kernel-gate compliance (§1)

Every adapter `Send` call ends up in a subprocess execution. The kernel's PreToolUse hook intercepts at the OS level — the adapter doesn't need to call `chitin-kernel gate evaluate` directly.

The chain emits `swarm_invocation` BEFORE the subprocess starts (so the gate sees the call attempt) and the subprocess's own tool-call hooks emit further events during execution. This is the existing kernel observation pattern; nothing new for spec 103.

## Activity wrappers

```go
// activities/swarm/send_openclaw.go
func SwarmSendOpenClaw(ctx context.Context, in SendInput) (SendOutput, error)

// activities/swarm/send_hermes.go
func SwarmSendHermes(ctx context.Context, in SendInput) (SendOutput, error)
```

Each is the Temporal-activity wrapper around the corresponding client. Activities own the timeout, retry, and chain-emit policy; the client is pure shell-out logic.

Activity timeout default: 5 minutes (matching FR edge case "`hermes mcp serve` subprocess hangs").
Retry policy: max 2 attempts, exponential backoff base 1s, no retry on `exit code == 130` (SIGINT).

## Session resolution (FR-009)

```go
// internal/gateway/session_resolver.go
type SessionResolver interface {
    Resolve(ctx context.Context, agent, gateway, declared string) (string, error)
}
```

If schedule entry's `gateway_session` is non-empty → use as-is.
If empty → call `<gateway> sessions list`, pick the session whose name matches the agent (case-insensitive), error if zero or multiple match.

## Edge cases

- **Gateway binary not on PATH:** activity returns error, workflow fails with `gateway_not_installed`. Chain emits `swarm_invocation_failed`.
- **Session not found:** error with `session_unresolved: <agent> via <gateway>`.
- **Subprocess hangs past timeout:** SIGKILL + chain emit `swarm_invocation_timeout`.
- **Subprocess crashes mid-send:** captured stderr in SendOutput; workflow logs and retries per policy.
