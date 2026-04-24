# Copilot SDK Feasibility Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a go/no-go verdict within 2 calendar days on whether chitin can integrate Copilot CLI via the GitHub Copilot Go SDK with inline governance, so the talk on 2026-05-07 can be scheduled on Option Y (SDK-embedded) or fall back to Option X (hermes-style external wrap) with 12 days of runway remaining.

**Architecture:** 4-rung ladder probe. Each rung tests one primitive of the integration (auth → observe → intercept → gate+log). Early-exit on first failed rung with documented blocker. Artifacts committed on spike branch. Structured findings report is the deliverable, committed to the specs directory so the follow-up v1 spec can cite it.

**Tech Stack:** Go 1.25 (chitin toolchain), `github.com/github/copilot-sdk` (Go client), GitHub Copilot Enterprise credentials (operator already holds), existing `chitin-kernel gate evaluate` binary (shipped in PR #45).

**Parent spec:** `docs/superpowers/specs/2026-04-23-copilot-sdk-spike-design.md`

**Dispatch:** Subagent-driven, fresh subagent per task. Operator reviews between tasks and makes the go/no-go call on the subagent's findings.

---

## Task 0: Worktree + branch setup

**Files:**
- Create: `~/workspace/chitin-spike-copilot-sdk/` (git worktree)
- Create: `~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/README.md`
- Create: `~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/.gitignore`

**Context:** Operator convention requires a dedicated worktree for any branch work. The spike branch is `spike/copilot-sdk-feasibility` off current origin/main. All probe code lives under `scratch/copilot-spike/` so it's clearly throwaway and won't contaminate `libs/`, `go/`, or `apps/`.

- [ ] **Step 1: Create the spike worktree**

```bash
cd ~/workspace/chitin
rtk git fetch origin
rtk git worktree add ~/workspace/chitin-spike-copilot-sdk -b spike/copilot-sdk-feasibility origin/main
cd ~/workspace/chitin-spike-copilot-sdk
rtk git status
```

Expected: Worktree at `~/workspace/chitin-spike-copilot-sdk/`; branch `spike/copilot-sdk-feasibility` checked out; status clean.

- [ ] **Step 2: Create scratch directory with README**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
mkdir -p scratch/copilot-spike
```

Write `scratch/copilot-spike/README.md`:

```markdown
# Copilot SDK Feasibility Spike

2-day time-boxed probe of the GitHub Copilot Go SDK to determine whether
chitin can integrate Copilot CLI with inline governance for a live demo
on 2026-05-07.

See `docs/superpowers/specs/2026-04-23-copilot-sdk-spike-design.md` for
the full spec and `docs/superpowers/plans/2026-04-23-copilot-sdk-spike.md`
for the execution plan.

## Directory layout

- `rung1-auth/` — SDK install + Enterprise auth probe
- `rung2-observe/` — JSON-RPC stream observation probe
- `rung3-intercept/` — Pre-execution intercept probe
- `rung4-gate/` — End-to-end gate + decision log probe

Each directory has its own `main.go`, `go.mod`, `README.md`, and
`RESULT.md` (evidence).

The findings report lands at
`docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md` when
the ladder completes.

## Running a rung locally

    cd scratch/copilot-spike/rung<N>-<name>/
    go mod tidy
    go run main.go

Credentials expected in the operator's existing gh-auth config. Do not
commit secrets to this directory.
```

Expected: `scratch/copilot-spike/README.md` present with the above content.

- [ ] **Step 3: Create scratch directory .gitignore**

Write `scratch/copilot-spike/.gitignore`:

```
*.token
*.key
.env
.env.*
*/bin/
*/build/
*/dist/
```

Expected: gitignore prevents credential and built-artifact leaks.

- [ ] **Step 4: Commit scaffold**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add scratch/copilot-spike/
rtk git commit -m "$(cat <<'EOF'
spike: scaffold copilot-sdk feasibility probe directory

Per docs/superpowers/specs/2026-04-23-copilot-sdk-spike-design.md —
creates scratch/copilot-spike/ for the 4-rung ladder. No probe code
yet; rungs committed individually in subsequent tasks.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
rtk git log --oneline -3
```

Expected: Commit lands on `spike/copilot-sdk-feasibility`; log shows the scaffold commit above recent main commits.

---

## Task 1: Rung 1 — SDK install + Enterprise auth

**Day/Window:** Day 1 AM

**Files:**
- Create: `scratch/copilot-spike/rung1-auth/main.go`
- Create: `scratch/copilot-spike/rung1-auth/go.mod`
- Create: `scratch/copilot-spike/rung1-auth/README.md`
- Create: `scratch/copilot-spike/rung1-auth/RESULT.md`

**Pass criterion:** A minimal Go program using `github.com/github/copilot-sdk` authenticates with the operator's Enterprise credentials and completes one round-trip to the Copilot backend without error.

**Kill conditions (force no-go, abort ladder):**
- Enterprise auth requires an org-level permission the operator does not control
- SDK install requires a platform the operator does not run

**Context:** The operator holds GitHub Enterprise with Copilot. `gh auth status` should show authenticated state for the same credentials. The SDK's setup docs (`https://github.com/github/copilot-sdk/blob/main/docs/setup/index.md`) cover install and auth flow; read before writing code.

- [ ] **Step 1: Read the Copilot Go SDK setup docs and document findings**

Fetch `https://github.com/github/copilot-sdk/blob/main/docs/setup/index.md` and walk the Go section. Look for: (a) exact Go install / `go get` path, (b) auth mechanism (OAuth browser flow, token file, gh-config reuse, env var), (c) minimal working Go example, (d) import path.

Write `scratch/copilot-spike/rung1-auth/README.md`:

```markdown
# Rung 1: SDK install + Enterprise auth

## What the SDK docs say

- Install: <exact command observed>
- Import path: <exact Go import>
- Auth mechanism: <mechanism observed>
- Minimal example: <link or inline reference>

## Probe strategy

Write a Go program that:
1. Imports the SDK
2. Loads credentials via <mechanism>
3. Creates a client
4. Makes one auth-requiring call (e.g., ListModels, Whoami — whatever the SDK exposes as the lowest-overhead auth-requiring call)
5. Prints the result or the auth error

## Expected output

<what a successful run should print, per the docs>
```

- [ ] **Step 2: Initialize the Rung 1 Go module**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung1-auth
go mod init chitin-spike/rung1-auth
go get <exact-import-path-from-step-1>
cat go.mod
```

Expected: `go.mod` shows the SDK dependency resolved; `go.sum` written; no errors.

**KILL CHECK:** If `go get` fails with "package not found," "module not supported," or "platform not supported," proceed directly to Task 5 with verdict `no-go, blocked at Rung 1 — SDK Go package unavailable or unsupported on this platform`. Skip remaining rungs.

- [ ] **Step 3: Write the auth probe**

Based on the minimal example from Step 1, write `scratch/copilot-spike/rung1-auth/main.go` that:
1. Imports the SDK package
2. Loads credentials per the mechanism identified in Step 1
3. Creates an SDK client
4. Makes ONE auth-requiring call (the lowest-overhead one the SDK exposes — typically a `/user` or `/models` read)
5. Prints the response (or error) to stdout as structured text (not binary)

The program must be self-contained (no other chitin dependencies). Keep it under 60 lines.

- [ ] **Step 4: Run the auth probe**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung1-auth
go run main.go 2>&1 | tee /tmp/rung1-output.txt
```

Expected: Either (a) a parseable response from the SDK confirming auth worked, or (b) a clear auth error message.

**KILL CHECK:**
- Successful response → Rung 1 passes, proceed to Step 5
- Auth error is `org-level permission denied`, `seat not provisioned`, or similar unfixable operator-side error → write RESULT.md with `fail`, abort ladder, proceed to Task 5
- Auth error is fixable (token expired, `gh auth login` needed, etc.) → fix per SDK docs, re-run ONCE. If still failing, abort ladder, proceed to Task 5.

- [ ] **Step 5: Write RESULT.md with evidence**

Write `scratch/copilot-spike/rung1-auth/RESULT.md`:

```markdown
# Rung 1 Result

**Pass/Fail:** <pass | fail>

**Date/Time:** <ISO-8601 UTC>

## Evidence

<paste actual program output from /tmp/rung1-output.txt, with any
tokens, bearer strings, or session IDs redacted as <REDACTED>>

## SDK version

<from go.sum — copy the github.com/github/copilot-sdk line>

## Auth mechanism used

<which mechanism actually worked: gh-auth | env var | config file | other>

## Time taken

Start: <ISO-8601 UTC>
End:   <ISO-8601 UTC>
Wall:  <minutes>

## Surprises

<any nonobvious issues, rate-limits, warnings, undocumented behavior —
write "none" if the probe ran as expected>
```

- [ ] **Step 6: Verify no secrets in committed files**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
grep -riE 'bearer |token=|secret=|api_key|apikey' scratch/copilot-spike/rung1-auth/ || echo "clean"
```

Expected: `clean` or only `<REDACTED>` matches. If any actual-looking token slipped through, re-redact before committing.

- [ ] **Step 7: Commit Rung 1 artifacts**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add scratch/copilot-spike/rung1-auth/
rtk git commit -m "$(cat <<'EOF'
spike(rung1): SDK install + enterprise auth — <pass|fail>

<2-3 sentences: what cleared, what the auth mechanism was, any surprises>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: Commit lands. If RESULT.md shows `fail`, skip Tasks 2/3/4 and proceed directly to Task 5 (findings report).

---

## Task 2: Rung 2 — Observe JSON-RPC stream

**Day/Window:** Day 1 PM

**Files:**
- Create: `scratch/copilot-spike/rung2-observe/main.go`
- Create: `scratch/copilot-spike/rung2-observe/go.mod`
- Create: `scratch/copilot-spike/rung2-observe/README.md`
- Create: `scratch/copilot-spike/rung2-observe/RESULT.md`
- Create: `scratch/copilot-spike/rung2-observe/captured-stream.jsonl`

**Pass criterion:** The SDK, driven by a minimal Go program, spawns or communicates with Copilot CLI and produces an observable JSON-RPC message stream; at least one tool-call message is captured as structured data with a parseable tool name and parseable arguments.

**Kill conditions:**
- JSON-RPC stream is encrypted, gzipped without a parser, or exposes only high-level events (e.g., `session_started`) without tool-call granularity
- Tool calls appear in the stream but their arguments are free-form prose that can't be normalized to a canonical Action

**Context:** The SDK README says the SDK "communicates with the Copilot CLI via JSON-RPC." This rung confirms that stream is observable and tool calls are structured. We pick a prompt that WILL produce a tool call — something that requires the model to invoke a tool to answer (e.g., "list the files in /tmp using the available tools").

- [ ] **Step 1: Read the SDK protocol docs and document findings**

Walk `https://github.com/github/copilot-sdk/blob/main/docs/` tree — look for `protocol.md`, `architecture.md`, `streaming.md`, or similar. Read `sdk-protocol-version.json` at repo root for protocol version info. Also read `nodejs/` or `python/` SDK sources to understand the protocol shape if Go docs are sparse.

Write `scratch/copilot-spike/rung2-observe/README.md`:

```markdown
# Rung 2: Observe JSON-RPC stream

## Protocol facts from SDK docs + source

- Transport: <stdio pipe | unix socket | tcp | other>
- Framing: <length-prefixed | newline-delimited | other>
- Message schema: <link to spec, or inline summary>
- Tool-call message type: <name/tag/jsonrpc-method>
- Protocol version: <from sdk-protocol-version.json>

## Probe strategy

Use the Go SDK to:
1. Start a Copilot session
2. Tap the JSON-RPC stream (mechanism: <from docs — middleware option,
   raw-stream flag, or transport wrapping>)
3. Send a prompt designed to trigger a tool call
4. Log all observed messages to captured-stream.jsonl
5. Exit cleanly after first tool-call message OR a 30-second timeout

## Expected tool call shape (from schema)

<paste or sketch the JSON shape expected, per the schema>
```

- [ ] **Step 2: Initialize the Rung 2 Go module**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung2-observe
go mod init chitin-spike/rung2-observe
go get <same-SDK-import-as-Rung-1>
cat go.mod
```

Expected: Module initialized; SDK resolved.

- [ ] **Step 3: Write the observation probe**

Write `scratch/copilot-spike/rung2-observe/main.go` that:
1. Creates an SDK client (reuse auth pattern from Rung 1)
2. Registers a stream tap / middleware / transport wrapper per the mechanism identified in Step 1
3. Starts a Copilot session
4. Sends this exact prompt: `"List the files in /tmp using the shell tool. Just run the command; do not explain."`
5. Captures every JSON-RPC message (inbound and outbound) as a line in `captured-stream.jsonl`
6. Breaks out of the loop on the first message whose shape matches a tool call OR after 30 seconds wall-clock
7. Closes the session cleanly

Keep the file under 120 lines. If the SDK's Go surface doesn't expose stream-tap primitives directly, try: (a) the underlying transport interface, (b) setting an environment variable the SDK documents for logging (e.g., `COPILOT_SDK_TRACE=1`), (c) wrapping `io.Reader`/`io.Writer` the SDK accepts. Document which approach worked.

- [ ] **Step 4: Run the observation probe**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung2-observe
go run main.go 2>&1 | tee /tmp/rung2-output.txt
ls -la captured-stream.jsonl
wc -l captured-stream.jsonl
```

Expected: `captured-stream.jsonl` exists and has ≥ 1 line.

- [ ] **Step 5: Verify stream is parseable and tool call is structured**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung2-observe

# Every line must be valid JSON
cat captured-stream.jsonl | while IFS= read -r line; do
  echo "$line" | jq . > /dev/null || echo "UNPARSEABLE: $line"
done

# Find the tool-call message
cat captured-stream.jsonl | jq 'select(.method == "tool_call" or .params.tool_name or .tool or .name)'
```

Expected: Every line parses as JSON; at least one message has a recognizable tool-call shape with a tool name (string) and args (structured object or array, not free-form prose).

**KILL CHECK:**
- Stream file is empty or not observable → Rung 2 fails, abort (kill: stream opaque)
- Stream has entries but all are high-level (`session_started`, `message`) with no tool-call granularity → Rung 2 fails, abort (kill: no tool-call granularity)
- Tool-call entries exist but args are free-form prose (e.g., `{"content": "I will run ls /tmp"}` with no structured args) → Rung 2 fails, abort (kill: can't normalize to Action)
- Structured tool call observable → Rung 2 passes, proceed

- [ ] **Step 6: Redact captured stream of any credentials**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung2-observe

# Redact common credential patterns in place (make a backup first)
cp captured-stream.jsonl captured-stream.jsonl.bak
sed -i -E 's/"(token|bearer|authorization|api_key|apikey|secret)":\s*"[^"]*"/"\1": "<REDACTED>"/gi' captured-stream.jsonl

# Verify
grep -iE '(bearer |token=|secret=|api_key|apikey)' captured-stream.jsonl || echo "clean"
rm captured-stream.jsonl.bak
```

Expected: `clean`.

- [ ] **Step 7: Write RESULT.md**

Write `scratch/copilot-spike/rung2-observe/RESULT.md`:

```markdown
# Rung 2 Result

**Pass/Fail:** <pass | fail>

**Date/Time:** <ISO-8601 UTC>

## Evidence

- Stream captured: `captured-stream.jsonl` (<N> lines)
- Tap mechanism that worked: <from Step 3>

### Example tool-call message (redacted)

```json
<paste one full redacted tool-call message from captured-stream.jsonl>
```

## Protocol observations

- Transport confirmed: <what actually worked>
- Framing confirmed: <what actually worked>
- Message types seen in the capture: <list unique method/type values>
- Protocol version: <from the handshake if visible>

## Normalization feasibility

Can the tool-name + tool-args be mapped to chitin's canonical Action
(see `go/execution-kernel/internal/gov/action.go`)?

<yes | no>

If yes, example mapping:

- Copilot tool message: `<paste the shape>`
- Chitin Action: `Action{Type: "<type>", Target: "<target>", Path: "<path>"}`

If no: explain why — this is the no-go reason.

## Time taken

Start: <ISO-8601 UTC>
End:   <ISO-8601 UTC>
Wall:  <minutes>

## Surprises

<nonobvious issues, or "none">
```

- [ ] **Step 8: Commit Rung 2 artifacts**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add scratch/copilot-spike/rung2-observe/
rtk git commit -m "$(cat <<'EOF'
spike(rung2): observe JSON-RPC stream — <pass|fail>

<2-3 sentences: what cleared, tap mechanism that worked, normalization feasibility>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: Commit lands. If RESULT.md shows `fail`, skip Tasks 3/4 and proceed to Task 5.

---

## Task 3: Rung 3 — Intercept + synchronous block

**Day/Window:** Day 2 AM

**Files:**
- Create: `scratch/copilot-spike/rung3-intercept/main.go`
- Create: `scratch/copilot-spike/rung3-intercept/go.mod`
- Create: `scratch/copilot-spike/rung3-intercept/README.md`
- Create: `scratch/copilot-spike/rung3-intercept/RESULT.md`
- Create: `scratch/copilot-spike/rung3-intercept/block-proof.txt`

**Pass criterion:** The SDK exposes (directly or indirectly) a pre-execution hook that allows the Go program to inspect a tool call and synchronously refuse it, preventing Copilot CLI from executing the underlying command. One synthetic refusal is demonstrated end-to-end.

**Kill conditions:**
- SDK supports only post-hoc observation, not pre-execution intercept
- SDK supports intercept but only asynchronously (can't block the executing call)
- No mechanism to signal "refuse this tool call" back to Copilot CLI

**Context:** This is the governance primitive without which Option Y reduces to observability-only. Option X already provides synchronous enforcement via `pre_tool_call` in the hermes plugin — if the SDK can't match that, there's no advantage to in-kernel integration. This rung MUST confirm synchronous refusal, not just observation or post-exec logging.

- [ ] **Step 1: Identify the SDK's intercept mechanism**

Search the SDK Go source and docs for:
- Function/type names matching: `Before*`, `Middleware`, `Interceptor`, `Hook`, `Handler`, `PreToolCall`, `OnToolCall`
- Example code showing how to reject / cancel / error-return a tool call
- Return-value semantics: does returning an error, a specific sentinel, or `ctx.Cancel()` cancel the tool call before execution?
- Whether the client constructor accepts a hooks/middleware option, or whether hooks are registered post-construction

Write `scratch/copilot-spike/rung3-intercept/README.md`:

```markdown
# Rung 3: Intercept + synchronous block

## Intercept mechanism found

- API type: <hook name / interface / function signature>
- Signature in Go: <paste actual signature from SDK source>
- Registration: <how the hook is wired — constructor option, Register call, middleware slice>
- Refusal semantics: <how the handler signals refusal — error return, sentinel
  value, context cancel, false return, other>
- Synchronicity: <is the hook called in the same goroutine as the tool-exec
  call path? confirm from SDK source>

## Probe strategy

Register an interceptor that:
1. Matches ANY tool call with name == "<tool from Rung 2>"
2. Returns the refusal signal
3. Captures (a) whether the hook was called, (b) whether the
   underlying tool executed anyway

Use a real side-effect tool call whose non-execution is visible
(e.g., `ls /tmp` — we capture `/tmp` state before and after and confirm
it is unchanged, OR a write to a canary file and we confirm the canary
never appears).
```

**KILL CHECK (pre-code):** If no intercept mechanism is documented or discoverable in the Go SDK source, write RESULT.md with `fail` and abort, proceed to Task 5 with `no-go, blocked at Rung 3 — SDK has no pre-exec intercept`.

- [ ] **Step 2: Initialize the Rung 3 Go module**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung3-intercept
go mod init chitin-spike/rung3-intercept
go get <same-SDK-import-as-Rung-1>
cat go.mod
```

Expected: Module initialized.

- [ ] **Step 3: Write the intercept probe**

Write `scratch/copilot-spike/rung3-intercept/main.go` that:
1. Creates an SDK client with an interceptor registered per Step 1's mechanism
2. The interceptor: if the observed tool name matches the one from Rung 2, writes `INTERCEPTOR_CALLED: true\nTOOL_NAME_SEEN: <name>\n` to `block-proof.txt` and returns the refusal signal. Otherwise passes through.
3. Sends this prompt: `"Create a file at /tmp/rung3-canary.txt containing the word canary, using the shell tool. Just run the command; do not explain."`
4. Waits up to 15 seconds for completion (tool call refused by interceptor should short-circuit faster)
5. After the SDK call returns (whether with error, empty response, or success), appends to `block-proof.txt`:

```
REFUSAL_RETURNED: true
SDK_RESPONSE: <whatever the SDK surfaces after the refusal — copy verbatim>
CANARY_FILE_EXISTS: <read /tmp/rung3-canary.txt; write "yes" or "no">
SIDE_EFFECT_OBSERVED: <"no side effect" if canary absent AND no other /tmp changes, else describe>
```

Keep the file under 150 lines.

- [ ] **Step 4: Clean up any pre-existing canary and run the probe**

```bash
rm -f /tmp/rung3-canary.txt

cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung3-intercept
go run main.go 2>&1 | tee /tmp/rung3-output.txt

cat block-proof.txt
ls -la /tmp/rung3-canary.txt 2>&1
```

Expected:
- `block-proof.txt` contains `INTERCEPTOR_CALLED: true`
- `block-proof.txt` contains `CANARY_FILE_EXISTS: no`
- `block-proof.txt` contains `SIDE_EFFECT_OBSERVED: no side effect`
- `ls /tmp/rung3-canary.txt` returns `No such file or directory`

**KILL CHECK:**
- `INTERCEPTOR_CALLED: false` → Rung 3 fails, abort (kill: hook doesn't fire)
- `INTERCEPTOR_CALLED: true` but `CANARY_FILE_EXISTS: yes` → Rung 3 fails, abort (kill: refusal not honored — observation-only)
- Interceptor fires AFTER the canary appears (sequence visible in timestamps) → Rung 3 fails, abort (kill: async intercept doesn't gate)
- All three green → Rung 3 passes

- [ ] **Step 5: Clean up test artifact**

```bash
rm -f /tmp/rung3-canary.txt
```

Expected: Clean environment for Task 4.

- [ ] **Step 6: Write RESULT.md**

Write `scratch/copilot-spike/rung3-intercept/RESULT.md`:

```markdown
# Rung 3 Result

**Pass/Fail:** <pass | fail>

**Date/Time:** <ISO-8601 UTC>

## Evidence

See `block-proof.txt`.

## Hook mechanism confirmed

- API surface: <exact Go function / method / interface used>
- Registration shape: <code snippet, 3-5 lines max, showing registration>
- Refusal signal: <error / sentinel / context cancel / bool return>
- Synchronicity confirmed: <yes/no — evidence: canary absent implies
  interceptor blocked before tool executed>

## SDK-side response to refusal

What does Copilot CLI do when its tool call is refused?

- Response shape: <error surfaced to prompt | empty response | retry attempt | rephrase>
- Impact on session: <session continues | session terminates | unclear>
- Any user-visible diagnostic: <yes/no — what does it say>

## Time taken

Start: <ISO-8601 UTC>
End:   <ISO-8601 UTC>
Wall:  <minutes>

## Surprises

<nonobvious issues, or "none">
```

- [ ] **Step 7: Commit Rung 3 artifacts**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add scratch/copilot-spike/rung3-intercept/
rtk git commit -m "$(cat <<'EOF'
spike(rung3): intercept + synchronous block — <pass|fail>

<2-3 sentences: hook mechanism used, refusal semantics, whether the block was honored>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: Commit lands. If RESULT.md shows `fail`, skip Task 4 and proceed to Task 5.

---

## Task 4: Rung 4 — End-to-end gate + decision log

**Day/Window:** Day 2 PM

**Files:**
- Create: `scratch/copilot-spike/rung4-gate/main.go`
- Create: `scratch/copilot-spike/rung4-gate/go.mod`
- Create: `scratch/copilot-spike/rung4-gate/README.md`
- Create: `scratch/copilot-spike/rung4-gate/RESULT.md`
- Create: `scratch/copilot-spike/rung4-gate/gate-run.log`

**Pass criterion:** The intercept handler from Rung 3 shells out to `chitin-kernel gate evaluate`, honors the returned `Decision`, and verifies the resulting line lands in `~/.chitin/gov-decisions-<today>.jsonl`. One allow path (shell command not in baseline deny rules) and one block path (`rm -rf`) are both exercised.

**Kill conditions:**
- `chitin-kernel gate evaluate` invocation succeeds but its Decision cannot be honored by the SDK (Rung 3 refusal mechanism doesn't accept Decision-derived params)

**Soft blockers (document in findings, do NOT force no-go):**
- Decision log write fails, directory missing, or permissions blocked — fixable wiring, flag it

**Context:** Rungs 1-3 prove the primitives; Rung 4 proves chitin's existing gate binary (PR #45) composes with them. If this rung passes, the full Option Y pattern is viable — chitin's gov machinery (already shipped) drives the Copilot intercept (newly proven).

- [ ] **Step 1: Confirm chitin-kernel is built and available**

```bash
cd ~/workspace/chitin-spike-copilot-sdk

which chitin-kernel 2>&1 || (
  cd go/execution-kernel
  go build -o ~/workspace/chitin-spike-copilot-sdk/bin/chitin-kernel ./cmd/chitin-kernel
  echo "built: ~/workspace/chitin-spike-copilot-sdk/bin/chitin-kernel"
)

# Use the local build explicitly for this rung
export PATH="${HOME}/workspace/chitin-spike-copilot-sdk/bin:${PATH}"
chitin-kernel --version
chitin-kernel gate evaluate --help 2>&1 | head -30
```

Expected: `chitin-kernel` resolves to the local build; `--version` prints; `gate evaluate --help` shows flags `--tool`, `--args-json`, `--agent`, `--cwd`.

- [ ] **Step 2: Confirm baseline policy + decision-log path**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
cat chitin.yaml | head -40
ls -la ~/.chitin/ 2>&1 || mkdir -p ~/.chitin && echo "~/.chitin created"
```

Expected: `chitin.yaml` exists at repo root with baseline rules (`no-destructive-rm`, `no-force-push`, etc.); `~/.chitin/` exists and is writable.

- [ ] **Step 3: Smoke-test the gate binary standalone (decouple SDK wiring from gate wiring)**

```bash
# Allow path smoke test
chitin-kernel gate evaluate \
  --tool=terminal \
  --args-json='{"command":"ls /tmp"}' \
  --agent=copilot-spike-smoke \
  --cwd="$(pwd)"
echo "allow exit: $?"

# Block path smoke test
chitin-kernel gate evaluate \
  --tool=terminal \
  --args-json='{"command":"rm -rf /tmp/never-exists-copilot-spike"}' \
  --agent=copilot-spike-smoke \
  --cwd="$(pwd)"
echo "block exit: $?"

# Confirm decision log has both lines
tail -5 ~/.chitin/gov-decisions-$(date -u +%Y-%m-%d).jsonl
```

Expected: Allow exit 0 with `{"allowed": true, ...}` on stdout; block exit 1 with `{"allowed": false, "mode": "guide", "reason": "Recursive delete is blocked...", ...}`; decision log tail shows two new lines with `agent: copilot-spike-smoke`.

- [ ] **Step 4: Initialize the Rung 4 Go module**

```bash
cd ~/workspace/chitin-spike-copilot-sdk/scratch/copilot-spike/rung4-gate
go mod init chitin-spike/rung4-gate
go get <same-SDK-import-as-Rung-1>
cat go.mod
```

Expected: Module initialized.

- [ ] **Step 5: Write the end-to-end probe**

Write `scratch/copilot-spike/rung4-gate/main.go` that:

1. Creates an SDK client with an interceptor registered (reuse Rung 3's mechanism)
2. The interceptor, on each tool call:
   a. Normalizes the observed tool call to a chitin Action shape. Minimal mapping for this rung:
      - Copilot tool name matching a shell-family tool (`shell`, `terminal`, `bash`, `exec`) → `action.Type = "shell.exec"`
      - Args: concatenate argv / use the `command` field / whatever the Rung 2 findings showed → `action.Target`
   b. Marshals `{"command": "<args>"}` as the `--args-json` payload
   c. Shells out: `chitin-kernel gate evaluate --tool=terminal --args-json=<j> --agent=copilot-spike --cwd=<repo-root>`
   d. Captures stdout as a `Decision` struct (unmarshal JSON). Fields needed: `Allowed`, `Reason`, `Suggestion`, `RuleID`.
   e. If `Decision.Allowed == true`: return the allow signal from the interceptor (pass-through)
   f. If `Decision.Allowed == false`: return the refusal signal from the interceptor (same as Rung 3)
   g. Append one line to `gate-run.log` per decision: `<timestamp>\t<scenario>\t<allowed>\t<rule_id>\t<command>`
3. Runs TWO scenarios sequentially:
   - **Scenario A (allow):** prompt `"Run ls /tmp using the shell tool. Just run the command; do not explain."` — expect allow, tool executes
   - **Scenario B (block):** prompt `"Create a temp directory /tmp/copilot-spike-test-dir containing canary.txt, then delete the whole directory tree with rm -rf. Just run the commands."` — expect the `rm -rf` to be blocked while any preceding mkdir/touch may or may not run (we only verify the destructive command is blocked)
4. Before Scenario B, creates `/tmp/copilot-spike-test-dir/canary.txt` manually so we can verify it's NOT deleted
5. After each scenario, writes summary to stdout

Keep under 250 lines.

- [ ] **Step 6: Run the end-to-end probe**

```bash
cd ~/workspace/chitin-spike-copilot-sdk

# Ensure chitin-kernel on PATH for the subprocess
export PATH="${HOME}/workspace/chitin-spike-copilot-sdk/bin:${PATH}"

# Pre-place the canary
mkdir -p /tmp/copilot-spike-test-dir
echo canary > /tmp/copilot-spike-test-dir/canary.txt
ls /tmp/copilot-spike-test-dir/

# Run
cd scratch/copilot-spike/rung4-gate
go run main.go 2>&1 | tee /tmp/rung4-output.txt

# Verify block path worked
ls /tmp/copilot-spike-test-dir/canary.txt 2>&1
# Expected: file still exists (block succeeded)

# Verify decision log has both entries
tail -20 ~/.chitin/gov-decisions-$(date -u +%Y-%m-%d).jsonl | grep copilot-spike
# Expected: at least 2 lines — one allow, one block, agent=copilot-spike

# Review the probe's own log
cat gate-run.log
```

Expected:
- Scenario A: `allowed=true`, `ls /tmp` executed successfully, output in stdout
- Scenario B: `allowed=false`, `rm -rf` rejected, `/tmp/copilot-spike-test-dir/canary.txt` STILL EXISTS
- Decision log has both entries tagged `agent=copilot-spike`
- `gate-run.log` has both scenarios' one-line summaries

**KILL CHECK:**
- Gate invocation returns non-zero-for-unexpected-reason exit code (not 0=allow, 1=deny) → document as soft blocker, check if fixable
- Gate decision `Allowed=false` but `/tmp/copilot-spike-test-dir/canary.txt` is GONE (block not honored) → Rung 4 fails, abort (kill: gate-to-SDK composition broken)
- Decision log is missing both entries → soft blocker (wiring issue), document in findings but do NOT automatically fail

- [ ] **Step 7: Clean up test artifacts**

```bash
rm -rf /tmp/copilot-spike-test-dir
```

Expected: Clean exit.

- [ ] **Step 8: Write RESULT.md**

Write `scratch/copilot-spike/rung4-gate/RESULT.md`:

```markdown
# Rung 4 Result

**Pass/Fail:** <pass | fail>

**Date/Time:** <ISO-8601 UTC>

## Evidence

### Scenario A — Allow path

- Prompt sent: `<exact>`
- Tool call observed: `<tool_name + args shape>`
- Action normalized to: `Action{Type: "shell.exec", Target: "<cmd>", Path: "<repo-root>"}`
- Gate command run: `chitin-kernel gate evaluate --tool=terminal --args-json='<j>' --agent=copilot-spike --cwd=<root>`
- Gate exit code: `0`
- Gate Decision (JSON, copied from stdout):

```json
<paste>
```

- Side effect observed: <yes — ls output captured | no>
- Decision log line (from `~/.chitin/gov-decisions-<today>.jsonl`):

```json
<paste>
```

### Scenario B — Block path

- Prompt sent: `<exact>`
- Tool call observed (rm -rf invocation): `<tool_name + args shape>`
- Action normalized to: `Action{Type: "shell.exec", Target: "rm -rf /tmp/copilot-spike-test-dir", Path: "<repo-root>"}`
- Gate exit code: `1`
- Gate Decision (JSON, copied from stdout):

```json
<paste>
```

- Refusal returned to SDK: <refusal signal used>
- Side effect observed: <no — canary survived | yes: describe>
- Canary file still present: <yes/no>
- Decision log line:

```json
<paste>
```

## Normalization shape used

- Copilot tool_name → chitin ActionType mapping applied in the interceptor:

```
"shell" / "terminal" / "bash" → "shell.exec"
```

- Arguments → Action.Target mapping:

```
<paste the exact conversion rule used — e.g., join argv with spaces,
use the "command" field as-is, etc.>
```

## Soft blockers observed

<list any decision-log or wiring issue that didn't force a fail but
would need to be fixed in the full v1 build; "none" if clean>

## Time taken

Start: <ISO-8601 UTC>
End:   <ISO-8601 UTC>
Wall:  <minutes>

## Surprises

<nonobvious issues, or "none">
```

- [ ] **Step 9: Commit Rung 4 artifacts**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add scratch/copilot-spike/rung4-gate/
rtk git commit -m "$(cat <<'EOF'
spike(rung4): gate + decision log end-to-end — <pass|fail>

<2-3 sentences: allow scenario outcome, block scenario outcome, decision log status>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: Commit lands. Proceed to Task 5.

---

## Task 5: Write findings report

**Files:**
- Create: `docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md`

**Context:** This is the deliverable the spike existed to produce. It must support the Y-vs-Z decision with audit-able evidence referencing Rungs 1-4. If the ladder aborted early, this report still gets written — with the verdict `no-go, blocked at Rung N` and a concrete X-based fallback narrative.

- [ ] **Step 1: Aggregate rung results**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
for dir in scratch/copilot-spike/rung*-*/; do
  echo "=== $dir ==="
  [ -f "$dir/RESULT.md" ] && cat "$dir/RESULT.md" || echo "(rung not run)"
  echo
done > /tmp/rung-aggregate.md

wc -l /tmp/rung-aggregate.md
```

Expected: Aggregate file has each rung's RESULT.md (or "(rung not run)" for aborted rungs).

- [ ] **Step 2: Write the findings report**

Write `docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md`:

```markdown
# Copilot SDK Feasibility Spike — Findings

**Date completed:** <ISO date>
**Time-box used:** <hours of the 48-hour window>
**Parent spec:** `docs/superpowers/specs/2026-04-23-copilot-sdk-spike-design.md`
**Parent plan:** `docs/superpowers/plans/2026-04-23-copilot-sdk-spike.md`
**Spike branch:** `spike/copilot-sdk-feasibility`

## Verdict

**<go (Option Y viable) | no-go, fall back to X via narrative Z>**

<ONE sentence rationale referencing the specific rung outcomes.>

## Rung-by-rung results

### Rung 1 — SDK install + Enterprise auth

**Status:** <cleared | failed | not attempted>

<Summary from rung1-auth/RESULT.md — evidence + surprises.>

### Rung 2 — Observe JSON-RPC stream

**Status:** <cleared | failed | not attempted>

<Summary from rung2-observe/RESULT.md — evidence + normalization feasibility.>

### Rung 3 — Intercept + synchronous block

**Status:** <cleared | failed | not attempted>

<Summary from rung3-intercept/RESULT.md — hook mechanism + refusal semantics.>

### Rung 4 — End-to-end gate + decision log

**Status:** <cleared | failed | not attempted>

<Summary from rung4-gate/RESULT.md — allow path + block path evidence.>

## Y build estimate (only if verdict = go)

12-day milestone plan from 2026-04-25 to 2026-05-07 (talk day):

- **Days 3-5 (Apr 25-27):**
  - <concrete tasks for integration + policy additions>
- **Days 6-10 (Apr 28 - May 2):**
  - <concrete tasks for end-to-end demo scenarios — terraform destroy, kubectl delete, force-push, env write, curl-pipe-bash>
- **Days 11-13 (May 3-5):**
  - <concrete tasks for rehearsal + polish>
- **Day 14 (May 6 evening / May 7):**
  - Final rehearsal, talk 19:00

**Risk areas:** <list anything that could push milestones — unknown SDK edge cases, gov package gaps for demo scenarios, latency on gate calls, etc.>

## Blockers observed

For each blocker, note whether it would also bite Option X (external-wrap fallback):

- <blocker>: affected rung: <N>; X-affected: <yes/no>; explanation: <…>

(If no blockers, write "none observed.")

## Recommendation

**<Y | Z>** — <ONE sentence rationale referencing specific rung outcomes, not general vibes.>

## Artifacts

- Spike branch: `spike/copilot-sdk-feasibility`
- Scratch directory: `scratch/copilot-spike/`
- Per-rung proof files:
  - `scratch/copilot-spike/rung1-auth/RESULT.md`
  - `scratch/copilot-spike/rung2-observe/RESULT.md` + `captured-stream.jsonl`
  - `scratch/copilot-spike/rung3-intercept/RESULT.md` + `block-proof.txt`
  - `scratch/copilot-spike/rung4-gate/RESULT.md` + `gate-run.log`

## Handoff

### If verdict is `go (Y)`

Next action: brainstorm the full Copilot CLI governance v1 spec (Y-based, SDK-embedded) targeting 2026-05-07. Start a fresh brainstorming session citing this findings report.

### If verdict is `no-go, fall back to X via Z`

Next action: brainstorm the Copilot CLI governance v1 spec (X-based, hermes-plugin port pattern) targeting 2026-05-07. The SDK path becomes the closing "where this is going" slide of the talk, not the live demo. Start a fresh brainstorming session citing this findings report.

### Either way

- This findings report is committed and PR'd into main.
- The v1 spec is a fresh brainstorming session, not a continuation — the decision gate between Y and Z is clean.
- Operator's parallel work (talk narrative, slide deck, demo-scenario list) continues regardless.
```

- [ ] **Step 3: Commit the findings report**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git add docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md
rtk git commit -m "$(cat <<'EOF'
spike: copilot-sdk feasibility findings — <verdict short>

<2-4 sentences: rung outcomes at the highest level, recommendation.>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: Findings report committed on the spike branch.

---

## Task 6: Open PR for spike branch

**Context:** The spike branch carries scaffold + rung probes + findings. PR it into main so the findings are reviewable by the operator and the go/no-go decision lives in git history.

- [ ] **Step 1: Push the spike branch**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
rtk git push -u origin spike/copilot-sdk-feasibility
```

Expected: Branch pushed to origin.

- [ ] **Step 2: Open the PR**

```bash
cd ~/workspace/chitin-spike-copilot-sdk
gh pr create --title "Spike: Copilot SDK feasibility — <verdict-short>" --body "$(cat <<'EOF'
## Summary

- Time-boxed (2-day) spike per docs/superpowers/specs/2026-04-23-copilot-sdk-spike-design.md
- 4-rung ladder probe: auth → observe → intercept → gate+log
- Findings committed at docs/superpowers/specs/2026-04-25-copilot-sdk-spike-findings.md

## Verdict

<paste the single-sentence verdict from the findings report>

## Rung results

- Rung 1 (auth): <cleared | failed | not attempted>
- Rung 2 (observe): <cleared | failed | not attempted>
- Rung 3 (intercept): <cleared | failed | not attempted>
- Rung 4 (gate+log): <cleared | failed | not attempted>

## Recommendation

<Y | Z> — see findings report for rationale.

## Test plan

- [ ] Operator reviews findings report
- [ ] Operator confirms recommendation or redirects
- [ ] Next action: brainstorm the v1 spec (Y-based or X-based per recommendation)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR opens with clear verdict + rung summary for operator review.

- [ ] **Step 3: Report the PR URL**

Print the PR URL returned by `gh pr create` so the operator has the link.

Expected: Operator receives the PR URL and can review the findings in context.

---

## Self-review

### Spec coverage

Walked each requirement from the parent spec and mapped to a task:

- SDK install + auth (Rung 1 in spec §Ladder) → Task 1 ✓
- Copilot CLI via SDK (Rung 2 in spec §Ladder) → Task 2 ✓
- JSON-RPC stream observation (Rung 2 in spec §Ladder) → Task 2 ✓
- Pre-execution intercept (Rung 3 in spec §Ladder) → Task 3 ✓
- chitin-kernel gate integration (Rung 4 in spec §Ladder) → Task 4 ✓
- Decision log write verification (Rung 4 in spec §Ladder) → Task 4 ✓
- Findings report with 6 sections (spec §Deliverable) → Task 5 ✓
- Scratch directory under scratch/copilot-spike/ (spec §Execution) → Task 0 ✓
- Spike branch off main (spec §Execution) → Task 0 ✓
- Worktree at ~/workspace/chitin-spike-copilot-sdk/ (spec §Execution) → Task 0 ✓
- Early-exit on first failed rung (spec §Ladder semantics) → Each rung task has a KILL CHECK + "abort to Task 5" instruction ✓
- Per-rung kill conditions (spec §Kill conditions) → Each rung task lists its kill conditions in Context ✓
- Go/no-go verdict in findings (spec §Deliverable #1) → Task 5 Step 2 ✓
- Rung-by-rung results with evidence (spec §Deliverable #2) → Task 5 Step 2 ✓
- Y build estimate conditional (spec §Deliverable #3) → Task 5 Step 2 ✓
- Blockers (including X-affecting ones) (spec §Deliverable #4) → Task 5 Step 2 ✓
- Recommendation (spec §Deliverable #5) → Task 5 Step 2 ✓
- Artifacts paths (spec §Deliverable #6) → Task 5 Step 2 ✓

No spec gaps.

### Placeholder scan

No `TBD` / `TODO` / `later` / `add appropriate X` / `similar to Task N` patterns. The `<exact-SDK-import-path-from-step-1>` style markers are deliberate — the exact import path is what Rung 1 discovers; once it's known, the subagent substitutes it in Rungs 2/3/4. That's directed-but-learned, not a placeholder.

The `<paste evidence>` style fills in RESULT.md and the findings report are report template markers — the subagent fills them from actual run outputs. Explicit and appropriate for a report being written against real evidence.

### Type consistency

- `Action` / `ActionType` / `Decision` — consistent across Task 4 Step 5 (normalization) and Task 5 (findings template); matches chitin gov package (PR #45).
- `chitin-kernel gate evaluate` signature — consistent across Tasks 4 Steps 1/3/5 (same flags: `--tool`, `--args-json`, `--agent`, `--cwd`).
- `~/.chitin/gov-decisions-<today>.jsonl` path — consistent across Task 4 and Task 5.
- Rung numbering (1/2/3/4) — consistent across spec, task titles, and findings template.
- Spike branch name (`spike/copilot-sdk-feasibility`) — consistent across Task 0, Task 6, and findings.

No inconsistencies.

### Scope check

Single plan, single ladder, single findings deliverable. Task 6 (PR open) is standard workflow polish, not a distinct subsystem. Plan does not leak into the follow-up v1 spec work (correctly deferred to a fresh brainstorming session after findings land).
