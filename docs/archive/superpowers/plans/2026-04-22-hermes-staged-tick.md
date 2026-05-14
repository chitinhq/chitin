# Hermes Staged Tick v1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `scripts/hermes/tick.sh` — a shell orchestrator that runs three isolated hermes invocations per cron tick with each stage's model hardcoded (glm-5.1 plans, qwen3-coder on the local 3090 generates diffs iff plan requires code, glm-5.1 applies) — plus the three prompt files, a JSON schema that contracts Stage 1's output, bats tests for the orchestration logic, and a CI job that validates the schema against fixtures.

**Architecture:** Pure shell + markdown + JSON Schema. No Go code. `tick.sh` is the cron entrypoint; it fetches the queue, invokes the three hermes processes in sequence (gating Stage 2 on `action=="code"` and local-ollama reachability), writes all artifacts to `~/chitin-sink/ticks/<date>/<ts>/`, and always exits 0 except on shell crash. Tests use `bats` with stub commands on `PATH` to assert stage sequencing; fixtures drive schema-validation CI.

**Tech Stack:** bash 5+, `jq`, `curl`, `gh`, `git`, `bats-core` (tests), JSON Schema Draft 2020-12 (validated via `ajv` in CI). Prompts are markdown files consumed by `hermes chat --system <file>`.

