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

  This is a SCAFFOLD — steps have correct shape but logic is stubbed.
  Each step has a TODO marker pointing at what needs real implementation.

args:
  ticket_id:
    description: hermes kanban ticket ID (e.g. t_8bda2f95). Required.

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
  # TODO 2026-05-11 (smoke-test learning): currently a hardcoded stub.
  # Originally tried `pipeline: llm.invoke --provider openclaw` but OpenClaw's
  # gateway doesn't have the `llm-task` tool registered (404 not_found).
  # Two paths to make this real:
  #   (a) Enable llm-task tool in openclaw (would require finding how to
  #       register it — `openclaw mcp set` is for channel bridging, not
  #       agent tools; the right mechanism is somewhere else).
  #   (b) Refactor to: `run: clawta --text "Classify this ticket: $(echo $fetch_ticket.stdout)..."`
  #       This uses the proven clawta path (verified working via the
  #       `Say OK` smoke test on 2026-05-10). Simpler; one fewer integration
  #       to keep alive.
  # Recommendation: (b). The whole point of clawta is to be the Hermes→Clawta wire.
  - id: classify
    description: Classify ticket (SCAFFOLD stub - hardcoded; see TODO above)
    run: >
      echo '{"complexity":"low","capabilities":["go","review"],"estimated_loc":15,"needs_frontier":false}'

  # ─────────────────────────────────────────────────────────────────────
  # 3. Pick driver by matching classify output against agent capability cards
  #
  # TODO: real implementation reads JSON cards from ~/.openclaw/data/agent-cards/
  # ranks by tier + cost + capability fit. For scaffold, returns first candidate.
  # ─────────────────────────────────────────────────────────────────────
  - id: pick_driver
    description: Deterministic matching of need to driver (reads agent cards)
    run: |
      python3 /home/red/.openclaw/workflows/_pick_driver.py
    stdin: $classify.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 4. Operator approval gate (delivered via Hermes → Discord → operator)
  #
  # When this gate fires, Lobster emits an approval-pending envelope
  # with a resume token. Hermes-side integration (TODO post-Discord) picks
  # it up and posts to the operator on Discord #clawta with the prompt.
  # Operator's reply triggers `lobster resume --token <X> --approve <yes|no>`.
  #
  # TODO 2026-05-11 (smoke-test learning): `${ticket_id}` and
  # `$pick_driver.json.driver` were NOT interpolated in the approval prompt
  # during smoke run — the literal text appeared. Need to find Lobster's
  # actual syntax for interpolating args + step outputs into approval
  # strings. The docs mention `stdin: $step.stdout` for chaining input but
  # don't document interpolation in approval prompts or shell run blocks.
  # Possibly needs JS template literal syntax, or a `parse:` step that
  # projects fields into the workflow scope.
  # ─────────────────────────────────────────────────────────────────────
  - id: confirm
    description: Operator confirms the dispatch decision
    approval: >
      Dispatch ${ticket_id} to $pick_driver.json.driver?
      Complexity: $pick_driver.json.complexity | Capabilities: $pick_driver.json.caps_needed

  # ─────────────────────────────────────────────────────────────────────
  # 5. Reassign the kanban ticket to the picked driver lane
  #
  # TODO 2026-05-11 (smoke-test learning): same `$pick_driver.json.driver`
  # interpolation issue as step 4. During smoke run, bash threw
  # "Bad substitution" because `$pick_driver.json.driver` is Lobster syntax
  # that bash doesn't understand, not a shell variable. The args
  # variable `${ticket_id}` DID substitute correctly — so the bug is
  # specific to step-output references inside shell `run:` strings.
  # Fix candidates:
  #   - Use `$pick_driver.json` syntax (without braces) if Lobster supports
  #     it inline
  #   - Add a `parse:` step that explodes pick_driver.json into named
  #     workflow-scope variables that ARE interpolated
  #   - Read the value at command time: `JQ_DRIVER=$(echo "$STEP_PICK_DRIVER_STDOUT" | jq -r .driver)`
  #     — if Lobster sets STEP_<id>_STDOUT env vars
  #   - Investigate Lobster source for the actual mechanism
  # ─────────────────────────────────────────────────────────────────────
  - id: reassign
    description: Move ticket to the worker's lane on kanban
    run: hermes kanban --board chitin assign ${ticket_id} $pick_driver.json.driver

  # ─────────────────────────────────────────────────────────────────────
  # 6. Comment on the ticket for audit (which driver, why)
  # ─────────────────────────────────────────────────────────────────────
  - id: audit_comment
    description: Comment on the ticket so the routing decision is durable on the board
    run: 'hermes kanban --board chitin comment ${ticket_id} --author clawta "Dispatched to $pick_driver.json.driver (classified $pick_driver.json.complexity complexity, capabilities $pick_driver.json.caps_needed) via kanban-dispatch workflow."'

  # ─────────────────────────────────────────────────────────────────────
  # 7. Spawn the actual worker (stubbed — needs per-driver invocation pattern)
  #
  # TODO: real implementation reads card.invocation from the picked driver,
  # spawns the worker subprocess with ticket_id as context, waits for PR.
  # For scaffold, just logs.
  # ─────────────────────────────────────────────────────────────────────
  - id: spawn_worker
    description: Start the worker
    run: >
      echo "SCAFFOLD: would spawn $pick_driver.json.driver worker for ${ticket_id} now"

# Future steps to add as we layer in:
#   8. monitor PR / status (resumable poll)
#   9. copilot judge step on completion (free GPT-4.1 — sanity-check the diff)
#   10. final approval gate or auto-merge
#
# Known issues discovered during 2026-05-11 smoke run (preserved for the
# next person picking this up):
#   - llm.invoke --provider openclaw → 404 (llm-task tool not registered).
#     See `classify` step TODO. Recommendation: refactor to clawta shell call.
#   - ${step.field} interpolation in shell `run:` and `approval:` strings
#     doesn't work — Lobster's interpolation syntax for step outputs is
#     undocumented (or different). See `confirm` and `reassign` step TODOs.
#   - args (e.g. ${ticket_id}) DO interpolate correctly in both `run:` and
#     `approval:` — that's the working baseline.
#
# Test the workflow:
#   export OPENCLAW_URL=http://127.0.0.1:18789
#   export OPENCLAW_TOKEN=$(python3 -c "import json; print(json.load(open('/home/red/.openclaw/openclaw.json'))['gateway']['auth']['token'])")
#   pnpm exec lobster run --file ~/.openclaw/workflows/kanban-dispatch.lobster --args-json '{"ticket_id":"<test ticket id>"}'
#   # halts at approval gate; copy the resumeToken from output, then:
#   pnpm exec lobster resume --token <copied> --approve yes
