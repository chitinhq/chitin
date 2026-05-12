name: kanban-dispatch
description: |
  Dispatch one Hermes kanban ticket to the appropriate worker driver.

  Invoked by:
    - Hermes via `clawta --text "dispatch ticket t_X"` (then Clawta runs this workflow)
    - Operator directly via Discord #clawta bot
    - Or for testing: `lobster run --file ~/.openclaw/workflows/kanban-dispatch.lobster --args-json '{"ticket_id":"t_X"}'`

  Architecture (post-2026-05-11):
    Hermes = operator UX (free, never dispatches except its own copilot helper)
    Clawta = backend dispatch authority (this workflow IS Clawta's brain for routing)
    Workers = codex / claude-code / copilot / gemini (the actual coders)

  Interpolation syntax (per Lobster 2026.4.6, source at
  node_modules/.pnpm/@clawdbot+lobster@2026.4.6/.../dist/src/workflows/file.js:560-580):

    Args:        ${arg_name}                (BRACED — only form for args)
    Step stdout: $<step_id>.stdout          (UNBRACED)
    Step JSON:   $<step_id>.json.<field>    (UNBRACED, dot-notation)
    Nested:      $<step_id>.json.a.b.c      (UNBRACED, supported)

  Asymmetry trap: ${step.json.field} (braced step ref) is NOT supported —
  it survives Lobster's regex as literal text, then shell-errors with
  "Bad substitution" because POSIX shells reject dots inside ${...}.
  Always use the UNBRACED form for step references; BRACED only for args.

  Silent failure modes:
  - Producing step's stdout isn't valid JSON → $step.json.X substitutes
    empty string (parseJson returns undefined silently)
  - Step ID typo or step skipped by when: → literal $step.json.X
    survives into the shell command

  This is a SCAFFOLD — steps have correct shape but logic is stubbed.
  Each step has a TODO marker pointing at what needs real implementation.

args:
  ticket_id:
    description: hermes kanban ticket ID (e.g. t_8bda2f95). Required.
  force_driver:
    description: |
      Optional driver override (e.g. "codex", "claude-code", "gemini",
      "copilot"). When set, pick_driver bypasses capability matching and
      returns this driver verbatim. Used by
      scripts/smoke-hermes-clawta-chain.sh to exercise each frontier-coder
      card deterministically. Empty in normal operation.
    default: ""

steps:
  # ─────────────────────────────────────────────────────────────────────
  # 1. Read the ticket from Hermes kanban
  # ─────────────────────────────────────────────────────────────────────
  - id: fetch_ticket
    description: Read the kanban ticket JSON
    run: hermes kanban --board chitin show ${ticket_id} --json

  # ─────────────────────────────────────────────────────────────────────
  # 2. Classify complexity + required capabilities (LLM step via Clawta)
  #
  # TODO: refine the prompt. Output schema:
  #   {
  #     "complexity": "low" | "med" | "high",
  #     "capabilities": ["go", "ts", "python", "refactor", "debug", "review", ...],
  #     "estimated_loc": number,
  #     "needs_frontier": bool
  #   }
  # ─────────────────────────────────────────────────────────────────────
  # Classify the ticket via clawta (option (b) from the original TODO).
  # The clawta wire is the proven Hermes→Clawta path; routing classification
  # through it keeps the same single dispatch surface and avoids depending
  # on an upstream `llm-task` tool that isn't registered in the gateway.
  #
  # Uses $fetch_ticket.stdout (UNBRACED) — see header for syntax rules.
  # NOTE: ticket body inlined directly into a shell-quoted string breaks
  # parsing the moment the ticket JSON contains embedded double quotes
  # (which it always does). Read via stdin + variable assignment inside
  # a bash heredoc so the JSON never touches the shell parser as a
  # literal. Same pattern as spawn_worker.
  - id: classify
    description: Classify ticket via clawta (glm-agent on glm-5.1:cloud)
    run: |
      bash -c "$(cat <<'BASH_SCRIPT'
      set -euo pipefail
      TICKET=$(cat)
      PROMPT="Classify this ticket and reply with ONLY a JSON object (no prose, no markdown fences): {\"complexity\": \"low\" | \"med\" | \"high\", \"capabilities\": [\"go\" | \"ts\" | \"python\" | \"refactor\" | \"debug\" | \"review\"], \"estimated_loc\": <integer>, \"needs_frontier\": <true|false>}. Ticket JSON: $TICKET"
      clawta --text "$PROMPT"
      BASH_SCRIPT
      )"
    stdin: $fetch_ticket.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 3. Pick driver by matching classify output against agent capability cards
  #
  # TODO: real implementation reads JSON cards from ~/.openclaw/data/agent-cards/
  # ranks by tier + cost + capability fit. For scaffold, returns first candidate.
  # ─────────────────────────────────────────────────────────────────────
  - id: pick_driver
    description: Deterministic matching of need to driver (reads agent cards)
    run: |
      FORCE_DRIVER="${force_driver}" python3 /home/red/.openclaw/workflows/_pick_driver.py
    stdin: $classify.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 4. Operator approval gate (delivered via Hermes → Discord → operator)
  #
  # When this gate fires, Lobster emits an approval-pending envelope
  # with a resume token. Hermes-side integration (TODO post-Discord) picks
  # it up and posts to the operator on Discord #clawta with the prompt.
  # Operator's reply triggers `lobster resume --token <X> --approve <yes|no>`.
  # ─────────────────────────────────────────────────────────────────────
  # Uses ${ticket_id} (arg, braced) + $pick_driver.json.* (step ref, UNBRACED).
  - id: confirm
    description: Operator confirms the dispatch decision
    approval: >
      Dispatch ${ticket_id} to $pick_driver.json.driver?
      Complexity: $pick_driver.json.complexity | Capabilities: $pick_driver.json.caps_needed

  # ─────────────────────────────────────────────────────────────────────
  # 5. Reassign the kanban ticket to the picked driver lane
  # ─────────────────────────────────────────────────────────────────────
  # Uses $pick_driver.json.driver (UNBRACED) — see header for syntax rules.
  - id: reassign
    description: Move ticket to the worker's lane on kanban
    run: hermes kanban --board chitin assign ${ticket_id} $pick_driver.json.driver

  # ─────────────────────────────────────────────────────────────────────
  # 6. Comment on the ticket for audit (which driver, why)
  # ─────────────────────────────────────────────────────────────────────
  # Uses ${ticket_id} (arg) + $pick_driver.json.driver / .complexity
  # (UNBRACED step refs). NOTE: do NOT inline JSON-array fields like
  # $pick_driver.json.caps_needed into shell-quoted strings — Lobster
  # substitutes the raw JSON value (with embedded double quotes) which
  # breaks shell parsing (dash: "Syntax error: \"(\" unexpected").
  # Capabilities are recoverable from the pick_driver step's chain
  # event metadata; no need to repeat them in the announce message.
  - id: audit_comment
    description: Announce dispatch on kanban + broadcast to Clawta's Discord channel
    run: |
      hermes kanban --board chitin comment ${ticket_id} --author clawta "🦞 Starting dispatch to $pick_driver.json.driver | complexity $pick_driver.json.complexity | via kanban-dispatch workflow"
      openclaw message send --channel discord --account default --text "🦞 Starting ${ticket_id} to $pick_driver.json.driver | complexity $pick_driver.json.complexity" 2>/dev/null || true

  # ─────────────────────────────────────────────────────────────────────
  # 7. Spawn the picked frontier-coder CLI as a worker.
  #
  # Reads ~/.openclaw/data/agent-cards/<driver>.json, picks the cheapest
  # model (lowest premium_cost — matches _pick_driver.py's ranking),
  # substitutes {model} and {prompt} into card.invocation.args, execs
  # the leaf CLI. The exec is gated by chitin under driver=clawta (the
  # spawning surface); the leaf CLI's own PreToolUse/BeforeTool hook
  # handles inner-hop chain events with driver=<cli>.
  #
  # CHITIN_DRIVER stamp: the final exec is wrapped in
  # `env CHITIN_DRIVER=clawta ...` so the spawn hop itself records
  # driver=clawta in the chain ledger. The smoke's I2 invariant ("at
  # least one event with actor=clawta") depends on this — without the
  # stamp the spawn would inherit whatever the parent stamped (e.g.,
  # hermes or claude-code) and clawta would be invisible in the
  # ledger. Note the env var only stamps the exec'd child; the leaf
  # CLI's own PreToolUse hook overrides CHITIN_DRIVER with its own id
  # for inner tool calls, so the inner hop is still attributed
  # correctly.
  #
  # Quoting safety: the raw ticket JSON is fed to the step via stdin
  # (lobster's resolveShellStdin path, not template substitution), so
  # apostrophes/backticks/dollars in the ticket body cannot break shell
  # parsing. Inside the step we read the JSON from /dev/stdin and pull
  # the body with jq. The only template substitution that hits the run:
  # string is $pick_driver.json.driver, which is a card id like "codex"
  # (alphanumeric + dash, no quoting hazard).
  # ─────────────────────────────────────────────────────────────────────
  - id: spawn_worker
    description: Spawn the picked frontier-coder CLI with the ticket as prompt
    run: |
      bash -c "$(cat <<'BASH_SCRIPT'
      set -euo pipefail
      DRIVER='$pick_driver.json.driver'
      CARD_PATH="$HOME/.openclaw/data/agent-cards/$DRIVER.json"
      if [[ ! -f "$CARD_PATH" ]]; then
        echo "spawn_worker: card not found for driver=$DRIVER at $CARD_PATH" >&2
        exit 1
      fi
      # Read the full ticket JSON from stdin (safe from quote injection) and
      # extract the body. `hermes kanban show --json` wraps the task as
      # {task:{...},comments:[...],events:[...],...}, so we look at .task.body
      # first; fall back to .body for the legacy/unwrapped shape; fall back to
      # the raw stdin if neither path resolves.
      TICKET_JSON=$(cat)
      TICKET_BODY=$(printf '%s' "$TICKET_JSON" | jq -r '.task.body // .body // empty')
      if [[ -z "$TICKET_BODY" ]]; then
        TICKET_BODY="$TICKET_JSON"
      fi
      PROMPT="Implement the ticket: $TICKET_BODY"
      MODEL=$(jq -r '[.models[]] | sort_by(.premium_cost) | .[0].id' "$CARD_PATH")
      CMD=$(jq -r '.invocation.cmd' "$CARD_PATH")
      ARGS_JSON=$(jq -c --arg model "$MODEL" --arg prompt "$PROMPT" \
        '.invocation.args | map(if . == "{model}" then $model elif . == "{prompt}" then $prompt else . end)' \
        "$CARD_PATH")
      # NUL-delimit so multi-line prompts survive intact; -r would split on \n.
      mapfile -d '' -t ARGV < <(echo "$ARGS_JSON" | jq -j '.[] | . + "\u0000"')
      # Emit a chain event attributing this spawn hop to clawta. Without
      # this, the smoke's I2 invariant ("at least one event with
      # driver=clawta") only finds the spawn if some downstream tool call
      # bubbles back through the chitin hook before the exec replaces the
      # process. Lobster doesn't expose a per-run id env var (verified in
      # lobster 2026.4.6 dist), so we synthesize one from epoch+pid; the
      # gate writes a `driver=clawta, agent=clawta` row into the daily
      # ledger via OnDecision (gate_hook.go:386). The carrier is a benign
      # Bash echo — the default-allow-shell rule passes it without any
      # side effect (the gate evaluates the action; it doesn't run it).
      # `|| true` keeps the spawn alive if the emission ever fails
      # (gov.db locked, etc.); chain telemetry must never block the exec.
      SPAWN_RUN_ID="clawta-spawn-$(date +%s)-$$"
      printf '{"session_id":"%s","tool_name":"Bash","tool_input":{"command":"echo clawta-spawn-marker"}}' \
        "$SPAWN_RUN_ID" \
        | CHITIN_DRIVER=clawta chitin-kernel gate evaluate --hook-stdin --agent=clawta >/dev/null 2>&1 || true
      echo "spawn_worker: exec $CMD ${ARGV[*]}" >&2
      exec env CHITIN_DRIVER=clawta "$CMD" "${ARGV[@]}"
      BASH_SCRIPT
      )"
    stdin: $fetch_ticket.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 8. Finalize — push branch, open PR, comment back to kanban + Discord.
  #
  # Runs after spawn_worker returns (leaf CLI exited). Locates the swarm
  # worktree by ticket id, detects the feature branch the worker committed
  # to, pushes it, opens a PR via gh, and announces the result on both
  # surfaces. Every external call is `|| true` so a single failure (gh
  # rate-limit, missing GH_TOKEN, etc.) never leaves the ticket without
  # a final status comment — kanban comment is the load-bearing record.
  # ─────────────────────────────────────────────────────────────────────
  - id: finalize_dispatch
    description: Push branch, open PR, comment + broadcast outcome
    run: |
      bash -c "$(cat <<'BASH_SCRIPT'
      set -uo pipefail  # NOT -e: each step below is best-effort
      DRIVER='$pick_driver.json.driver'
      # Locate the most recent swarm worktree for this ticket. Pattern is
      # ~/.cache/chitin/swarm-worktrees/swarm-<slug>-${ticket_id}/ — the
      # leaf CLI creates it, slug varies by ticket title. Newest match wins
      # if the worker re-tried.
      WORKTREE=$(ls -td "$HOME"/.cache/chitin/swarm-worktrees/swarm-*-${ticket_id}*/ 2>/dev/null | head -1)
      if [[ -z "$WORKTREE" || ! -d "$WORKTREE" ]]; then
        MSG="🦞 spawn_worker completed but no worktree found for ${ticket_id} — worker may have failed before committing."
        hermes kanban --board chitin comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
        exit 0
      fi
      cd "$WORKTREE"
      BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
      if [[ -z "$BRANCH" || "$BRANCH" == "main" || "$BRANCH" == "HEAD" ]]; then
        MSG="🦞 ${ticket_id}: worker finished but no feature branch checked out (HEAD=$BRANCH). No PR opened."
        hermes kanban --board chitin comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
        exit 0
      fi
      # Push (idempotent — git push on already-up-to-date is fine).
      git push -u origin "$BRANCH" 2>&1 | tail -3 || true
      # Open PR. Reuse if one already exists for this head.
      EXISTING=$(gh pr list --head "$BRANCH" --json url --jq '.[0].url // empty' 2>/dev/null || echo "")
      if [[ -n "$EXISTING" ]]; then
        PR_URL="$EXISTING"
      else
        # gh pr create outputs the URL on success. Capture last line.
        PR_URL=$(gh pr create \
          --title "$(git log -1 --pretty=%s)" \
          --body "Closes ticket ${ticket_id} (dispatched via clawta to $DRIVER). Auto-opened by kanban-dispatch.lobster finalize step. Branch $BRANCH." \
          2>&1 | grep -oE 'https://github.com/[^ ]+/pull/[0-9]+' | head -1 || echo "")
      fi
      if [[ -z "$PR_URL" ]]; then
        MSG="🦞 ${ticket_id}: branch $BRANCH pushed, but gh pr create failed. Manual PR open needed."
      else
        MSG="🦞 ${ticket_id}: done. Branch: $BRANCH. PR: $PR_URL"
      fi
      hermes kanban --board chitin comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
      openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
      BASH_SCRIPT
      )"