**Spec:** `docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md`
**Supersedes spec:** `docs/superpowers/specs/2026-04-21-hermes-autonomy-v1-design.md` (abandoned)
**Relies on:** Chitin governance v1 (PR #45, merged `5cc74bbe8a`) — the `pre_tool_call` hook is the enforcement layer Stage 3 hits.

**Commit / identity reminders:**
- Git identity: `jpleva91@gmail.com` (chitin OSS — do NOT use readybench.io).
- No content from Readybench / bench-devs. Chitin is OSS.
- PR flow: non-draft PR → Copilot review → adversarial review (`/review`) → fix findings → merge on green.
- Always work in this worktree (`/home/red/workspace/chitin-staged-tick`), not the primary checkout.

---

## File Structure

Files this plan creates or modifies.

### Chitin repo — new under `scripts/hermes/`

- **Create:** `scripts/hermes/tick.sh` — cron entrypoint; bash orchestrator (~200 lines)
- **Create:** `scripts/hermes/prompt-plan.md` — Stage 1 system prompt (glm-5.1)
- **Create:** `scripts/hermes/prompt-code.md` — Stage 2 system prompt (qwen3-coder:30b)
- **Create:** `scripts/hermes/prompt-act.md` — Stage 3 system prompt (glm-5.1)
- **Create:** `scripts/hermes/plan-schema.json` — JSON Schema Draft 2020-12 for plan.json
- **Create:** `scripts/hermes/README.md` — operator handbook
- **Create:** `scripts/hermes/MIGRATION.md` — user-run steps to retire v1 and enable v1-staged

### Chitin repo — new under `scripts/hermes/tests/`

- **Create:** `scripts/hermes/tests/tick.bats` — orchestration tests (6 scenarios)
- **Create:** `scripts/hermes/tests/validate-plans.sh` — fixture validator wrapper
- **Create:** `scripts/hermes/tests/fixtures/plan-valid-skip.json` — minimal skip plan
- **Create:** `scripts/hermes/tests/fixtures/plan-valid-code.json` — code plan with diff_request
- **Create:** `scripts/hermes/tests/fixtures/plan-valid-external-comment.json` — external comment
- **Create:** `scripts/hermes/tests/fixtures/plan-valid-external-label.json` — grooming label-apply
- **Create:** `scripts/hermes/tests/fixtures/plan-valid-external-pr.json` — external pr_open
- **Create:** `scripts/hermes/tests/fixtures/plan-invalid-missing-action.json`
- **Create:** `scripts/hermes/tests/fixtures/plan-invalid-wrong-action-enum.json`
- **Create:** `scripts/hermes/tests/fixtures/plan-invalid-code-without-diff-request.json`
- **Create:** `scripts/hermes/tests/fixtures/plan-invalid-external-without-external-action.json`
- **Create:** `scripts/hermes/tests/fixtures/plan-invalid-issue-number-as-string.json`

### Chitin repo — new under `.github/workflows/`

- **Create:** `.github/workflows/hermes-plan-schema.yml` — validates fixtures against schema on PR / push

### Not modified in this plan (user runs post-merge, guided by MIGRATION.md)

- `~/.hermes/scripts/autonomous-worker-orders.txt` (tombstone to `~/.hermes/scripts/retired-<date>/`)
- `~/.hermes/scripts/autonomous-worker-context.py` (tombstone)
- Hermes cron registration (rm old, add new pointing at the repo'd tick.sh)

---

## Task 0: Verify worktree + dependencies

**Files:** None. Environment check only.

- [ ] **Step 0.1: Confirm worktree and branch**

Run: `git rev-parse --abbrev-ref HEAD && pwd -P`

Expected:
```
spec/hermes-staged-tick-v1
/home/red/workspace/chitin-staged-tick
```

If not, `cd /home/red/workspace/chitin-staged-tick` before proceeding. All task paths in this plan are relative to that worktree root.

- [ ] **Step 0.2: Confirm required binaries are present**

Run:
```bash
for b in bash jq curl gh git bats; do
  command -v "$b" >/dev/null && echo "$b: $(command -v "$b")" || echo "$b: MISSING"
done
```

Expected: all six lines show a path, none say MISSING. If `bats` is missing, install via `brew install bats-core` (macOS) or `apt-get install -y bats` (Debian). If `ajv` is desired locally (it's only strictly required in CI), `npm install -g ajv-cli`.

- [ ] **Step 0.3: Confirm local ollama is reachable**

Run: `curl -sf --max-time 2 http://127.0.0.1:11434/api/tags | jq -r '.models | length'`

Expected: a non-negative integer (count of installed models).

If curl fails, start `ollama serve` in another terminal before running later canary/dry-run tasks; the implementation work itself does not require ollama.

- [ ] **Step 0.4: Create the `scripts/hermes/` directory tree**

Run:
```bash
mkdir -p scripts/hermes/tests/fixtures
```

Expected: no error. Verify with `ls scripts/hermes/tests/fixtures` (should print nothing, directory is empty).

---

## Task 1: plan-schema.json + fixtures (TDD)

**Files:**
- Create: `scripts/hermes/plan-schema.json`
- Create: `scripts/hermes/tests/fixtures/plan-valid-skip.json`
- Create: `scripts/hermes/tests/fixtures/plan-valid-code.json`
- Create: `scripts/hermes/tests/fixtures/plan-valid-external-comment.json`
- Create: `scripts/hermes/tests/fixtures/plan-valid-external-label.json`
- Create: `scripts/hermes/tests/fixtures/plan-valid-external-pr.json`
- Create: `scripts/hermes/tests/fixtures/plan-invalid-missing-action.json`
- Create: `scripts/hermes/tests/fixtures/plan-invalid-wrong-action-enum.json`
- Create: `scripts/hermes/tests/fixtures/plan-invalid-code-without-diff-request.json`
- Create: `scripts/hermes/tests/fixtures/plan-invalid-external-without-external-action.json`
- Create: `scripts/hermes/tests/fixtures/plan-invalid-issue-number-as-string.json`
- Create: `scripts/hermes/tests/validate-plans.sh`

- [ ] **Step 1.1: Write the 5 valid fixtures**

`scripts/hermes/tests/fixtures/plan-valid-skip.json`:
```json
{
  "action": "skip",
  "issue_number": 0,
  "reason": "no viable targets in queue"
}
```

`scripts/hermes/tests/fixtures/plan-valid-code.json`:
```json
{
  "action": "code",
  "issue_number": 10,
  "reason": "add .js extension for ESM import in jsonl-tailer.ts",
  "diff_request": {
    "files": ["apps/cli/src/telemetry/jsonl-tailer.ts"],
    "intent": "append .js to the relative import of ./event-parser per ESM requirements"
  }
}
```

`scripts/hermes/tests/fixtures/plan-valid-external-comment.json`:
```json
{
  "action": "external",
  "issue_number": 22,
  "reason": "request clarification on chain-id behavior",
  "external_action": {
    "kind": "comment",
    "body_or_label": "Should PreCompact emit one event per /compact invocation regardless of preceding state?",
    "linked_issue": 22
  }
}
```

`scripts/hermes/tests/fixtures/plan-valid-external-label.json`:
```json
{
  "action": "external",
  "issue_number": 10,
  "reason": "promote small, clear-scope issue to the autonomous queue",
  "external_action": {
    "kind": "label",
    "body_or_label": "hermes-autonomous",
    "linked_issue": 10
  }
}
```

`scripts/hermes/tests/fixtures/plan-valid-external-pr.json`:
```json
{
  "action": "external",
  "issue_number": 10,
  "reason": "open PR for previously-implemented diff",
  "external_action": {
    "kind": "pr_open",
    "body_or_label": "fix: add .js extension for ESM import\n\nCloses #10",
    "linked_issue": 10
  }
}
```

- [ ] **Step 1.2: Write the 5 invalid fixtures**

`scripts/hermes/tests/fixtures/plan-invalid-missing-action.json`:
```json
{
  "issue_number": 10,
  "reason": "action field omitted"
}
```

`scripts/hermes/tests/fixtures/plan-invalid-wrong-action-enum.json`:
```json
{
  "action": "merge",
  "issue_number": 10,
  "reason": "merge is not a permitted action"
}
```

`scripts/hermes/tests/fixtures/plan-invalid-code-without-diff-request.json`:
```json
{
  "action": "code",
  "issue_number": 10,
  "reason": "code action must include diff_request"
}
```

`scripts/hermes/tests/fixtures/plan-invalid-external-without-external-action.json`:
```json
{
  "action": "external",
  "issue_number": 10,
  "reason": "external action must include external_action"
}
```

`scripts/hermes/tests/fixtures/plan-invalid-issue-number-as-string.json`:
```json
{
  "action": "skip",
  "issue_number": "10",
  "reason": "issue_number must be integer, not string"
}
```

- [ ] **Step 1.3: Write `validate-plans.sh`**

`scripts/hermes/tests/validate-plans.sh`:
```bash
#!/usr/bin/env bash
# Validates every fixture file against plan-schema.json.
# Files named plan-valid-*.json must validate; plan-invalid-*.json must fail.
# Exits 0 iff all fixtures behave as expected.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMA="$SCRIPT_DIR/../plan-schema.json"
FIXTURES_DIR="$SCRIPT_DIR/fixtures"

if ! command -v ajv >/dev/null; then
  echo "SKIP: ajv not installed (install via: npm i -g ajv-cli)"
  exit 0
fi

failures=0
for f in "$FIXTURES_DIR"/plan-*.json; do
  name="$(basename "$f")"
  if ajv validate -s "$SCHEMA" -d "$f" --spec=draft2020 >/dev/null 2>&1; then
    outcome=valid
  else
    outcome=invalid
  fi
  case "$name" in
    plan-valid-*)
      if [[ "$outcome" != valid ]]; then
        echo "FAIL: $name should be valid but schema rejected it"
        failures=$((failures+1))
      else
        echo "OK:   $name -> valid (expected)"
      fi
      ;;
    plan-invalid-*)
      if [[ "$outcome" != invalid ]]; then
        echo "FAIL: $name should be invalid but schema accepted it"
        failures=$((failures+1))
      else
        echo "OK:   $name -> invalid (expected)"
      fi
      ;;
  esac
done

if [[ $failures -gt 0 ]]; then
  echo ""
  echo "$failures fixture(s) failed"
  exit 1
fi
```

Make executable: `chmod +x scripts/hermes/tests/validate-plans.sh`.

- [ ] **Step 1.4: Run the validator to confirm all fixtures fail without a schema**

Run: `scripts/hermes/tests/validate-plans.sh` (after creating empty `plan-schema.json: {}` first, since an absent schema path breaks ajv).

Create empty schema placeholder:
```bash
echo '{}' > scripts/hermes/plan-schema.json
```

Expected: all 5 `plan-valid-*` pass (empty schema accepts anything), all 5 `plan-invalid-*` FAIL (because empty schema accepts them too). Exit code 1.

Why this step: it confirms the validator wrapper itself is wired correctly before we write the real schema.

- [ ] **Step 1.5: Write `plan-schema.json`**

`scripts/hermes/plan-schema.json`:
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://chitinhq/chitin/scripts/hermes/plan-schema.json",
  "title": "Hermes staged-tick plan",
  "type": "object",
  "required": ["action", "issue_number", "reason"],
  "additionalProperties": false,
  "properties": {
    "action": {
      "type": "string",
      "enum": ["skip", "code", "external"]
    },
    "issue_number": {
      "type": "integer",
      "minimum": 0
    },
    "reason": {
      "type": "string",
      "minLength": 1,
      "maxLength": 500
    },
    "diff_request": {
      "type": "object",
      "required": ["files", "intent"],
      "additionalProperties": false,
      "properties": {
        "files": {
          "type": "array",
          "items": { "type": "string", "minLength": 1 },
          "minItems": 1,
          "maxItems": 20
        },
        "intent": {
          "type": "string",
          "minLength": 1,
          "maxLength": 2000
        }
      }
    },
    "external_action": {
      "type": "object",
      "required": ["kind", "body_or_label", "linked_issue"],
      "additionalProperties": false,
      "properties": {
        "kind": {
          "type": "string",
          "enum": ["comment", "label", "pr_open"]
        },
        "body_or_label": {
          "type": "string",
          "minLength": 1,
          "maxLength": 4000
        },
        "linked_issue": {
          "type": "integer",
          "minimum": 1
        }
      }
    }
  },
  "allOf": [
    {
      "if":  { "properties": { "action": { "const": "code" } } },
      "then": { "required": ["diff_request"] }
    },
    {
      "if":  { "properties": { "action": { "const": "external" } } },
      "then": { "required": ["external_action"] }
    }
  ]
}
```

- [ ] **Step 1.6: Re-run the validator — all fixtures should pass their expected outcome**

Run: `scripts/hermes/tests/validate-plans.sh`

Expected: all 10 lines print `OK:`, exit code 0.

If `ajv` is not installed locally, the script prints `SKIP:` and exits 0 — that's accepted for local dev (CI will run ajv). To exercise locally: `npm i -g ajv-cli` and re-run.

- [ ] **Step 1.7: Commit**

```bash
git add scripts/hermes/plan-schema.json scripts/hermes/tests/validate-plans.sh scripts/hermes/tests/fixtures/
git commit -m "hermes-tick: plan-schema.json + 10 fixtures + validator"
```

---

## Task 2: bats harness + first failing test (skip path)

**Files:**
- Create: `scripts/hermes/tests/tick.bats`
- Create: `scripts/hermes/tests/stubs/` (empty dir for stub binaries)

- [ ] **Step 2.1: Write `tick.bats` with a single test for the skip path**

`scripts/hermes/tests/tick.bats`:
```bash
#!/usr/bin/env bats
# Orchestration tests for scripts/hermes/tick.sh.
# Strategy: prepend scripts/hermes/tests/stubs/ to PATH so all external
# commands (hermes, gh, curl, git, jq) are replaced with shell scripts
# that write deterministic output and log calls to $STUB_LOG.

setup() {
  export TEST_TMPDIR="$(mktemp -d)"
  export STUB_LOG="$TEST_TMPDIR/stub-calls.log"
  export CHITIN_SINK_ROOT="$TEST_TMPDIR/chitin-sink"
  export HERMES_TICK_TS="20260422T000000Z"         # deterministic tick dir name
  export HERMES_TICK_DATE="2026-04-22"
  mkdir -p "$CHITIN_SINK_ROOT/ticks"
  : > "$STUB_LOG"

  STUBS="$BATS_TEST_DIRNAME/stubs"
  export PATH="$STUBS:$PATH"

  # Defaults; individual tests can override by exporting STUB_* before run
  export STUB_HERMES_PLAN_OUTPUT='{"action":"skip","issue_number":0,"reason":"no viable targets"}'
  export STUB_CURL_OLLAMA_OK=1
  export STUB_GH_ISSUE_LIST_OUTPUT='[]'
  export STUB_GH_PR_LIST_OUTPUT='[]'
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

@test "skip path: Stage 1 runs, emits skip plan, Stages 2 and 3 do not run" {
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/queue.json" ]
  [ ! -f "$tick_dir/diff.patch" ]
  [ ! -f "$tick_dir/act-log.txt" ]

  # Stage 1 invoked exactly once
  grep -c '^hermes chat --model glm-5.1:cloud' "$STUB_LOG" | grep -qx 1

  # Stage 2 (qwen) never invoked
  ! grep -q 'qwen3-coder' "$STUB_LOG"

  # Stage 3 (prompt-act) never invoked
  ! grep -q 'prompt-act.md' "$STUB_LOG"
}
```

- [ ] **Step 2.2: Write the six stubs the tests rely on**

Make stubs directory: `mkdir -p scripts/hermes/tests/stubs`.

`scripts/hermes/tests/stubs/hermes`:
```bash
#!/usr/bin/env bash
# Logs the invocation, emits a plan per the test's STUB_HERMES_PLAN_OUTPUT
# (for Stage 1), a diff for Stage 2, or a short act-log for Stage 3.
set -euo pipefail
echo "hermes $*" >> "${STUB_LOG:?STUB_LOG unset}"
# Detect stage by inspecting --system flag value.
for i in "$@"; do
  case "$i" in
    *prompt-plan.md) echo "${STUB_HERMES_PLAN_OUTPUT:-{}}"; exit 0 ;;
    *prompt-code.md) echo "${STUB_HERMES_CODE_OUTPUT:-}"; exit 0 ;;
    *prompt-act.md)  echo "${STUB_HERMES_ACT_OUTPUT:-ok}"; exit 0 ;;
  esac
