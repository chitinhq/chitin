# Rung 1 Result

**Pass/Fail:** pass

**Date/Time:** 2026-04-24T00:50:25Z

## Evidence

```
[rung1] start: 2026-04-24T00:50:20Z
[rung1] cli-path: /home/red/.vite-plus/bin/copilot
[rung1] client started (CLI subprocess running)
[rung1] session created
[rung1] PASS — response: "AUTH_OK"
[rung1] end: 2026-04-24T00:50:25Z  wall: 4.697s
```

Exit code: 0. No errors. Full round-trip completed: CLI subprocess spawned, JSON-RPC negotiated, session created, prompt sent, assistant reply received.

## SDK version

```
github.com/github/copilot-sdk/go v0.2.2
```

(from go.sum: `github.com/github/copilot-sdk/go v0.2.2 h1:...`)

## Auth mechanism used

**gh-auth / CLI keychain** — `UseLoggedInUser` default (true). No token env vars set. The SDK's `NewClient` with `CLIPath` spawned the existing CLI binary which used the credentials stored in the system keychain by the prior `gh auth login` session (`jpleva91`, scopes: `admin:org admin:public_key gist repo`).

## Time taken

Start: 2026-04-24T00:50:20Z
End:   2026-04-24T00:50:25Z
Wall:  ~5s (4.697s measured)

## Surprises

1. **Go SDK does not bundle the CLI** — unlike Node.js/Python/.NET, the Go SDK is a pure JSON-RPC client. You *must* supply `CLIPath` or `COPILOT_CLI_PATH`. This is not highlighted prominently in the main setup index; it appears only as a note in the bundled-cli guide's Go tab and in local-cli.md. Chitin must either (a) ship the CLI binary, (b) install it as a side-effect, or (c) document that the user has it on `PATH`. For the spike this was a non-issue since the CLI was already at `/home/red/.vite-plus/bin/copilot` (version 1.0.35).

2. **`SendAndWait` is the right probe API** — the README's Quick Start uses `Send` + event loop, but `SendAndWait` exists and is cleaner for synchronous probes. It returns a single `*SessionEvent` when the session goes idle.

3. **`OnPermissionRequest` is required in `SessionConfig`** — `CreateSession` panics if omitted. `copilot.PermissionHandler.ApproveAll` is the no-op stub.

4. **No `ListModels` or `Ping` equivalent** — the lowest-overhead auth-requiring call is `CreateSession` + `SendAndWait`. There is a `client.Ping(message)` method but it only pings the JSON-RPC layer, not the Copilot backend. First real auth happens at session creation / first send.

5. **Model `gpt-4.1` is the documented default** — not `gpt-5` (which the README's Quick Start example uses). `gpt-5` may be unavailable on this seat; `gpt-4.1` succeeded.
