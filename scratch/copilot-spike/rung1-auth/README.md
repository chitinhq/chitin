# Rung 1: SDK install + Enterprise auth

## What the SDK docs say

- Install: `go get github.com/github/copilot-sdk/go`
- Import path: `github.com/github/copilot-sdk/go` (package alias: `copilot`)
- Auth mechanism: **Signed-in CLI credentials** (default). The Go SDK uses the `copilot` CLI binary as a subprocess over JSON-RPC/stdio. Auth is inherited from the CLI's keychain session (the same session used by `gh auth`). Alternatively: `COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN` env vars; or pass `GitHubToken` in `ClientOptions`.
- Minimal example: https://github.com/github/copilot-sdk/blob/main/docs/setup/local-cli.md (Go tab)
- CLI binary location on this box: `/home/red/.vite-plus/bin/copilot` (version 1.0.35)

**Key Go SDK constraint:** Unlike Node.js/Python/.NET, the Go SDK does **not** bundle the CLI. You must supply `CLIPath` in `ClientOptions` or set `COPILOT_CLI_PATH` env var. This is a Go-only requirement.

## Probe strategy

Write a Go program that:
1. Imports `github.com/github/copilot-sdk/go` as `copilot`
2. Creates a `Client` with `CLIPath` pointing to the installed CLI binary
3. Calls `client.Start(ctx)` — this spawns the CLI subprocess and establishes JSON-RPC
4. Creates a session with `client.CreateSession(ctx, &copilot.SessionConfig{...})`
5. Sends one prompt via `session.SendAndWait` — the lowest-overhead auth-requiring round-trip
6. Prints the response content or the auth error to stdout

## Expected output

A successful run should print a short assistant reply to the prompt (e.g., "Hello! How can I help you today?") followed by a clean exit. If auth fails, the CLI subprocess will emit a JSON-RPC error that the SDK surfaces as a Go `error` return from `Start()` or `CreateSession()`.
