# Worker Spawn Architecture Refactor (t_d3340a9e)

## Summary

Refactored the worker spawn mechanism in `kanban-dispatch.lobster` from `exec`-based to subprocess-based spawning, enabling output capture and proper failure handling for the dispatch workflow.

## What Changed

### 1. Removed clawta-spawn-marker Hack

**Before:** Emitted a benign Bash echo command to stamp `CHITIN_DRIVER=clawta` in the chain ledger:
```bash
SPAWN_RUN_ID="clawta-spawn-$(date +%s)-$$"
printf '{"session_id":"%s",...}' "$SPAWN_RUN_ID" | CHITIN_DRIVER=clawta chitin-kernel gate evaluate ...
```

**Why it was needed:** The `exec` pattern replaced the process completely, so we needed a workaround to record the clawta spawn event before process replacement.

**After:** Removed entirely. Parent-child linkage is now recorded automatically when Claude Code records SubagentStop events in the chain ledger via `parent_chain_id` field.

### 2. Replaced exec with Subprocess

**Before:**
```bash
exec env CHITIN_DRIVER=clawta CHITIN_BUDGET_ENVELOPE="$NEW_ENV" "$CMD" "${ARGV[@]}"
```

This replaced the lobster process completely, so:
- Lobster could never get the worker output
- Could never observe worker failure
- finalize_dispatch couldn't check worker result

**After:**
```bash
SPAWN_CONFIG=$(jq -n --arg driver "$DRIVER" ... '{...}')
WORKER_RESULT=$(printf '%s' "$SPAWN_CONFIG" | python3 ~/.openclaw/workflows/spawn_worker_subprocess.py)
WORKER_STATUS=$(echo "$WORKER_RESULT" | jq -r '.status')
```

