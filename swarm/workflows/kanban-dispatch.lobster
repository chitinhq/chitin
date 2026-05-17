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
  board:
    description: |
      Kanban board slug (e.g. "chitin", "readybench"). Determines which
      repo and base branch the dispatch targets. Board config is read
      via chitin-kernel board-config for workspace_root, default_branch,
      and repo — so the dispatch lands in the correct repo on the
      correct integration branch.
    default: chitin
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
  # 1. Resolve board configuration — repo path and base branch per board.
  #
  # The `board` arg (default "chitin") determines which kanban board's
  # config to read. chitin-kernel board-config returns the workspace_root,
  # default_branch, and repo for the board, so the dispatch can work on
  # any repo, not just chitin.
  # ─────────────────────────────────────────────────────────────────────
  - id: resolve_board
    description: Read board config for workspace_root, default_branch, and repo
    run: |
      BOARD=${board}
      REPO=$(chitin-kernel board-config "$BOARD" repo 2>/dev/null || echo "chitinhq/chitin")
      WORKSPACE_ROOT=$(chitin-kernel board-config "$BOARD" workspace_root 2>/dev/null || echo "$HOME/workspace/chitin")
      DEFAULT_BRANCH=$(chitin-kernel board-config "$BOARD" default_branch 2>/dev/null || echo "main")
      # Output JSON for downstream steps to reference
      printf '{"board":"%s","repo":"%s","workspace_root":"%s","default_branch":"%s"}\n' "$BOARD" "$REPO" "$WORKSPACE_ROOT" "$DEFAULT_BRANCH"

  # ─────────────────────────────────────────────────────────────────────
  # 2. Read the ticket from the correct kanban board
  # ─────────────────────────────────────────────────────────────────────
  - id: fetch_ticket
    description: Read the kanban ticket JSON
    run: hermes kanban --board $resolve_board.json.board show ${ticket_id} --json

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
  # Why this calls `openclaw agent` directly instead of `clawta --text`:
  # the `clawta` wrapper has unanchored pattern detection for dispatch
  # intent — it matches any `t_xxxxxxxx` id and any dispatch-verb word
  # anywhere in the prompt. The classify prompt embeds the full ticket
  # JSON, which always contains a ticket id and frequently contains
  # dispatch-verb words in the body. That misfires as a recursive
  # dispatch call, runs the workflow on itself, and returns a failure
  # envelope instead of classification JSON. Every dispatch on
  # 2026-05-12 failed for this reason until the bypass landed (#529).
  - id: classify
    description: Classify ticket via clawta agent (direct, no wrapper)
    run: |
      bash -c "$(cat <<'BASH_SCRIPT'
      set -euo pipefail
      TICKET=$(cat)
      PROMPT='Classify this ticket and reply with ONLY a JSON object (no prose, no markdown fences): {"complexity": "low" | "med" | "high", "capabilities": ["go" | "ts" | "python" | "refactor" | "debug" | "review"], "estimated_loc": <integer>, "needs_frontier": <true|false>}. Ticket JSON:'"$TICKET"
      OPENCLAW_BIN="${OPENCLAW_BIN:-$HOME/.vite-plus/bin/openclaw}"
      "$OPENCLAW_BIN" agent --agent clawta --message "$PROMPT"
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
  # 4. Explain and persist the routing decision before approval.
  #
  # The explanation is generated by the clawta agent, then recorded in
  # ~/.openclaw/data/clawta_decisions.db and mirrored into the chitin
  # event chain as a clawta.routing_decision event. The kanban
  # comment is deliberately separate from the later start comment: users
  # asking "why did you dispatch t_X to Y?" get the original wording, not
  # an after-the-fact summary.
  # ─────────────────────────────────────────────────────────────────────
  - id: router_explanation
    description: Explain the selected driver + model in Clawta's voice
    run: |
      bash -c "$(cat <<'BASH_SCRIPT'
      set -euo pipefail
      TICKET=$(cat)
      PROMPT=$(cat <<PROMPT_TEXT
      Ticket JSON: $TICKET

      Clawta picked driver=$pick_driver.json.driver and model=$pick_driver.json.model for this dispatch.
      In 2 sentences, explain why this driver and model are the right pick. Ground the answer in the ticket's code complexity, language or stack, ticket type, required capabilities, and cost tradeoff. Return plain text only.
      PROMPT_TEXT
      )
      OPENCLAW_BIN="${OPENCLAW_BIN:-$HOME/.vite-plus/bin/openclaw}"
      "$OPENCLAW_BIN" agent --agent clawta --message "$PROMPT"
      BASH_SCRIPT
      )"
    stdin: $fetch_ticket.stdout

  - id: routing_record
    description: Store routing explanation in SQLite, kanban, and chain
    run: |
      set -eu
      REASONING=$(cat)
      COMMENT=$(printf '%s' "$REASONING" | python3 /home/red/.openclaw/workflows/clawta_decisions.py record \
        --ticket-id ${ticket_id} \
        --driver "$pick_driver.json.driver" \
        --model "$pick_driver.json.model" \
        --shape-bucket "$pick_driver.json.shape_bucket" \
        --selection-mode "$pick_driver.json.selection_mode")
      hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "🦞 $COMMENT"
    stdin: $router_explanation.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 5. Operator approval gate (delivered via Hermes → Discord → operator)
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
      Model: $pick_driver.json.model | Complexity: $pick_driver.json.complexity

  # ─────────────────────────────────────────────────────────────────────
  # 6. Reassign the kanban ticket to the picked driver lane
  # ─────────────────────────────────────────────────────────────────────
  # Uses $pick_driver.json.driver (UNBRACED) — see header for syntax rules.
  - id: reassign
    description: Move ticket to the worker's lane on kanban
    run: hermes kanban --board $resolve_board.json.board assign ${ticket_id} $pick_driver.json.driver

  # ─────────────────────────────────────────────────────────────────────
  # 7. Comment on the ticket for audit (which driver, why)
  # ─────────────────────────────────────────────────────────────────────
  # Uses ${ticket_id} (arg) + $pick_driver.json.driver / .complexity
  # (UNBRACED step refs). NOTE: do NOT inline JSON-array fields like
  # $pick_driver.json.caps_needed into shell-quoted strings — Lobster
  # substitutes the raw JSON value (with embedded double quotes) which
  # breaks shell parsing (dash: "Syntax error: \"(\" unexpected").
  # Capabilities are recoverable from the pick_driver step's chain
  # event metadata; no need to repeat them in the announce message.
  - id: audit_comment
    description: Announce dispatch + flip ready→in_progress + broadcast to Discord
    run: |
      hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "🦞 Starting dispatch to $pick_driver.json.driver/$pick_driver.json.model | complexity $pick_driver.json.complexity | via kanban-dispatch workflow"
      # Slice D: lifecycle discipline. Flip to in_progress so the board
      # reflects reality. kanban-flow is idempotent when the ticket is
      # already in_progress (e.g. clawta-poller flipped it before this
      # workflow ran), so failure here is benign.
      kanban-flow start ${ticket_id} --author clawta 2>/dev/null || true
      env \
        CHITIN_DISPATCH_MODEL="$pick_driver.json.model" \
        CHITIN_DISPATCH_SOUL_ID="$pick_driver.json.soul_id" \
        CHITIN_DISPATCH_SOUL_HASH="$pick_driver.json.soul_hash" \
        CHITIN_DISPATCH_AGENT_FINGERPRINT="$pick_driver.json.agent_fingerprint" \
        CHITIN_ACTIVE_SOUL="$pick_driver.json.soul_id" \
        kanban-flow stamp-run-identity ${ticket_id} --author clawta 2>/dev/null || true
      openclaw message send --channel discord --account default --text "🦞 Starting ${ticket_id} to $pick_driver.json.driver/$pick_driver.json.model | complexity $pick_driver.json.complexity" 2>/dev/null || true

  # ─────────────────────────────────────────────────────────────────────
  # 8. Spawn the picked frontier-coder CLI as a worker.
  #
  # Reads ~/.openclaw/data/agent-cards/<driver>.json and honors the
  # model selected by _pick_driver.py (complexity-aware and ELO-ready).
  # substitutes {model} into card.invocation.args and passes the full
  # prompt via stdin inside the subprocess helper so long ticket bodies
  # stay out of argv / ps output. The leaf CLI exec is gated by chitin under driver=clawta (the
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
      ROLE_DEFAULT="${ROLE:-programmer}"
      ROLE=$(printf '%s' "$TICKET_JSON" | python3 "$HOME/.openclaw/workflows/ticket_metadata.py" "$ROLE_DEFAULT" 2>/dev/null || true)
      if [[ -z "$ROLE" ]]; then
        ROLE="$ROLE_DEFAULT"
      fi
      # Extract ticket id from the JSON rather than depending on
      # lobster's ${ticket_id} substitution. Lobster substitutes
      # unbraced step refs inside this single-quoted heredoc (the
      # DRIVER='$pick_driver.json.driver' pattern works), but braced
      # arg refs (${ticket_id}) don't — they survive as literal text
      # and bash errors with `unbound variable` under set -u.
      TICKET_ID=$(printf '%s' "$TICKET_JSON" | jq -r '.task.id // .id // empty')
      if [[ -z "$TICKET_ID" ]]; then
        echo "spawn_worker: could not extract ticket id from JSON" >&2
        exit 1
      fi
      # Pre-create the swarm worktree at the path finalize_dispatch
      # expects. Without this, workers either (a) commit on whatever
      # branch they happened to be on in the chitin primary checkout,
      # leaving nothing for finalize to find, or (b) call
      # scripts/create-worktree.sh which defaults to ../chitin-<branch>/
      # — a path finalize doesn't search. Lobster owns the worktree
      # contract end-to-end now; the leaf CLI just edits and commits.
      WORKTREE_DIR="$HOME/.cache/chitin/swarm-worktrees/${DRIVER}-${TICKET_ID}"
      TICKET_ID_SHORT="${TICKET_ID#t_}"
      BRANCH="agent/${DRIVER}-${TICKET_ID_SHORT}"
      BOARD_SLUG='$resolve_board.json.board'
      CHITIN_REPO=$(chitin-kernel board-config "$BOARD_SLUG" workspace_root 2>/dev/null || echo "$HOME/workspace/chitin")
      DEFAULT_BRANCH=$(chitin-kernel board-config "$BOARD_SLUG" default_branch 2>/dev/null || echo "main")
      mkdir -p "$(dirname "$WORKTREE_DIR")"
      if [[ ! -d "$WORKTREE_DIR" ]]; then
        # Use git worktree add directly — scripts/create-worktree.sh
        # has extra bootstrap logic we don't need here. If the branch
        # already exists (re-dispatch case), check it out instead of
        # erroring on duplicate.
        git -C "$CHITIN_REPO" worktree add -b "$BRANCH" "$WORKTREE_DIR" "origin/$DEFAULT_BRANCH" 2>&1 \
          || git -C "$CHITIN_REPO" worktree add "$WORKTREE_DIR" "$BRANCH" 2>&1
      fi
      if ! cd "$WORKTREE_DIR"; then
        echo "spawn_worker: failed to enter worktree $WORKTREE_DIR" >&2
        exit 1
      fi
      # Inline role SKILL.md into the worker's system prompt. Role is
      # set via ROLE env var (default 'programmer'). Falls back to
      # no-prefix if the role file is missing. printf %s avoids the
      # YAML-scanner-breaking multi-line bash string.
      ROLE_SKILL="$HOME/.openclaw/roles/$ROLE/SKILL.md"
      if [[ -f "$ROLE_SKILL" ]]; then
        ROLE_HEADER=$(printf '# Worker role: %s\n\n%s\n\n' "$ROLE" "$(cat "$ROLE_SKILL")")
      else
        ROLE_HEADER=""
      fi
      # The working-environment header makes the contract concrete:
      # the worker is already in a worktree on a feature branch; it
      # must NOT try to create another, push, or open a PR — lobster's
      # finalize_dispatch step does those. Without these explicit DO
      # NOTs, frontier LLMs read the role SKILL.md, attempt the full
      # SDLC, and fail at the create-worktree step (path mismatch) or
      # the push step (the worker has no GH_TOKEN).
      ENV_HEADER=$(printf '# Working environment\n\nYou are already in a fresh git worktree at %s on branch %s (based on origin/%s).\n\nDO NOT create another worktree or branch — you are in one.\nDO NOT run `git push` or `gh pr create` — lobster handles those after you exit.\n\nYour job: make the code change for the ticket, run any relevant tests, then commit with a clear message referencing %s. Then exit. That is it.\n\n# Ticket %s\n\n' \\\n        "$WORKTREE_DIR" "$BRANCH" "$DEFAULT_BRANCH" "$TICKET_ID" "$TICKET_ID")
      PROMPT="${ROLE_HEADER}${ENV_HEADER}${TICKET_BODY}"
      MODEL='$pick_driver.json.model'
      if [[ -z "$MODEL" || "$MODEL" == "null" || "$MODEL" == '$pick_driver.json.model' ]]; then
        MODEL=$(jq -r '[.models[]] | sort_by(.premium_cost) | .[0].id' "$CARD_PATH")
      fi
      if ! jq -e --arg model "$MODEL" '.models[]? | select(.id == $model)' "$CARD_PATH" >/dev/null; then
        echo "spawn_worker: selected model $MODEL is not present on driver=$DRIVER card" >&2
        exit 1
      fi
      ROLE="${ROLE:-programmer}"
      hermes kanban --board $resolve_board.json.board comment "$TICKET_ID" --author clawta \
        "dispatch driver=$DRIVER model=$MODEL soul_id=$pick_driver.json.soul_id soul_hash=$pick_driver.json.soul_hash role=$ROLE" 2>/dev/null || true
      SOUL_ID='$pick_driver.json.soul_id'
      SOUL_PROMPT=""
      for SOUL_PATH in \
        "$CHITIN_REPO/souls/canonical/${SOUL_ID}.md" \
        "$CHITIN_REPO/souls/experimental/${SOUL_ID}.md"
      do
        if [[ -f "$SOUL_PATH" ]]; then
          SOUL_PROMPT=$(cat "$SOUL_PATH")
          break
        fi
      done
      CMD=$(jq -r '.invocation.cmd' "$CARD_PATH")
      ARGS_JSON=$(jq -c --arg model "$MODEL" \
        '.invocation.args | map(if . == "{model}" then $model else . end)' \
        "$CARD_PATH")
      # Per-dispatch envelope: each worker gets its own spent_calls
      # counter. Without this, the worker inherits the parent's
      # CHITIN_BUDGET_ENVELOPE (which beats the ~/.chitin/current-envelope
      # file in resolveEnvelope) and aggregates spend with every other
      # agent dispatched under the same parent — one cap hit locks
      # whichever agent calls next.
      NEW_ENV=$(chitin-kernel envelope create --calls=10000 --bytes=33554432 --usd=2.0)
      PROMPT_BYTES=$(printf '%s' "$PROMPT" | wc -c | tr -d ' ')
      echo "spawn_worker: spawning driver=$DRIVER model=$MODEL cmd=$CMD prompt_bytes=$PROMPT_BYTES envelope=$NEW_ENV" >&2

      # Resolve the file-system scope from the bound spec (Constitution §1.1,
      # added 2026-05-17 after portal MVP day-0 retro). Emits JSON like
      # {"may": ["apps/portal/**"], "must_not": ["frontend/**"], "source": "..."}.
      # If the spec has no `## File-system scope` section (legacy spec), arrays
      # are empty and the validator in spawn_worker_subprocess.py treats it
      # as no-enforcement (back-compat). Helper script never errors — always
      # emits valid JSON on stdout.
      FS_SCOPE_JSON=$(python3 "$HOME/.openclaw/workflows/resolve-file-system-scope.py" \
        --board "$BOARD_SLUG" --ticket-id "${TICKET_ID}" 2>/dev/null \
        || echo '{"may":[],"must_not":[],"source":null}')
      echo "spawn_worker: file_system_scope: $FS_SCOPE_JSON" >&2

      # Spawn worker as subprocess with output capture instead of exec.
      # This enables: output capture, result observation, and parent-child
      # session tracking via parent_chain_id in chain ledger.
      #
      # The prompt is fed to jq over stdin (--rawfile /dev/stdin), not as
      # --arg, so the full ticket body never lands in jq's argv / ps output
      # or risks ARG_MAX. Everything else passed to jq is small + non-sensitive.
      SPAWN_CONFIG=$(printf '%s' "$PROMPT" | jq -n \
        --arg driver "$DRIVER" \
        --arg model "$MODEL" \
        --arg worktree "$WORKTREE_DIR" \
        --arg branch "$BRANCH" \
        --arg cmd "$CMD" \
        --arg NEW_ENV "$NEW_ENV" \
        --arg system_prompt "$SOUL_PROMPT" \
        --arg soul_id "$pick_driver.json.soul_id" \
        --arg soul_hash "$pick_driver.json.soul_hash" \
        --arg agent_fingerprint "$pick_driver.json.agent_fingerprint" \
        --arg repo_root "$CHITIN_REPO" \
        --arg default_branch "$DEFAULT_BRANCH" \
        --rawfile prompt /dev/stdin \
        --argjson args "$(echo "$ARGS_JSON" | jq -c '.')" \
        --argjson file_system_scope "$FS_SCOPE_JSON" \
        '{
          driver: $driver,
          model: $model,
          worktree: $worktree,
          branch: $branch,
          cmd: $cmd,
          prompt: $prompt,
          system_prompt: $system_prompt,
          soul_id: $soul_id,
          soul_hash: $soul_hash,
          agent_fingerprint: $agent_fingerprint,
          repo_root: $repo_root,
          default_branch: $default_branch,
          args: $args,
          file_system_scope: $file_system_scope,
          env: {
            "CHITIN_DRIVER": "clawta",
            "CHITIN_BUDGET_ENVELOPE": $NEW_ENV,
            "CHITIN_DISPATCH_DRIVER": $driver,
            "CHITIN_DISPATCH_MODEL": $model,
            "CHITIN_DISPATCH_SOUL_ID": $soul_id,
            "CHITIN_DISPATCH_SOUL_HASH": $soul_hash,
            "CHITIN_DISPATCH_AGENT_FINGERPRINT": $agent_fingerprint,
            "CHITIN_ACTIVE_SOUL": $soul_id,
            "CHITIN_AGENT_FINGERPRINT": $agent_fingerprint,
            "CHITIN_DEFAULT_BRANCH": $default_branch
          }
        }')

      # `|| true`: spawn_worker_subprocess.py exits non-zero on timeout /
      # session-error, but it always emits a structured JSON result on
      # stdout first. Without this, `set -e` aborts here before the result
      # is parsed and written to RESULT_FILE, so finalize_dispatch loses
      # the failure detail. The JSON-driven status checks below handle the
      # non-zero outcome explicitly.
      WORKER_RESULT=$(printf '%s' "$SPAWN_CONFIG" | python3 "$HOME/.openclaw/workflows/spawn_worker_subprocess.py" || true)
      WORKER_STATUS=$(echo "$WORKER_RESULT" | jq -r '.status // "unknown"')
      WORKER_RETURNCODE=$(echo "$WORKER_RESULT" | jq -r '.returncode // -1')
      WORKER_ERROR=$(echo "$WORKER_RESULT" | jq -r '.error // ""')

      echo "spawn_worker: completed with status=$WORKER_STATUS returncode=$WORKER_RETURNCODE" >&2

      # Store result for finalize_dispatch to retrieve
      RESULT_FILE="/tmp/dispatch-worker-result-${TICKET_ID}.json"
      printf '%s' "$WORKER_RESULT" > "$RESULT_FILE"

      # Return appropriate exit code based on worker result
      if [[ "$WORKER_STATUS" != "completed" && "$WORKER_STATUS" != "completed_no_commit" ]]; then
        echo "spawn_worker: worker failed with status=$WORKER_STATUS" >&2
        exit 1
      fi
      if [[ "$WORKER_RETURNCODE" -ne 0 ]]; then
        echo "spawn_worker: worker returned non-zero exit code: $WORKER_RETURNCODE" >&2
        exit 1
      fi
      BASH_SCRIPT
      )"
    stdin: $fetch_ticket.stdout

  # ─────────────────────────────────────────────────────────────────────
  # 9. Finalize — push branch, open PR, comment back to kanban + Discord.
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
      BOARD_SLUG='$resolve_board.json.board'
      DEFAULT_BRANCH=$(chitin-kernel board-config "$BOARD_SLUG" default_branch 2>/dev/null || echo "main")

      # Check worker result from spawn_worker step
      RESULT_FILE="/tmp/dispatch-worker-result-${ticket_id}.json"
      if [[ -f "$RESULT_FILE" ]]; then
        WORKER_RESULT=$(cat "$RESULT_FILE")
        WORKER_STATUS=$(echo "$WORKER_RESULT" | jq -r '.status // "unknown"')
        WORKER_ERROR=$(echo "$WORKER_RESULT" | jq -r '.error // ""')
        WORKER_EVENT_CHAIN_HASH=$(echo "$WORKER_RESULT" | jq -r '.event_chain_hash // ""')
        WORKER_SOUL_MISMATCH=$(echo "$WORKER_RESULT" | jq -r '.soul_hash_mismatch // false')
        WORKER_OBSERVED_SOUL_HASH=$(echo "$WORKER_RESULT" | jq -r '.observed_soul_hash // ""')

        if [[ "$WORKER_SOUL_MISMATCH" == "true" ]]; then
          hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta \
            "Audit flag: soul hash changed during run. dispatched=$pick_driver.json.soul_hash observed=$WORKER_OBSERVED_SOUL_HASH" 2>/dev/null || true
        fi

        case "$WORKER_STATUS" in
          timeout)
            MSG="🦞 ${ticket_id}: worker session timed out after 1 hour. No changes committed."
            hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
            kanban-flow crash ${ticket_id} "worker timeout" --author clawta --run-status timed_out --outcome timed_out --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
            openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
            exit 0
            ;;
          completed_no_commit)
            # Guard the report pipeline: if worker_failure_report.py is not
            # deployed or emits unparseable JSON, fall back to WORKER_ERROR
            # so we never post an empty comment or block with an empty reason.
            REPORT=$(printf '%s' "$WORKER_RESULT" | python3 "$HOME/.openclaw/workflows/worker_failure_report.py" --ticket-id ${ticket_id} 2>/dev/null || true)
            MSG=$(printf '%s' "$REPORT" | jq -r '.message // empty' 2>/dev/null || true)
            BLOCK_REASON=$(printf '%s' "$REPORT" | jq -r '.block_reason // empty' 2>/dev/null || true)
            [[ -n "$MSG" ]] || MSG="🦞 ${ticket_id}: worker exited with no commits. ${WORKER_ERROR:-(no error detail)}"
            [[ -n "$BLOCK_REASON" ]] || BLOCK_REASON="worker exited with no commits; ${WORKER_ERROR:-see dispatch log}"
            python3 "$HOME/.openclaw/workflows/clawta_decisions.py" mark-outcome \
              --ticket-id ${ticket_id} \
              --outcome failure \
              --failure-kind empty_branch >/dev/null 2>&1 || true
            hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
            kanban-flow crash ${ticket_id} "$BLOCK_REASON" --author clawta --run-status blocked --outcome blocked --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
            openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
            exit 0
            ;;
          failed)
            REPORT=$(printf '%s' "$WORKER_RESULT" | python3 "$HOME/.openclaw/workflows/worker_failure_report.py" --ticket-id ${ticket_id} 2>/dev/null || true)
            MSG=$(printf '%s' "$REPORT" | jq -r '.message // empty' 2>/dev/null || true)
            BLOCK_REASON=$(printf '%s' "$REPORT" | jq -r '.block_reason // empty' 2>/dev/null || true)
            [[ -n "$MSG" ]] || MSG="🦞 ${ticket_id}: worker session failed. ${WORKER_ERROR:-(no error detail)}"
            [[ -n "$BLOCK_REASON" ]] || BLOCK_REASON="worker session failed; ${WORKER_ERROR:-see dispatch log}"
            hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
            kanban-flow crash ${ticket_id} "$BLOCK_REASON" --author clawta --run-status failed --outcome failed --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
            openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
            exit 0
            ;;
          completed)
            # Worker succeeded, proceed to PR logic below
            ;;
          *)
            MSG="🦞 ${ticket_id}: worker returned unknown status: $WORKER_STATUS"
            hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
            kanban-flow crash ${ticket_id} "unknown worker status" --author clawta --run-status failed --outcome failed --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
            exit 0
            ;;
        esac
      fi

      # Locate the most recent worktree for this ticket. Pattern is
      # ~/.cache/chitin/swarm-worktrees/<driver>-<ticket_id>/ — the
      # spawn_worker step creates it. Newest match wins if the worker re-tried.
      WORKTREE=$(ls -td "$HOME"/.cache/chitin/swarm-worktrees/*-${ticket_id}*/ 2>/dev/null | head -1)
      if [[ -z "$WORKTREE" || ! -d "$WORKTREE" ]]; then
        MSG="🦞 spawn_worker completed but no worktree found for ${ticket_id} — worker may have failed before committing."
        hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        # Slice D: failure path — block the ticket so it isn't silently lost.
        kanban-flow crash ${ticket_id} "worker completed but produced no worktree" --author clawta --run-status failed --outcome failed 2>/dev/null || true
        openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
        exit 0
      fi
      if ! cd "$WORKTREE"; then
        MSG="🦞 ${ticket_id}: worker worktree exists but could not be entered: $WORKTREE"
        hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        kanban-flow crash ${ticket_id} "worker worktree could not be entered" --author clawta --run-status failed --outcome failed 2>/dev/null || true
        openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
        exit 0
      fi
      BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
      if [[ -z "$BRANCH" || "$BRANCH" == "$DEFAULT_BRANCH" || "$BRANCH" == "HEAD" ]]; then
        MSG="🦞 ${ticket_id}: worker finished but no feature branch checked out (HEAD=$BRANCH, default_branch=$DEFAULT_BRANCH). No PR opened."
        hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        # Slice D: failure path — block, don't leave in_progress with no PR.
        kanban-flow crash ${ticket_id} "no feature branch checked out at end of run (HEAD=$BRANCH)" --author clawta --run-status failed --outcome failed 2>/dev/null || true
        openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
        exit 0
      fi
      # Defensive invariant: the worker's branch MUST have at least one commit
      # not present in origin/$DEFAULT_BRANCH before we push. Without this, a
      # worker that exits with WORKER_STATUS=completed but never actually
      # committed (lying or buggy worker) produces a zombie branch on the
      # remote (HEAD == origin/$DEFAULT_BRANCH HEAD) and `gh pr create` then
      # fails with "no commits between …", silently losing the ticket.
      # This gate runs regardless of WORKER_STATUS — git is the source of truth.
      git fetch origin "$DEFAULT_BRANCH" --quiet 2>/dev/null || true
      COMMITS_AHEAD=$(git rev-list --count "origin/$DEFAULT_BRANCH..HEAD" 2>/dev/null || echo 0)
      if [[ "${COMMITS_AHEAD:-0}" -eq 0 ]]; then
        MSG="🦞 ${ticket_id}: worker reported status=$WORKER_STATUS but branch $BRANCH has 0 commits ahead of $DEFAULT_BRANCH. Refusing to push an empty branch. Worktree=$WORKTREE."
        hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
        python3 "$HOME/.openclaw/workflows/clawta_decisions.py" mark-outcome \
          --ticket-id ${ticket_id} \
          --outcome failure \
          --failure-kind empty_branch >/dev/null 2>&1 || true
        kanban-flow crash ${ticket_id} "worker produced 0 commits on branch $BRANCH ahead of $DEFAULT_BRANCH (worktree=$WORKTREE, worker_status=$WORKER_STATUS)" --author clawta --run-status blocked --outcome blocked --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
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
        # gh pr create writes the PR URL on stdout when it succeeds, but
        # the useful diagnostics are often on stderr when it fails. Keep
        # both streams so the block comment and dispatch log preserve the
        # actual gh-side reason.
        GH_PR_STDOUT_FILE=$(mktemp)
        GH_PR_STDERR_FILE=$(mktemp)
        GH_PR_EXIT=0
        if gh pr create \
          --base "$DEFAULT_BRANCH" \
          --title "$(git log -1 --pretty=%s)" \
          --body "Closes ticket ${ticket_id} (dispatched via clawta to $DRIVER). Auto-opened by kanban-dispatch.lobster finalize step. Branch $BRANCH." \
          >"$GH_PR_STDOUT_FILE" 2>"$GH_PR_STDERR_FILE"; then
          :
        else
          GH_PR_EXIT=$?
        fi
        GH_PR_STDOUT=$(cat "$GH_PR_STDOUT_FILE" 2>/dev/null || true)
        GH_PR_STDERR=$(cat "$GH_PR_STDERR_FILE" 2>/dev/null || true)
        rm -f "$GH_PR_STDOUT_FILE" "$GH_PR_STDERR_FILE"
        PR_URL=$(printf '%s' "$GH_PR_STDOUT" | grep -oE 'https://github.com/[^ ]+/pull/[0-9]+' | head -1 || echo "")
      fi
      if [[ -z "$PR_URL" ]]; then
        REPORT=$(jq -nc \
          --arg stdout "${GH_PR_STDOUT:-}" \
          --arg stderr "${GH_PR_STDERR:-}" \
          --argjson exit_code "${GH_PR_EXIT:-0}" \
          '{stdout:$stdout, stderr:$stderr, exit_code:$exit_code}' \
          | python3 "$HOME/.openclaw/workflows/pr_failure_report.py" --ticket-id ${ticket_id} --branch "$BRANCH" 2>/dev/null || true)
        MSG=$(printf '%s' "$REPORT" | jq -r '.message // empty' 2>/dev/null || true)
        BLOCK_REASON=$(printf '%s' "$REPORT" | jq -r '.block_reason // empty' 2>/dev/null || true)
        [[ -n "$MSG" ]] || MSG="🦞 ${ticket_id}: branch $BRANCH pushed, but gh pr create failed. Manual PR open needed. stdout=${GH_PR_STDOUT:-unknown} stderr=${GH_PR_STDERR:-unknown} exit_code=${GH_PR_EXIT:-unknown}"
        [[ -n "$BLOCK_REASON" ]] || BLOCK_REASON="branch $BRANCH pushed but gh pr create failed: stdout=${GH_PR_STDOUT:-unknown} stderr=${GH_PR_STDERR:-unknown} exit_code=${GH_PR_EXIT:-unknown}"
        python3 "$HOME/.openclaw/workflows/clawta_decisions.py" mark-outcome \
          --ticket-id ${ticket_id} \
          --outcome failure \
          --failure-kind gh_pr_create_fail >/dev/null 2>&1 || true
        # Slice D: PR failed — surface the detail (#650) and finalize the
        # task_run via `crash` so the run ledger keeps the chain link (#646).
        printf '%s\n' "$BLOCK_REASON"
        printf '%s\n' "$MSG"
        kanban-flow crash ${ticket_id} "$BLOCK_REASON" --author clawta --run-status failed --outcome failed --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
      else
        python3 "$HOME/.openclaw/workflows/clawta_decisions.py" mark-outcome \
          --ticket-id ${ticket_id} \
          --outcome success >/dev/null 2>&1 || true
        MSG="🦞 ${ticket_id}: PR opened; ticket remains in_progress for review/merge. Branch: $BRANCH. PR: $PR_URL"
        # PR opened — record the URL on the ticket. Ticket stays in
        # in_progress (Hermes UI has no code_review column; the PR's
        # GitHub state is the review-phase truth).
        kanban-flow pr ${ticket_id} "$PR_URL" --author clawta --event-chain-hash "$WORKER_EVENT_CHAIN_HASH" 2>/dev/null || true
      fi
      hermes kanban --board $resolve_board.json.board comment ${ticket_id} --author clawta "$MSG" 2>/dev/null || true
      # Broadcast to Clawta's default channel (operator-facing log).
      openclaw message send --channel discord --account default --text "$MSG" 2>/dev/null || true
      # Reply back to Hermes in #ares with @-mention so the peer-comm
      # loop closes visibly. Hermes (with DISCORD_ALLOW_BOTS=mentions)
      # picks up the @-mention and can summarize for the operator.
      # Channel id 1503438297597350062 is #ares; user 1503231892865024030
      # is the Ares (Hermes) bot. Best-effort: failure never blocks.
      openclaw message send --channel discord --account default \
        --target channel:1503438297597350062 \
        --text "<@1503231892865024030> $MSG" 2>/dev/null || true
      # Slice 5: post-PR judge writes ELO. Fire only when a real PR
      # url exists AND driver/model are populated. Background + short
      # timeout — judge results are observable later via swarm-elo CLI;
      # we don't block the dispatch return on its completion.
      DRIVER='$pick_driver.json.driver'
      ROLE_DEFAULT="${ROLE:-programmer}"
      TICKET_JSON=$(hermes kanban --board $resolve_board.json.board show ${ticket_id} --json 2>/dev/null || true)
      ROLE=$(printf '%s' "$TICKET_JSON" | python3 "$HOME/.openclaw/workflows/ticket_metadata.py" "$ROLE_DEFAULT" 2>/dev/null || true)
      if [[ -z "$ROLE" ]]; then
        ROLE="$ROLE_DEFAULT"
      fi
      MODEL='$pick_driver.json.model'
      if [[ -n "$PR_URL" && -n "$DRIVER" && -n "$MODEL" && "$DRIVER" != "unassigned" ]]; then
        (timeout 180 python3 "$HOME/.openclaw/workflows/judge.py" \
          --ticket ${ticket_id} \
          --pr-url "$PR_URL" \
          --driver "$DRIVER" \
          --model "$MODEL" \
          --soul-id "$pick_driver.json.soul_id" \
          --soul-hash "$pick_driver.json.soul_hash" \
          --role "$ROLE" \
          >> "$HOME/.openclaw/logs/judge.log" 2>&1 || true) &
        disown
      fi
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
