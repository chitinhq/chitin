# SP-1 Dogfood Gate — deferred

**Date:** 2026-04-21
**Status:** Deferred.
**Workstream:** `docs/superpowers/specs/2026-04-20-otel-genai-ingest-workstream-design.md`, SP-1
**Plan:** `docs/superpowers/plans/2026-04-20-sp1-openclaw-dialect-translator.md`, Task 8

The SP-1 plan's Task 8 dogfood gate attempted to capture one real
`openclaw.model.usage` span end-to-end. It deferred because the
environmental blocker SP-0 identified is still present and resolving
it requires actions outside SP-1's scope.

## What SP-0 already established

Per `docs/observations/2026-04-20-openclaw-otel-capture.md` §Runtime
capture:

- The openclaw embedded-agent runtime reads the model from a persisted
  agent profile (`profile=sha256:9c018ec112cf`) which references
  `qwen2.5-coder:7b` and does **not** honour
  `agents.defaults.model.primary`.
- `qwen2.5-coder:7b` is not pulled locally. `qwen2.5:0.5b` is the only
  local model, but the embedded agent ignores it because the profile
  hard-codes the 7B coder tag.
- A failed run produces a `stage=assistant decision=surface_error`
  log line AND zero `openclaw.model.usage` spans — the plugin only
  instruments successful model events (line 53651+ in the plugin
  bundle).
- "Further config surgery was not pursued within SP-0's scope."

## What ran in SP-1 Task 8

- **Step 1 (state check, read-only):**
  - `~/.openclaw/openclaw.json` already has
    `diagnostics.enabled=true`, `diagnostics.otel.enabled=false`
    (SP-0's Option 2 revert). Re-enabling the exporter is a one-line
    edit, not the blocker.
  - `systemctl --user is-active openclaw-gateway` → `active`.
  - `/tmp/otel-capture/receiver.py` is still present from SP-0; the
    capture tooling works.
  - `ollama list` shows `qwen2.5:0.5b` and `qwen3-coder:480b-cloud`.
    `qwen2.5-coder:7b` is still not pulled.
  - `~/.openclaw/agents/main/agent/models.json` is readable plaintext
    JSON: the `ollama` provider entry contains exactly one model id,
    `qwen2.5-coder:7b`. The embedded agent's model enumeration comes
    from this file, not from `openclaw.json`.
- **Steps 2–5 (deferred):** not attempted — the unblock paths all
  require either a multi-GB model pull or a schema-unclear edit to
  `models.json` + possibly `sessions.json` + possibly a cache invalidation
  (`profile=sha256:9c018ec112cf` is a content-hashed pointer, so
  editing `models.json` may not invalidate the cached profile).

## What would unblock this

Two concrete paths, either of which is sufficient on its own:

1. **Pull `qwen2.5-coder:7b`** via `ollama pull qwen2.5-coder:7b`
   (~4.7GB). The embedded agent's persisted profile already resolves
   this tag; pulling it is the smallest-surface fix. Dogfood gate can
   then rerun with no config edits at all. This is the path SP-1's
   planner recommends for the retry.
2. **Agent profile surgery** on
   `~/.openclaw/agents/main/agent/models.json` to repoint the
   `ollama` provider's sole entry to `qwen2.5:0.5b`, plus whatever
   invalidates the `sha256:9c018ec112cf` profile cache. This path is
   cheaper in bytes but unknown in schema — SP-0 explicitly did not
   pursue it and this gate follows suit.

Either path, once taken, is a single Task 8 rerun: re-enable
`diagnostics.otel.enabled`, restore the systemd drop-in, start the
receiver, run `openclaw agent --agent main -m "say hi in one word"`,
capture the first non-empty `v1/traces-*.pb`, and ingest it through
`chitin-kernel ingest-otel --dialect openclaw`.

## Impact on SP-1

None on correctness. The translator is fully tested at the unit,
integration, and CLI levels against the SP-0-source-derived
synthesized fixture (`synthesized-model-usage.pb`). Every attribute
key, span name, and required/optional rule in the mapping table was
derived from static inspection of the installed plugin bundle, so the
synthesized fixture and the real capture are wire-identical up to
attribute values; the translator has no dependency on real-run
content.

The real-capture artifact (`real-model-usage.pb`) is not committed.
SP-2 or a follow-up checkpoint will retry once either unblocker above
lands.

## Box state at deferral time

- `diagnostics.enabled=true`, `diagnostics.otel.enabled=false` —
  unchanged from SP-0's Option 2 revert; no network traffic to
  `:4318`, plugin stays loaded so the re-enable path is a one-line
  JSON edit.
- No systemd drop-in present
  (`~/.config/systemd/user/openclaw-gateway.service.d/otel-capture.conf`
  does not exist).
- `/tmp/otel-capture/receiver.py` still present from SP-0; reusable.
- No openclaw config files modified by SP-1; no gateway restarts; no
  ollama pulls.