done
exit 0
```

`scripts/hermes/tests/stubs/curl`:
```bash
#!/usr/bin/env bash
# Only used by tick.sh to probe ollama at 127.0.0.1:11434.
# Emits a successful or failing HTTP based on STUB_CURL_OLLAMA_OK.
set -euo pipefail
echo "curl $*" >> "${STUB_LOG:?}"
if [[ "${STUB_CURL_OLLAMA_OK:-1}" -eq 1 ]]; then
  echo '{"models":[{"name":"qwen3-coder:30b"}]}'
  exit 0
fi
exit 7   # curl "failed to connect"
```

`scripts/hermes/tests/stubs/gh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
echo "gh $*" >> "${STUB_LOG:?}"
case "$*" in
  *"issue list"*) echo "${STUB_GH_ISSUE_LIST_OUTPUT:-[]}" ;;
  *"pr list"*)    echo "${STUB_GH_PR_LIST_OUTPUT:-[]}" ;;
  *)              echo "" ;;
esac
```

`scripts/hermes/tests/stubs/git`:
```bash
#!/usr/bin/env bash
set -euo pipefail
echo "git $*" >> "${STUB_LOG:?}"
# Pass-through mode for sub-commands tick.sh cares about (apply --check
# must succeed for happy paths; commit/push are no-ops).
case "$*" in
  "apply --check"*) exit 0 ;;
  *)                exit 0 ;;