# Future steps to add as we layer in:
#   8. monitor PR / status (resumable poll)
#   9. copilot judge step on completion (free GPT-4.1 — sanity-check the diff)
#   10. final approval gate or auto-merge
#
# Working syntax confirmed 2026-05-11 (Task 11 of the finish-plan):
#   Task 10 read Lobster 2026.4.6 source (dist/src/workflows/file.js:560-580)
#   and Task 11 ran a positive+negative smoke pair. The unbraced step-ref
#   form ($pick_driver.json.driver) substitutes correctly in shell run:
#   blocks; the braced form (${pick_driver.json.driver}) shell-errors with
#   "Bad substitution". The earlier "Known issues" footer claimed step
#   interpolation was broken — that was wrong about the syntax. See the
#   "Interpolation syntax" block in the header description for the rules.
#
# Test the workflow:
#   export OPENCLAW_URL=http://127.0.0.1:18789
#   export OPENCLAW_TOKEN=$(python3 -c "import json; print(json.load(open('/home/red/.openclaw/openclaw.json'))['gateway']['auth']['token'])")
#   pnpm exec lobster run --file ~/.openclaw/workflows/kanban-dispatch.lobster --args-json '{"ticket_id":"<test ticket id>"}'
#   # halts at approval gate; copy the resumeToken from output, then:
#   pnpm exec lobster resume --token <copied> --approve yes
