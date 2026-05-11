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
  - id: classify
    description: Classify ticket via clawta (glm-agent on glm-5.1:cloud)
    run: >
      clawta --text "Classify this ticket and reply with ONLY a JSON
      object (no prose, no markdown fences):
      {\"complexity\": \"low\" | \"med\" | \"high\",
       \"capabilities\": [\"go\" | \"ts\" | \"python\" | \"refactor\" | \"debug\" | \"review\"],
       \"estimated_loc\": <integer>,
       \"needs_frontier\": <true|false>}.
      Ticket JSON: $fetch_ticket.stdout"

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
  # Uses ${ticket_id} (arg) + $pick_driver.json.* (UNBRACED step refs).
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