esac
```

`scripts/hermes/tests/stubs/jq`:
```bash
#!/usr/bin/env bash
# Real jq — the schema-validation path relies on its actual behavior.
exec /usr/bin/jq "$@"
```

(If the real jq lives at a different path on the target machine, adjust or use `exec "$(command -v jq)"`. Using an absolute path in stubs keeps the $PATH shim from recursing.)

Make all stubs executable:
```bash
chmod +x scripts/hermes/tests/stubs/*
```

- [ ] **Step 2.3: Run bats — expect fail (tick.sh does not exist yet)**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: test fails with something like `tick.sh: command not found` or `No such file or directory`.

- [ ] **Step 2.4: Commit**

```bash
git add scripts/hermes/tests/tick.bats scripts/hermes/tests/stubs/
git commit -m "hermes-tick: bats harness + stubs + first failing orchestration test"
```

---

## Task 3: `tick.sh` skeleton — skip path only

**Files:** Create `scripts/hermes/tick.sh`

- [ ] **Step 3.1: Write the initial tick.sh**

`scripts/hermes/tick.sh`:
```bash
#!/usr/bin/env bash
# Hermes staged-tick cron entrypoint.
# Spec: docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md
#
# Three isolated stages: PLAN (glm-5.1) → CODE (qwen3-coder, iff
# action=="code" and local ollama reachable) → ACT (glm-5.1).
# Each stage's model is hardcoded; no same-session delegation.
# Artifacts persist at $CHITIN_SINK_ROOT/ticks/<date>/<ts>/.
# Always exits 0 except on shell crash — stage failures are data.

set -euo pipefail

# ---- Config (env-overridable for tests) -----------------------------------
CHITIN_SINK_ROOT="${CHITIN_SINK_ROOT:-$HOME/chitin-sink}"
REPO_ROOT="${HERMES_TICK_REPO_ROOT:-$HOME/workspace/chitin}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMPT_PLAN="$SCRIPT_DIR/prompt-plan.md"
PROMPT_CODE="$SCRIPT_DIR/prompt-code.md"
PROMPT_ACT="$SCRIPT_DIR/prompt-act.md"
SCHEMA="$SCRIPT_DIR/plan-schema.json"
MODEL_PLAN="${HERMES_TICK_MODEL_PLAN:-glm-5.1:cloud}"
MODEL_CODE="${HERMES_TICK_MODEL_CODE:-qwen3-coder:30b}"
MODEL_ACT="${HERMES_TICK_MODEL_ACT:-glm-5.1:cloud}"

# Deterministic timestamps for tests; real runs compute fresh.
ts="${HERMES_TICK_TS:-$(date -u +%Y%m%dT%H%M%SZ)}"
date_str="${HERMES_TICK_DATE:-$(date -u +%Y-%m-%d)}"

TICK_DIR="$CHITIN_SINK_ROOT/ticks/$date_str/$ts"
mkdir -p "$TICK_DIR"

log() { echo "[$(date -u +%H:%M:%SZ)] $*" >> "$TICK_DIR/tick.log"; }
log "tick starting; dir=$TICK_DIR"

# Capture env snapshot (scrubbed).
env | grep -E '^(CHITIN|HERMES|OLLAMA|PATH)=' > "$TICK_DIR/env.txt" || true

# ---- Queue fetch ----------------------------------------------------------
queue_labeled="$(gh issue list --repo chitinhq/chitin --label hermes-autonomous --state open --json number,title,body 2>/dev/null || echo '[]')"
queue_unlabeled="$(gh issue list --repo chitinhq/chitin --search 'no:label is:open' --json number,title,body 2>/dev/null || echo '[]')"
pr_inflight="$(gh pr list --repo chitinhq/chitin --search 'is:open linked:issue' --json number,title 2>/dev/null || echo '[]')"
jq -n \
  --argjson labeled "$queue_labeled" \
  --argjson unlabeled "$queue_unlabeled" \
  --argjson prs "$pr_inflight" \
  '{labeled: $labeled, unlabeled: $unlabeled, in_flight_prs: $prs}' \
  > "$TICK_DIR/queue.json"
log "queue captured"

# ---- STAGE 1: PLAN (glm-5.1) ----------------------------------------------
log "stage 1 (plan) starting"
if ! hermes chat --model "$MODEL_PLAN" --system "$PROMPT_PLAN" \
       --context "$(cat "$TICK_DIR/queue.json")" \
       > "$TICK_DIR/plan.json" 2> "$TICK_DIR/plan-stderr.txt"; then
  log "stage 1 failed (hermes non-zero)"
  exit 0
fi

if ! jq -e 'type == "object" and has("action")' "$TICK_DIR/plan.json" >/dev/null 2>&1; then
  log "plan_parse_error: stage 1 output is not a plan object"
  exit 0
fi

action="$(jq -r '.action' "$TICK_DIR/plan.json")"
log "stage 1 done; action=$action"

case "$action" in
  skip)
    log "skip: exit without invoking stages 2 or 3"
    exit 0
    ;;
  code|external)
    log "TODO: stages 2/3 not wired yet"
    exit 0
    ;;
  *)
    log "plan_schema_violation: unknown action=$action"
    exit 0
    ;;
esac
```

Make executable: `chmod +x scripts/hermes/tick.sh`.

- [ ] **Step 3.2: Run bats to confirm skip-path test passes**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `1 test, 0 failures`.

- [ ] **Step 3.3: Commit**

```bash
git add scripts/hermes/tick.sh
git commit -m "hermes-tick: tick.sh skeleton — queue fetch + Stage 1 + skip path"
```

---

## Task 4: bats test — external path (TDD)

**Files:** Modify `scripts/hermes/tests/tick.bats`. Create empty `scripts/hermes/prompt-act.md` and `scripts/hermes/prompt-plan.md` (content fills in Tasks 11–13).

- [ ] **Step 4.1: Create empty prompt files so `--system "$PROMPT_*"` doesn't error**

```bash
echo "(v1 placeholder — real prompt added in Task 11)" > scripts/hermes/prompt-plan.md
echo "(v1 placeholder — real prompt added in Task 13)" > scripts/hermes/prompt-act.md
```

- [ ] **Step 4.2: Append the external-path test to tick.bats**

Add at the end of `scripts/hermes/tests/tick.bats`:
```bash
@test "external path: Stage 1 + Stage 3 run, Stage 2 does not" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"external","issue_number":10,"reason":"groom","external_action":{"kind":"label","body_or_label":"hermes-autonomous","linked_issue":10}}'

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/act-log.txt" ]
  [ ! -f "$tick_dir/diff.patch" ]

  grep -q 'prompt-plan.md' "$STUB_LOG"
  grep -q 'prompt-act.md'  "$STUB_LOG"
  ! grep -q 'prompt-code.md' "$STUB_LOG"
  ! grep -q 'qwen3-coder'     "$STUB_LOG"
}
```

- [ ] **Step 4.3: Run bats — expect the new test to fail**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `2 tests, 1 failure` — the new external-path test fails because tick.sh's `code|external)` branch currently just logs TODO.

- [ ] **Step 4.4: Commit**

```bash
git add scripts/hermes/tests/tick.bats scripts/hermes/prompt-plan.md scripts/hermes/prompt-act.md
git commit -m "hermes-tick: failing bats test for external path"
```

---

## Task 5: `tick.sh` — wire Stage 3 (external path)

**Files:** Modify `scripts/hermes/tick.sh`.

- [ ] **Step 5.1: Replace the `code|external)` branch with Stage 3 invocation**

In `scripts/hermes/tick.sh`, replace:
```bash
  code|external)
    log "TODO: stages 2/3 not wired yet"
    exit 0
    ;;
```

with:
```bash
  code)
    log "TODO: stage 2 + 3 not wired yet for code"
    exit 0
    ;;
  external)
    run_stage_act
    exit 0
    ;;
```

And add the `run_stage_act` function before the queue fetch block (right after the `log()` helper):

```bash
run_stage_act() {
  log "stage 3 (act) starting"
  local plan_body diff_body
  plan_body="$(cat "$TICK_DIR/plan.json")"
  diff_body=""
  [[ -f "$TICK_DIR/diff.patch" ]] && diff_body="$(cat "$TICK_DIR/diff.patch")"

  if ! hermes chat --model "$MODEL_ACT" --system "$PROMPT_ACT" \
         --context "plan=$plan_body diff=$diff_body" \
         > "$TICK_DIR/act-log.txt" 2> "$TICK_DIR/act-stderr.txt"; then
    log "stage 3 failed (hermes non-zero)"
    return 0
  fi
  log "stage 3 done"
}
```

- [ ] **Step 5.2: Run bats — expect both tests to pass**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `2 tests, 0 failures`.

- [ ] **Step 5.3: Commit**

```bash
git add scripts/hermes/tick.sh
git commit -m "hermes-tick: wire Stage 3 (act) for external-action plans"
```

---

## Task 6: bats tests — code path + ollama probe (TDD)

**Files:** Modify `scripts/hermes/tests/tick.bats`. Create empty `scripts/hermes/prompt-code.md`.

- [ ] **Step 6.1: Create empty prompt-code placeholder**

```bash
echo "(v1 placeholder — real prompt added in Task 12)" > scripts/hermes/prompt-code.md
```

- [ ] **Step 6.2: Append two tests for code path**

Add at the end of `scripts/hermes/tests/tick.bats`:
```bash
@test "code path + ollama ok: all three stages run; diff.patch written" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix ESM import","diff_request":{"files":["apps/cli/src/telemetry/jsonl-tailer.ts"],"intent":"append .js extension"}}'
  export STUB_HERMES_CODE_OUTPUT='--- a/apps/cli/src/telemetry/jsonl-tailer.ts
+++ b/apps/cli/src/telemetry/jsonl-tailer.ts
@@ -1 +1 @@
-import { foo } from "./event-parser";
+import { foo } from "./event-parser.js";'
  export STUB_CURL_OLLAMA_OK=1

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ -f "$tick_dir/diff.patch" ]
  [ -f "$tick_dir/act-log.txt" ]
  [ "$(cat "$tick_dir/ollama-probe.txt")" = "ok" ]

  grep -q 'prompt-plan.md' "$STUB_LOG"
  grep -q 'prompt-code.md' "$STUB_LOG"
  grep -q 'prompt-act.md'  "$STUB_LOG"
  grep -q 'qwen3-coder'    "$STUB_LOG"
}

@test "code path + ollama unreachable: Stage 1 runs; Stages 2 & 3 skipped" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix ESM import","diff_request":{"files":["apps/cli/src/telemetry/jsonl-tailer.ts"],"intent":"append .js extension"}}'
  export STUB_CURL_OLLAMA_OK=0

  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/plan.json" ]
  [ ! -f "$tick_dir/diff.patch" ]
  [ ! -f "$tick_dir/act-log.txt" ]
  [ "$(cat "$tick_dir/ollama-probe.txt")" = "unreachable" ]

  ! grep -q 'qwen3-coder' "$STUB_LOG"
  ! grep -q 'prompt-act.md' "$STUB_LOG"
}
```

- [ ] **Step 6.3: Run bats — expect the two new tests to fail**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `4 tests, 2 failures` — the code-path tests fail because tick.sh's `code)` branch still logs TODO.

- [ ] **Step 6.4: Commit**

```bash
git add scripts/hermes/tests/tick.bats scripts/hermes/prompt-code.md
git commit -m "hermes-tick: failing bats tests for code path + ollama probe"
```

---

## Task 7: `tick.sh` — wire ollama probe + Stage 2 + happy code path

**Files:** Modify `scripts/hermes/tick.sh`.

- [ ] **Step 7.1: Add `probe_ollama` and `run_stage_code` helpers**

Insert after the `log()` helper (or before `run_stage_act`):

```bash
probe_ollama() {
  if curl -sf --max-time 2 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    echo "ok" > "$TICK_DIR/ollama-probe.txt"
    return 0
  fi
  echo "unreachable" > "$TICK_DIR/ollama-probe.txt"
  return 1
}

run_stage_code() {
  log "stage 2 (code) starting"
  local plan_body file_dump files
  plan_body="$(cat "$TICK_DIR/plan.json")"
  # Read the files listed in diff_request.files (best-effort — missing
  # files become empty context strings).
  files="$(jq -r '.diff_request.files[]?' "$TICK_DIR/plan.json")"
  file_dump=""
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    if [[ -f "$REPO_ROOT/$f" ]]; then
      file_dump+=$'\n--- FILE: '"$f"$' ---\n'
      file_dump+="$(cat "$REPO_ROOT/$f")"
    fi
  done <<< "$files"

  if ! hermes chat --model "$MODEL_CODE" --system "$PROMPT_CODE" \
         --context "plan=$plan_body files=$file_dump" \
         > "$TICK_DIR/diff.patch" 2> "$TICK_DIR/code-stderr.txt"; then
    log "stage 2 failed (hermes non-zero)"
    return 1
  fi

  if [[ ! -s "$TICK_DIR/diff.patch" ]]; then
    log "code_empty_output: stage 2 emitted empty diff"
    return 1
  fi
  log "stage 2 done"
}
```

- [ ] **Step 7.2: Replace the `code)` branch with the wired-up flow**

Replace:
```bash
  code)
    log "TODO: stage 2 + 3 not wired yet for code"
    exit 0
    ;;
```

with:
```bash
  code)
    if ! probe_ollama; then
      log "ollama_unreachable: skip stages 2 and 3"
      exit 0
    fi
    if ! run_stage_code; then
      exit 0
    fi
    run_stage_act
    exit 0
    ;;
```

- [ ] **Step 7.3: Run bats — expect all 4 tests to pass**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `4 tests, 0 failures`.

- [ ] **Step 7.4: Commit**

```bash
git add scripts/hermes/tick.sh
git commit -m "hermes-tick: wire ollama probe + Stage 2 (code) for code-action plans"
```

---

## Task 8: bats tests — 3-tick unreachable streak (TDD)

**Files:** Modify `scripts/hermes/tests/tick.bats`.

- [ ] **Step 8.1: Append streak-counter tests**

Add at the end of `scripts/hermes/tests/tick.bats`:
```bash
@test "streak: counter increments on each unreachable, resets on reachable" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"code","issue_number":10,"reason":"fix","diff_request":{"files":["x.ts"],"intent":"fix"}}'
  streak_file="$CHITIN_SINK_ROOT/ollama-unreachable-streak.txt"

  # Run 1 — unreachable
  export STUB_CURL_OLLAMA_OK=0
  export HERMES_TICK_TS="20260422T000000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$status" -eq 0 ]
  [ "$(cat "$streak_file")" = "1" ]

  # Run 2 — unreachable
  export HERMES_TICK_TS="20260422T001000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "2" ]

  # Run 3 — unreachable
  export HERMES_TICK_TS="20260422T002000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "3" ]

  # Run 4 — reachable → reset
  export STUB_CURL_OLLAMA_OK=1
  export HERMES_TICK_TS="20260422T003000Z"
  run "$BATS_TEST_DIRNAME/../tick.sh"
  [ "$(cat "$streak_file")" = "0" ]
}
```

- [ ] **Step 8.2: Run bats — expect new test to fail**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `5 tests, 1 failure` — streak counter doesn't exist yet.

- [ ] **Step 8.3: Commit**

```bash
git add scripts/hermes/tests/tick.bats
git commit -m "hermes-tick: failing bats test for 3-tick ollama-unreachable streak"
```

---

## Task 9: `tick.sh` — wire streak counter

**Files:** Modify `scripts/hermes/tick.sh`.

- [ ] **Step 9.1: Extend `probe_ollama` to maintain the streak counter**

Replace `probe_ollama` with:
```bash
probe_ollama() {
  local streak_file="$CHITIN_SINK_ROOT/ollama-unreachable-streak.txt"
  local current=0
  [[ -f "$streak_file" ]] && current="$(cat "$streak_file")"

  if curl -sf --max-time 2 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    echo "ok" > "$TICK_DIR/ollama-probe.txt"
    echo "0" > "$streak_file"
    return 0
  fi

  echo "unreachable" > "$TICK_DIR/ollama-probe.txt"
  echo "$((current + 1))" > "$streak_file"
  return 1
}
```

- [ ] **Step 9.2: Run bats — expect 5/5 passing**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `5 tests, 0 failures`.

- [ ] **Step 9.3: Commit**

```bash
git add scripts/hermes/tick.sh
git commit -m "hermes-tick: ollama-unreachable streak counter in probe_ollama"
```

---

## Task 10: `--dry-run` flag (TDD)

**Files:** Modify `scripts/hermes/tests/tick.bats`, `scripts/hermes/tick.sh`.

- [ ] **Step 10.1: Append dry-run test**

Add at the end of `scripts/hermes/tests/tick.bats`:
```bash
@test "dry-run: external action path — stage 3 invoked with HERMES_TICK_DRY_RUN=1 in env" {
  export STUB_HERMES_PLAN_OUTPUT='{"action":"external","issue_number":10,"reason":"groom","external_action":{"kind":"label","body_or_label":"hermes-autonomous","linked_issue":10}}'

  run "$BATS_TEST_DIRNAME/../tick.sh" --dry-run
  [ "$status" -eq 0 ]

  tick_dir="$CHITIN_SINK_ROOT/ticks/$HERMES_TICK_DATE/$HERMES_TICK_TS"
  [ -f "$tick_dir/act-log.txt" ]
  grep -q 'HERMES_TICK_DRY_RUN=1' "$STUB_LOG"
}
```

- [ ] **Step 10.2: Update the `hermes` stub to echo env when $HERMES_TICK_DRY_RUN is set**

In `scripts/hermes/tests/stubs/hermes`, after the `echo "hermes $*" >> "$STUB_LOG"` line, add:
```bash
[[ -n "${HERMES_TICK_DRY_RUN:-}" ]] && echo "HERMES_TICK_DRY_RUN=${HERMES_TICK_DRY_RUN}" >> "$STUB_LOG"
```

- [ ] **Step 10.3: Run bats — expect new test to fail**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `6 tests, 1 failure` — tick.sh does not parse `--dry-run`.

- [ ] **Step 10.4: Parse `--dry-run` in tick.sh and export for children**

Near the top of `tick.sh`, right after the shebang and `set -euo pipefail`, add:
```bash
HERMES_TICK_DRY_RUN="${HERMES_TICK_DRY_RUN:-0}"
for arg in "$@"; do
  case "$arg" in
    --dry-run) HERMES_TICK_DRY_RUN=1 ;;
    -h|--help)
      echo "Usage: tick.sh [--dry-run]"
      echo "  Runs one staged-tick cycle. Artifacts at \$CHITIN_SINK_ROOT/ticks/<date>/<ts>/."
      echo "  --dry-run : Stage 3 describes tool calls without executing them (handled by prompt-act.md)."
      exit 0
      ;;
  esac
done
export HERMES_TICK_DRY_RUN
```

- [ ] **Step 10.5: Run bats — expect 6/6 passing**

Run: `bats scripts/hermes/tests/tick.bats`

Expected: `6 tests, 0 failures`.

- [ ] **Step 10.6: Commit**

```bash
git add scripts/hermes/tick.sh scripts/hermes/tests/tick.bats scripts/hermes/tests/stubs/hermes
git commit -m "hermes-tick: --dry-run flag; prompt-act honors HERMES_TICK_DRY_RUN=1"
```

---

## Task 11: `prompt-plan.md` (Stage 1 — glm-5.1)

**Files:** Overwrite `scripts/hermes/prompt-plan.md`.

- [ ] **Step 11.1: Write the Stage 1 prompt**

`scripts/hermes/prompt-plan.md`:
```markdown
# Hermes Staged Tick — Stage 1 (PLAN)

You are the planner of a staged autonomous tick for the `chitinhq/chitin` repository.

## Your one job

Read the queue context provided and emit **exactly one JSON object** to stdout
conforming to `scripts/hermes/plan-schema.json`. Emit nothing else — no preface,
no explanation, no markdown. Your entire output must parse as JSON.

## Output schema

```json
{
  "action":        "skip" | "code" | "external",
  "issue_number":  <integer>,
  "reason":        "<one sentence>",
  "diff_request":  { "files": [...], "intent": "..." },   // iff action=="code"
  "external_action": {                                      // iff action=="external"
    "kind":          "comment" | "label" | "pr_open",
    "body_or_label": "...",
    "linked_issue":  <integer>
  }
}
```

## Selection rules

The queue context has three lists: `labeled` (issues with
`hermes-autonomous`), `unlabeled` (open, no labels), and `in_flight_prs`
(PRs linked to an issue).

1. **Work an already-labeled issue if one is eligible.** From `labeled`,
   pick the oldest issue whose number does NOT appear in any
   `in_flight_prs.<pr>.linkedIssue`. Emit either:
   - `{"action":"code", "issue_number":<n>, ..., "diff_request":{...}}`
     if you can state a small, concrete code change in one paragraph.
     Populate `diff_request.files` with 1–5 relative paths you believe
     need editing; `diff_request.intent` with a self-contained instruction
     another model can implement without access to this context.
   - `{"action":"external", "issue_number":<n>, ...,
      "external_action":{"kind":"comment", ...}}`
     if the issue needs clarification from the user before code can be
     written.

2. **If no labeled issue is eligible, groom.** From `unlabeled`, find at
   most one issue that fits **all**:
   - small, clear scope (resolvable in a single PR)
   - code-only (no docs-only, no discussion-only)
   - no words matching `security|breaking|auth|credential` in title or body
   - no open PR linked to it in `in_flight_prs`

   If you find one, emit:
   `{"action":"external", ..., "external_action":{"kind":"label",
     "body_or_label":"hermes-autonomous", "linked_issue":<n>}}`.

3. **Otherwise, skip.** Emit
   `{"action":"skip", "issue_number":0, "reason":"<one sentence>"}`.

## Hard rules

- Never propose `merge`, `force-push`, `delete`, or touching any file
  whose path matches `security|secret|credential|\\.env`.
- Never propose an action on an issue that already has a PR in
  `in_flight_prs`.
- Never emit anything except the JSON object. If you need to explain, put
  the explanation in the `reason` field.
- If the queue is malformed or empty, emit
  `{"action":"skip","issue_number":0,"reason":"empty or malformed queue"}`.

## Your output

A single JSON object matching the schema above. No surrounding text.
```

- [ ] **Step 11.2: Commit**

```bash
git add scripts/hermes/prompt-plan.md
git commit -m "hermes-tick: Stage 1 (PLAN) system prompt for glm-5.1"
```

---

## Task 12: `prompt-code.md` (Stage 2 — qwen3-coder:30b)

**Files:** Overwrite `scripts/hermes/prompt-code.md`.

- [ ] **Step 12.1: Write the Stage 2 prompt**

`scripts/hermes/prompt-code.md`:
```markdown
# Hermes Staged Tick — Stage 2 (CODE)

You are the code-generation stage of a staged autonomous tick. Your only
job is to produce a unified diff that implements the plan handed to you.

## Input (provided as context)

- `plan=<json>`: the Stage 1 plan object. You only care about
  `plan.diff_request.files` and `plan.diff_request.intent`.
- `files=<string>`: the full current contents of the files listed in
  `plan.diff_request.files`, concatenated with `--- FILE: <path> ---`
  separators.

## Output

A single unified diff, and nothing else. Format:

```
--- a/<path>
+++ b/<path>
@@ -<old-start>,<old-count> +<new-start>,<new-count> @@
 <context>
-<removed>
+<added>
 <context>
```

- Paths use `a/` and `b/` prefixes so `git apply` accepts them.
- Every file in `plan.diff_request.files` that you modify must appear.
- Do NOT create new files unless `plan.diff_request.intent` explicitly
  requires it.
- Do NOT emit `commit -m`, PR descriptions, explanations, or any text
  outside the diff.

## Hard rules

- You do not make decisions. If `plan.diff_request.intent` is ambiguous,
  pick the most literal interpretation of the stated intent, and if that
  is not possible, emit an empty diff (zero bytes of output).
- You never propose file deletions unless the intent contains the exact
  word "delete" or "remove".
- You do not touch any file outside `plan.diff_request.files`. If the
  intent requires it, emit an empty diff and exit — Stage 1 will plan
  again next tick.

## Your output

The unified diff. Nothing else. If you cannot produce a valid diff for
any reason, emit zero bytes.
```

- [ ] **Step 12.2: Commit**

```bash
git add scripts/hermes/prompt-code.md
git commit -m "hermes-tick: Stage 2 (CODE) system prompt for qwen3-coder:30b"
```

---

## Task 13: `prompt-act.md` (Stage 3 — glm-5.1)

**Files:** Overwrite `scripts/hermes/prompt-act.md`.

- [ ] **Step 13.1: Write the Stage 3 prompt**

`scripts/hermes/prompt-act.md`:
```markdown
# Hermes Staged Tick — Stage 3 (ACT)

You are the execution stage of a staged autonomous tick. Your job is to
apply the plan's action to the real world using your tools. Governance
v1 (the chitin-governance `pre_tool_call` hook) will veto any tool call
that violates policy; treat veto messages as expected outcomes and log
them, don't retry.

## Input (provided as context)

- `plan=<json>`: the Stage 1 plan object.
- `diff=<string>`: empty unless `plan.action=="code"`, otherwise the
  unified diff produced by Stage 2.
- Environment: `HERMES_TICK_DRY_RUN` is `0` or `1`. When `1`, describe
  the tool calls you would make — do NOT execute them.

## Behavior by `plan.action`

### action == "code"

Required sequence. Perform each step; stop on the first failure.

1. Choose a branch name: `fix/<issue_number>-<slug-of-reason>`.
2. `cd $HOME/workspace/chitin-<issue_number>` — creating the worktree
   first if it does not exist:
   `git -C $HOME/workspace/chitin worktree add $HOME/workspace/chitin-<N> -b <branch> origin/main`.
3. Symlink node_modules:
   `ln -sfn $HOME/workspace/chitin/node_modules $HOME/workspace/chitin-<N>/node_modules`.
4. Apply the diff:
   `printf '%s' "$diff" | git apply -` (in the worktree). If it fails,
   log the stderr and stop.
5. Commit with message `fix: <plan.reason> (#<issue_number>)` using
   `git commit -am` — do not skip hooks.
6. Push: `git push -u origin <branch>`.
7. Open PR: `gh pr create --title "<short title>" --body "Closes
   #<issue_number>\n\n<plan.diff_request.intent>" --base main --head <branch>`.
8. Print the PR URL.

### action == "external"

Based on `plan.external_action.kind`:

- `comment`: `gh issue comment <linked_issue> --repo chitinhq/chitin --body <body_or_label>`
- `label`: `gh issue edit <linked_issue> --repo chitinhq/chitin --add-label <body_or_label>`
- `pr_open`: this form is for opening a PR when a diff was produced in a
  previous tick. v1 behavior: log `pr_open-unsupported-in-v1` and stop.
  (This branch ships in v2 when cross-tick memory is added.)

### action == "skip"

You should never be invoked with `action == "skip"`. If you see this,
log `stage3-invoked-for-skip-action` and exit.

## Hard rules

- Never merge a PR. Never force-push. Never delete a branch.
- Never modify files in `$HOME/workspace/chitin/` — that is the primary
  checkout; all work happens in `$HOME/workspace/chitin-<N>/`.
- Never use `rm -rf` or `git reset --hard` on any path.
- Git identity is `jpleva91@gmail.com` — set by repo config, do not
  override.
- If a governance block is returned for any tool call, log the block
  message to your output and STOP. Do not attempt a workaround.

## Dry-run mode (`HERMES_TICK_DRY_RUN=1`)

Do NOT execute any tool. Instead, print one line per call of the form:

```
WOULD-RUN: <command>
```

…and then exit with a summary line `DRY-RUN COMPLETE`. This lets an
operator inspect the intended sequence without side effects.

## Your output

A plain-text log of each tool call made and its result, one per line.
If you made a PR, the last line must be the PR URL.
```

- [ ] **Step 13.2: Commit**

```bash
git add scripts/hermes/prompt-act.md
git commit -m "hermes-tick: Stage 3 (ACT) system prompt for glm-5.1 (dry-run aware)"
```

---

## Task 14: `README.md` + `MIGRATION.md`

**Files:**
- Create: `scripts/hermes/README.md`
- Create: `scripts/hermes/MIGRATION.md`

- [ ] **Step 14.1: Write README.md**

`scripts/hermes/README.md`:
```markdown
# Hermes Staged Tick v1

Cron-triggered autonomous worker for `chitinhq/chitin`. Three stages,
each with a locked model:

| Stage | Model | Purpose |
|-------|-------|---------|
| PLAN  | `glm-5.1:cloud`      | Read the queue; pick an action |
| CODE  | `qwen3-coder:30b`    | Local on the 3090; write a diff (iff `plan.action=="code"`) |
| ACT   | `glm-5.1:cloud`      | Apply diff, commit, push, open PR, leave comments |

Governance v1 (`go/execution-kernel/internal/gov/`) fires on every tool
call in Stage 3; destructive actions are blocked at the tool boundary,
not in the prompt.

## Running one tick manually

```bash
cd ~/workspace/chitin/scripts/hermes
./tick.sh            # normal — executes tool calls in Stage 3
./tick.sh --dry-run  # Stage 3 prints WOULD-RUN lines, takes no action
```

## Where artifacts land

Each tick writes to `~/chitin-sink/ticks/<YYYY-MM-DD>/<UTC-ISO>/`:

| File | Stage | Contents |
|------|-------|----------|
| `env.txt`          | setup | Scrubbed env (CHITIN/HERMES/PATH) |
| `queue.json`       | setup | `{labeled, unlabeled, in_flight_prs}` |
| `plan.json`        | 1     | Stage 1's plan object |
| `plan-stderr.txt`  | 1     | Stage 1 hermes stderr |
| `ollama-probe.txt` | 2     | `ok` or `unreachable` |
| `diff.patch`       | 2     | Unified diff (iff `action=="code"` + probe ok) |
| `code-stderr.txt`  | 2     | Stage 2 hermes stderr |
| `act-log.txt`      | 3     | Stage 3 tool-call log / WOULD-RUN lines |
| `act-stderr.txt`   | 3     | Stage 3 hermes stderr |
| `tick.log`         | sh    | tick.sh orchestration log |

## Pausing the worker

```bash
hermes cron list                       # find the id of autonomous-worker
hermes cron pause <id>                 # pause
hermes cron resume <id>                # resume
```

## Troubleshooting

- **Empty `plan.json`**: Stage 1 returned non-JSON. See
  `plan-stderr.txt`. Next tick will retry from scratch.
- **`ollama-probe.txt: unreachable`**: local ollama daemon is not
  reachable at `127.0.0.1:11434`. Run `ollama serve` in a terminal. See
  also `~/chitin-sink/ollama-unreachable-streak.txt` — after 3
  consecutive unreachable ticks, the daily-summary WhatsApp cron
  surfaces this.
- **Tool call blocked by governance**: see `act-log.txt` for the block
  dict; also in `~/chitin-sink/gate-log-<date>.jsonl`. This is normal
  behavior — do not disable governance.

## Tests

```bash
bats scripts/hermes/tests/tick.bats         # orchestration tests (6)
scripts/hermes/tests/validate-plans.sh      # schema vs fixtures
```

## Specs and plan

- Spec: `docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md`
- Plan: `docs/superpowers/plans/2026-04-22-hermes-staged-tick.md`
```

- [ ] **Step 14.2: Write MIGRATION.md**

`scripts/hermes/MIGRATION.md`:
```markdown
# Migration: autonomy v1 → staged-tick v1

Run these steps on the machine that hosts the hermes worker, AFTER this
PR is merged to main.

## 1. Find and remove the existing cron

```bash
hermes cron list | grep -A3 autonomous-worker
```

Note the id (first column / `Id:` line). Then:

```bash
hermes cron rm <id>
```

## 2. Tombstone the retired prompt + script

```bash
mkdir -p ~/.hermes/scripts/retired-$(date -u +%Y-%m-%d)
mv ~/.hermes/scripts/autonomous-worker-orders.txt \
   ~/.hermes/scripts/autonomous-worker-context.py \
   ~/.hermes/scripts/retired-$(date -u +%Y-%m-%d)/
```

## 3. Pull the merged branch

```bash
cd ~/workspace/chitin
git pull origin main
```

Verify `scripts/hermes/tick.sh` is present and executable:
```bash
test -x scripts/hermes/tick.sh && echo OK
```

## 4. Register the new cron

```bash
hermes cron create \
  --name autonomous-worker-staged \
  --schedule 'every 10m' \
  --command "$HOME/workspace/chitin/scripts/hermes/tick.sh"
```

## 5. Dry-run before letting cron fire

```bash
cd ~/workspace/chitin
./scripts/hermes/tick.sh --dry-run
```

Inspect the newest tick dir under `~/chitin-sink/ticks/` and confirm
`plan.json` and `act-log.txt` (containing `WOULD-RUN:` lines) look sane.

## 6. Let one real tick fire

Wait ≤10 minutes, then:

```bash
hermes cron list | grep autonomous-worker-staged
```

`Last run` should show `ok`. Inspect the artifact dir for that tick.

## Optional: wire the 3-tick streak to WhatsApp

`tick.sh` maintains `~/chitin-sink/ollama-unreachable-streak.txt`. The
existing `daily-summary` cron does not read it yet. To enable the
spec's 3-tick unreachable alert, add a `tail -c 10
~/chitin-sink/ollama-unreachable-streak.txt` check to
`~/.hermes/scripts/daily-summary-orders.txt` so the cron surfaces
`"⚠ ollama unreachable for N ticks — 3090 likely off"` when N ≥ 3.
Left as a follow-up to keep this PR focused on orchestrator
correctness.

## Rollback

If the new worker misbehaves:

```bash
hermes cron pause <id of autonomous-worker-staged>
```

Work reverts to whatever manual `hermes chat` invocations you run. The
retired prompt can be restored from `~/.hermes/scripts/retired-<date>/`
if you want to re-register the v1 cron.
```

- [ ] **Step 14.3: Commit**

```bash
git add scripts/hermes/README.md scripts/hermes/MIGRATION.md
git commit -m "hermes-tick: README + MIGRATION docs"
```

---

## Task 15: CI workflow — validate plan-schema fixtures on push

**Files:** Create `.github/workflows/hermes-plan-schema.yml`.

- [ ] **Step 15.1: Confirm existing workflow structure for conventions**

Run: `ls .github/workflows/` — note the naming style of other workflow files.

- [ ] **Step 15.2: Write the workflow**

`.github/workflows/hermes-plan-schema.yml`:
```yaml
name: hermes-plan-schema

on:
  push:
    paths:
      - 'scripts/hermes/plan-schema.json'
      - 'scripts/hermes/tests/fixtures/**'
      - 'scripts/hermes/tests/validate-plans.sh'
      - '.github/workflows/hermes-plan-schema.yml'
  pull_request:
    paths:
      - 'scripts/hermes/plan-schema.json'
      - 'scripts/hermes/tests/fixtures/**'
      - 'scripts/hermes/tests/validate-plans.sh'
      - '.github/workflows/hermes-plan-schema.yml'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - name: Install ajv-cli
        run: npm install -g ajv-cli
      - name: Validate fixtures
        run: bash scripts/hermes/tests/validate-plans.sh
```

- [ ] **Step 15.3: Verify YAML syntax locally (if `yq` present)**

Run: `yq '.' .github/workflows/hermes-plan-schema.yml >/dev/null && echo OK` (or skip if `yq` not installed — CI catches syntax errors on push).

Expected: `OK` or no output (skipped).

- [ ] **Step 15.4: Commit**

```bash
git add .github/workflows/hermes-plan-schema.yml
git commit -m "hermes-tick: CI — validate plan-schema fixtures via ajv"
```

---

## Task 16: Push branch + open PR

**Files:** None — git + gh operations.

- [ ] **Step 16.1: Confirm `bats` suite and local schema validator pass one more time**

Run:
```bash
bats scripts/hermes/tests/tick.bats && \
scripts/hermes/tests/validate-plans.sh
```

Expected: `6 tests, 0 failures` then `OK:` lines for all 10 fixtures (or `SKIP: ajv not installed`).

- [ ] **Step 16.2: Verify commit log reads cleanly**

Run: `git log --oneline main..HEAD | head -30`

Expected: one commit per task (roughly 14 commits for Tasks 1–15).

- [ ] **Step 16.3: Push branch**

```bash
git push -u origin spec/hermes-staged-tick-v1
```

- [ ] **Step 16.4: Open the PR**

```bash
gh pr create \
  --title "hermes staged tick v1 — PLAN (glm) → CODE (qwen) → ACT (glm)" \
  --body "$(cat <<'EOF'
## Summary

- Ports octi's stage→tier mapping to the hermes autonomous worker: three
  isolated hermes invocations per cron tick, each with a hardcoded
  model. qwen3-coder:30b runs exactly one stage (CODE), and only when
  the plan requires code and the local 3090 is reachable. glm-5.1:cloud
  handles PLAN and ACT. No same-session delegation — the tier lock is
  architectural.
- Supersedes the abandoned hermes-autonomy-v1 spec; see the post-mortem
  in PR #46 for root-cause.
- Governance v1 (PR #45) is the enforcement layer. This PR adds no new
  governance surface — every Stage 3 tool call goes through
  `pre_tool_call` unchanged.

## Changes

- `scripts/hermes/tick.sh` — bash orchestrator (~200 lines).
- `scripts/hermes/prompt-{plan,code,act}.md` — the three stage system
  prompts.
- `scripts/hermes/plan-schema.json` — JSON Schema for Stage 1's output;
  validated in CI against 10 fixtures.
- `scripts/hermes/tests/tick.bats` — 6 orchestration tests using
  stubbed `hermes`, `gh`, `git`, `curl`.
- `scripts/hermes/README.md` + `MIGRATION.md` — operator docs and
  retire-v1 steps.
- `.github/workflows/hermes-plan-schema.yml` — fixture validation CI.

## Test plan

- [ ] `bats scripts/hermes/tests/tick.bats` → 6 tests, 0 failures
- [ ] `scripts/hermes/tests/validate-plans.sh` (local, with ajv) → all 10 OK
- [ ] GitHub Actions `hermes-plan-schema` workflow green
- [ ] Manual `./scripts/hermes/tick.sh --dry-run` on this box produces
      a sane plan.json + act-log (WOULD-RUN lines)
- [ ] Post-merge: one canary tick on issue #10 under new cron per MIGRATION.md

## Related

- Spec: `docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md`
- Post-mortem of superseded v1: PR #46
- Governance dependency: PR #45 (merged `5cc74bbe8a`)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 16.5: Expect the PR URL; save it for review cycle**

After `gh pr create` exits, the PR URL is the last line of stdout. Follow the chitin review convention: Copilot review → adversarial review via `/review` → fix findings → merge on green. Post-merge, run `scripts/hermes/MIGRATION.md`.

---

## Post-merge (not part of this plan's task list — runbook in MIGRATION.md)

1. Tombstone `~/.hermes/scripts/autonomous-worker-{orders.txt,context.py}`.
2. Remove old cron via `hermes cron rm`.
3. Register new cron pointing at `~/workspace/chitin/scripts/hermes/tick.sh`.
4. `./scripts/hermes/tick.sh --dry-run` once on this box (Layer 3 test).
5. Let one real tick fire on issue #10 (Layer 4 canary). Inspect the
   resulting PR; if it matches dry-run output modulo LLM nondeterminism,
   autonomous operation begins.
