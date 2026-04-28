# Rung 3 Result

**Pass/Fail:** pass

**Date/Time:** 2026-04-24T01:08:36Z

## Evidence

See `block-proof.txt`.

```
INTERCEPTOR_CALLED: true
PERMISSION_KIND_SEEN: shell
COMMAND_SEEN: echo canary > /tmp/rung3-canary.txt
CANARY_FILE_EXISTS: no
SIDE_EFFECT_OBSERVED: no side effect
```

## Hook mechanism confirmed

- **API surface:** `copilot.PermissionHandlerFunc` — the `OnPermissionRequest`
  field of `copilot.SessionConfig`, passed to `client.CreateSession`.

- **Registration shape:**
  ```go
  session, err := client.CreateSession(ctx, &copilot.SessionConfig{
      Model: "gpt-4.1",
      OnPermissionRequest: func(req copilot.PermissionRequest, inv copilot.PermissionInvocation) (copilot.PermissionRequestResult, error) {
          if req.Kind == copilot.PermissionRequestKindShell {
              return copilot.PermissionRequestResult{}, errors.New("denied")
          }
          return copilot.PermissionRequestResult{Kind: copilot.PermissionRequestResultKindApproved}, nil
      },
  })
  ```

- **Refusal signal:** Return a non-nil `error`. The SDK maps this to
  `PermissionDecisionKindDeniedNoApprovalRuleAndCouldNotRequestFromUser` and
  sends that decision to the CLI subprocess via `HandlePendingPermissionRequest`
  RPC call, which runs **after** the handler returns.

- **Synchronicity confirmed:** yes — canary absent implies interceptor blocked
  before tool executed. Observed event sequence:
  ```
  tool.execution_start   ← Copilot signals intent
  permission.requested   ← SDK calls our handler (we return deny)
  permission.completed   ← SDK sent deny decision to CLI
  tool.execution_complete ← CLI reports refused execution (no side effect)
  ```
  The shell command (`echo canary > /tmp/rung3-canary.txt`) was **never run**.
  `/tmp/rung3-canary.txt` does not exist post-run.

## SDK-side response to refusal

What does Copilot CLI do when its tool call is refused?

- **Response shape:** The CLI emits `tool.execution_complete` immediately after
  `permission.completed` (no retry or rephrase within the same turn). The model
  then starts a new `assistant.turn_start`, presumably to acknowledge or
  rephrase given the refusal.
- **Impact on session:** Session continues — a second `assistant.turn_start`
  event fired, indicating the model was handed back the refusal result and
  began composing a follow-up response. Session did not terminate.
- **Any user-visible diagnostic:** The model received the refusal as a tool
  result; in a UI it would likely show an error or explain it cannot complete
  the request.

## Time taken

Start: 2026-04-24T01:08:30Z
End:   2026-04-24T01:08:36Z
Wall:  ~6 minutes (including README, code, tidy, compile-fix, run)

## Surprises

1. **`tool.execution_complete` still fires on refusal.** The event fires even
   when the tool was denied — it represents the completed decision cycle (not
   successful execution). The absence of the canary file is the only definitive
   proof the shell did not run.

2. **Second `assistant.turn_start` fires after refusal.** The model is given
   the refusal outcome and continues the session. This means governance can
   deny silently and the agent will attempt to rephrase or acknowledge — useful
   context for Rung 4 (chitin-kernel gate integration).

3. **Event ordering is deterministic.** The sequence
   `permission.requested → permission.completed → tool.execution_complete` was
   consistent. No race observed between permission dispatch and tool execution.