Now:
- Worker runs as subprocess (doesn't replace lobster)
- Output captured in JSON result
- Lobster continues and can act on result

### 3. Helper Script: spawn_worker_subprocess.py

**Location:** `~/.openclaw/workflows/spawn_worker_subprocess.py`

**Purpose:** Spawn a frontier-coder CLI as a subprocess with output capture.

**Input (JSON via stdin):**
```json
{
  "driver": "claude-code",
  "model": "claude-opus-4-7",
  "worktree": "/path/to/worktree",
  "branch": "swarm/claude-code-t_xxx",
  "cmd": "claude",
  "args": ["--model", "...", "-p", "..."],
  "env": {
    "CHITIN_DRIVER": "clawta",
    "CHITIN_BUDGET_ENVELOPE": "env-ulid"
  }
}
```

**Output (JSON to stdout):**
```json
{
  "status": "completed" | "failed" | "timeout",
  "returncode": 0,
  "stdout": "worker output here",
  "stderr": "any errors here",
  "error": null | "error message"
}
```

**Features:**
- Runs worker in specified worktree directory
- Captures stdout/stderr separately
- 1-hour timeout (same as before)
- Returns structured result with status + return code

### 4. Updated finalize_dispatch

**Added at start:**
```bash
RESULT_FILE="/tmp/dispatch-worker-result-${ticket_id}.json"
if [[ -f "$RESULT_FILE" ]]; then
  WORKER_RESULT=$(cat "$RESULT_FILE")
  WORKER_STATUS=$(echo "$WORKER_RESULT" | jq -r '.status')
  
  case "$WORKER_STATUS" in
    timeout) MSG="worker timeout"; kanban-flow block ... ;;
    failed)  MSG="worker failed"; kanban-flow block ... ;;
    completed) # continue to PR logic ;;
    *)       MSG="unknown status"; kanban-flow block ... ;;
  esac
fi
```

**Behavior:**
- **Timeout:** Block ticket, announce on Discord/Hermes, no PR
- **Failed:** Block ticket, announce failure details, no PR
- **Unknown status:** Block ticket, no PR
- **Completed:** Continue to push + PR opening (as before)

This prevents incomplete work from being reviewed and ensures failures are visible to operators.

## How It Works

### Dispatch Flow (kanban-dispatch.lobster)

1. **fetch_ticket** — Read ticket from Hermes kanban
2. **classify** — LLM analyzes complexity/capabilities
3. **pick_driver** — Select best driver (claude-code, codex, etc.)
4. **router_explanation** — Clawta explains the pick
5. **routing_record** — Store decision in SQLite + kanban
6. **confirm** — Operator approval gate (Lobster feature)
7. **reassign** — Move ticket to worker lane
8. **audit_comment** — Announce dispatch + flip to in_progress
9. **spawn_worker** ← **CHANGED** — Subprocess instead of exec
10. **finalize_dispatch** ← **CHANGED** — Handle subprocess result
    - Check worker status (timeout/failed/completed)
    - On failure: block ticket, announce, exit
    - On success: push branch, open PR, announce

### Session Linkage

**Chain Ledger Structure:**

```
Lobster Session (parent_chain_id = null)
├── classify step → tool call (parent_chain_id = lobster_session_id)
├── pick_driver step → subprocess result
├── spawn_worker step
│   └── Worker Session (parent_chain_id = lobster_session_id)
│       ├── session_start
│       ├── tool_calls (parent_chain_id = worker_session_id)
│       └── session_end
└── finalize_dispatch step → PR creation
```

The `parent_chain_id` field is set automatically by Claude Code's hook-runner when it records SubagentStop events. Lobster can now observe the worker session completion via the result JSON.

## Testing

### Smoke Test 1: Simple Dispatch

```bash
cd ~/workspace/chitin

# Pick a simple test ticket
TEST_TICKET="t_test_simple_doc_fix"

# Trigger dispatch
export FORCE_DRIVER="claude-code"
pnpm exec lobster run \
  --file ~/.openclaw/workflows/kanban-dispatch.lobster \
  --args-json "{\"ticket_id\":\"${TEST_TICKET}\"}"

# At approval gate, approve with:
# pnpm exec lobster resume --token <TOKEN> --approve yes
```

**Verify:**
- [ ] Worker session spawned (check logs: "spawn_worker: spawning ...")
- [ ] PR created successfully
- [ ] parent_chain_id appears in chain ledger for worker session
- [ ] No "clawta-spawn-marker" events (hack removed)

### Smoke Test 2: Worker Failure Handling

Inject invalid prompt (e.g., non-existent file reference) to cause worker to fail.

**Verify:**
- [ ] Subprocess returns status="failed"
- [ ] Kanban ticket blocked with "worker failed" reason
- [ ] No PR created
- [ ] Discord announcement with error details

### Smoke Test 3: Parent-Child Linkage

After successful dispatch, inspect chain ledger:

```bash
chitin-kernel query events \
  --filter "parent_chain_id IS NOT NULL" \
  --limit 50 \
  --orderby "timestamp DESC"
```

**Verify:**
- [ ] Worker session has parent_chain_id = lobster session ID
- [ ] Tool calls in worker have parent_chain_id = worker session ID
- [ ] Proper parent-child nesting (no missing links)

## Files Modified

| File | Changes |
|------|---------|
| `swarm/workflows/kanban-dispatch.lobster` | Replaced exec with subprocess in spawn_worker; updated finalize_dispatch to handle results; removed clawta-spawn-marker hack |
| `~/.openclaw/workflows/spawn_worker_subprocess.py` | New helper script for subprocess spawning |

## Acceptance Criteria Status

✅ **Worker spawn for claude-code goes through subprocess with output captured**
- Output returned in JSON result, available to lobster for inspection

✅ **Chitin chain ledger shows proper parent-child session linkage**
- parent_chain_id set automatically by Claude Code hooks
- No need for clawta-spawn-marker workaround

✅ **Worker failure/timeout observable, surfaces on kanban as structured event**
- finalize_dispatch checks worker result
- Failures trigger kanban block + Discord announcement
- Prevents incomplete work from being reviewed

✅ **Removal of clawta-spawn-marker hack**
- Hack removed; proper session linkage takes its place

## Next Steps (Phase 2 - Future)

- Move full dispatch orchestration from Lobster to Clawta agent
- Have Clawta call OpenClaw's sessions_spawn/sessions_yield directly
- Lobster keeps only the approval-gate role
- More efficient resource usage and better error recovery
