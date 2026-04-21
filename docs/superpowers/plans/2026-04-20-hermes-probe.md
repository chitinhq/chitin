# Hermes Probe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Note — this plan is operational, not software.** The probe runs over a calendar week with daily touch-points. Most tasks are user-driven (install, configure, use, observe, document). Agentic executors should pause for user input rather than synthesize daily log entries.

**Goal:** Execute the 1-week Hermes probe defined in `docs/superpowers/specs/2026-04-20-hermes-probe-design.md`, producing a committed observations doc with verdict + evidence.

**Architecture:** Install Hermes via `ollama launch hermes`, configure Telegram gateway, run on `qwen3.5:cloud` days 1-3, swap to local `qwen3.5:27b` on the 3090 days 4-7, write daily log entries, write final observations doc at day-7 review gate.

**Tech Stack:** Ollama (existing local install + Cloud subscription), Hermes (to be installed via `ollama launch hermes`), Telegram bot (new), markdown.

---

## File Structure

Files this plan creates or modifies:

- **Create:** `docs/observations/<DATE>-hermes-probe-log.md` — daily scratch log entries during the week
- **Create:** `docs/observations/<DATE>-hermes-probe.md` — final observations doc written at day-7 review gate
- **Create (conditional):** `docs/observations/<DATE>-hermes-otel-capture.md` — only if Hermes has an OTEL surface; a captured payload + short characterization
- **Modify (conditional):** `~/.claude/projects/-home-red-workspace-chitin/memory/project_strategic_roadmap.md` — post-probe, reflect verdict

`<DATE>` placeholder resolves to the actual YYYY-MM-DD when the task runs. Task 3 instructs setting the variable once; subsequent tasks use the same date (the probe's start date, not each day's date, because the filename anchors to the probe not the day).

No source code is written. No tests are written. No build system is touched.

---

## Task 1: Install Hermes + verify round-trip (Day 1, part 1)

**Files:** None yet — this task produces a running Hermes install, not a file.

**Prerequisite check:**

- [ ] **Step 1.1: Confirm Ollama is running locally**

```bash
curl -s http://127.0.0.1:11434/api/tags | head -c 200
```

Expected: A JSON response listing installed models (not an error). If Ollama is not running: `ollama serve &` in a separate terminal, then retry.

- [ ] **Step 1.2: Confirm Ollama Cloud is authenticated**

```bash
ollama list | grep ':cloud' | head -5
```

Expected: At least one `*:cloud` model visible. If not, authenticate via `ollama signin` (interactive; paste `! ollama signin` in the prompt if running inside a Claude Code session).

**Install Hermes:**

- [ ] **Step 1.3: Launch Hermes via Ollama**

```bash
ollama launch hermes
```

Expected: Ollama's wizard runs — installs Hermes if not present, prompts for model selection, prompts for messaging gateway. Walk through the wizard:
- **Model selection:** choose `qwen3.5:cloud` (this is the cloud-phase primary per the spec).
- **Gateway:** choose Telegram.
- **Endpoint:** confirm `http://127.0.0.1:11434/v1` (default).

If the wizard errors on Hermes install: run `curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh | bash` manually, then re-run `ollama launch hermes`.

- [ ] **Step 1.4: Verify Hermes version**

```bash
hermes --version
```

Expected: A version string. Record it for the probe log (Step 3.2).

**Configure Telegram bot (if the Ollama wizard didn't finish it):**

- [ ] **Step 1.5: Create a Telegram bot if one doesn't exist**

Open Telegram, message `@BotFather`, send `/newbot`, give it a name (e.g. `hermes_probe_bot`), copy the HTTP API token.

- [ ] **Step 1.6: Wire the bot into Hermes**

```bash
hermes gateway setup
```

Expected: Interactive prompt. Select Telegram, paste the bot token from Step 1.5. Wizard reports "Gateway connected."

**Verify round-trip:**

- [ ] **Step 1.7: Send a test message from Telegram**

Open Telegram, find your bot, send: `hi — can you hear me?`

Expected: Hermes replies within ~10 seconds with a coherent response. If it doesn't reply: check `hermes status` (or equivalent) for gateway state, check the Ollama server is still running, check bot token is right.

- [ ] **Step 1.8: Record round-trip success**

No commit yet — the probe log is seeded in Task 3. Note the success mentally (or in a terminal scratch pad): install version, model `qwen3.5:cloud`, Telegram bot name, first message timestamp, reply latency.

---

## Task 2: OTEL surface investigation (Day 1, part 2; ≤30 min hard cap)

**Files:** Possibly create `docs/observations/<DATE>-hermes-otel-capture.md` (conditional).

**Set a 30-minute timer before starting this task.** If 30 min elapses with no clear OTEL surface confirmed, stop — one-line finding in the probe log (Task 3), move on. This cap is in the spec.

- [ ] **Step 2.1: Scan Hermes help and setup for telemetry/export flags**

```bash
hermes --help 2>&1 | grep -iE 'otel|opentelemetry|telemetr|export|trace|observ' | head -20
hermes setup --help 2>&1 | grep -iE 'otel|opentelemetry|telemetr|export|trace|observ' | head -20
```

Expected: Either matches (investigation continues) or empty (surface likely absent, proceed to Step 2.4).

- [ ] **Step 2.2: Grep the Hermes install directory for OTEL references**

```bash
which hermes
# Take the path from above, then:
HERMES_DIR="$(dirname "$(readlink -f "$(which hermes)")")/.."
grep -rIE 'otel|opentelemetry|gen_ai' "$HERMES_DIR" 2>/dev/null | grep -v node_modules | head -30
```

Expected: Either matches (note the files, inspect them in Step 2.3) or empty (surface likely absent, proceed to Step 2.4).

- [ ] **Step 2.3: Inspect Hermes config file for telemetry section**

```bash
ls -la ~/.hermes/ ~/.config/hermes/ 2>/dev/null
# For each config file found:
cat ~/.hermes/config.* 2>/dev/null | grep -iE 'otel|opentelemetry|telemetr|export|trace'
```

If a telemetry/OTEL config section exists: enable export to a local file (exact config knob depends on what's found — typical pattern is `{telemetry: {enabled: true, exporter: "file", path: "/tmp/hermes-otel.jsonl"}}` or similar in the config schema). If unclear how to configure, time-box to 10 more minutes of doc search; otherwise proceed to Step 2.4.

- [ ] **Step 2.4a: If OTEL surface found and enabled — capture a payload**

Send 2–3 varied messages to Hermes via Telegram (a question, a task request, a code snippet). Wait for replies. Then:

```bash
test -s /tmp/hermes-otel.jsonl && head -1 /tmp/hermes-otel.jsonl | python3 -m json.tool | head -60
```

Expected: A valid OTEL span or batch. If the output is empty or not JSON, continue investigation OR accept that the surface exists but produces nothing usable and record that as the finding.

- [ ] **Step 2.4b: If OTEL surface found — commit the capture + characterization**

```bash
DATE=$(date +%Y-%m-%d)
cp /tmp/hermes-otel.jsonl docs/observations/${DATE}-hermes-otel-capture.jsonl
```

Write `docs/observations/${DATE}-hermes-otel-capture.md` with this structure (replace bracketed content):

```markdown
# Hermes OTEL Capture — <DATE>

Captured during the Hermes probe's day-1 investigation (spec: `docs/superpowers/specs/2026-04-20-hermes-probe-design.md`).

## Source

- Hermes version: <from Step 1.4>
- Model: qwen3.5:cloud
- Trigger: 2–3 messages sent via Telegram gateway during day-1 setup.
- Captured at: `docs/observations/<DATE>-hermes-otel-capture.jsonl`

## Attribute keys observed

<list the top-level attribute keys seen in the capture — grep span attributes>

## Semconv compliance check

- `gen_ai.*` attributes present? <yes/no, with examples>
- `service.name` present? <yes/no, value>
- Span naming convention: <OTEL semconv / bespoke / other>

## One-line verdict

<one sentence: "Hermes emits gen_ai-compliant OTEL" / "Hermes emits OTEL but not gen_ai semconv" / "Hermes emits OTEL with partial gen_ai coverage" / etc.>
```

Commit:

```bash
git add docs/observations/${DATE}-hermes-otel-capture.md docs/observations/${DATE}-hermes-otel-capture.jsonl
git commit -m "observations: Hermes OTEL capture — day 1 of probe"
```

- [ ] **Step 2.4c: If OTEL surface NOT found — record one-line finding only**

No commit yet; the finding lands in the probe log at Task 3. Note the one-line finding mentally: "Hermes has no OTEL surface in its help / install / config as of `<version>`."

---

## Task 3: Seed the probe log (Day 1, part 3)

**Files:**
- Create: `docs/observations/<DATE>-hermes-probe-log.md`

- [ ] **Step 3.1: Set the probe start date variable**

```bash
export PROBE_DATE=$(date +%Y-%m-%d)
echo "$PROBE_DATE"
```

Record `$PROBE_DATE` somewhere (paste it into a scratch note). **Every subsequent log/doc task uses this same date**, not the current day's date — the filename anchors to the probe's start, not each day.

- [ ] **Step 3.2: Create the probe log with day-1 entry**

Write `docs/observations/${PROBE_DATE}-hermes-probe-log.md`:

```markdown
# Hermes Probe Log

**Probe spec:** `docs/superpowers/specs/2026-04-20-hermes-probe-design.md`
**Started:** <PROBE_DATE>

---

## Day 1 — <PROBE_DATE>

- Install: hermes <version from Step 1.4>
- Model (cloud phase): qwen3.5:cloud
- Gateway: Telegram (bot: `<bot name from Step 1.5>`)
- Round-trip verified: yes, <latency> on test message.

**OTEL surface finding:** <one-line finding from Task 2 — either "captured, see docs/observations/<PROBE_DATE>-hermes-otel-capture.md" OR "not found: <brief reason>">

**habit:** N  (day 1 is setup; unprompted-message habit not yet a signal)
```

- [ ] **Step 3.3: Commit the probe log**

```bash
git add docs/observations/${PROBE_DATE}-hermes-probe-log.md
git commit -m "observations: Hermes probe log — day 1 seeded"
```

---

## Task 4: Daily usage protocol (Days 2–3, cloud phase; Days 5–7, local phase)

**Files:**
- Modify: `docs/observations/<PROBE_DATE>-hermes-probe-log.md` (one appended section per day)

**This task is applied once per day on Days 2, 3, 5, 6, 7.** Day 4 is the model swap (Task 5). The protocol is the same on cloud-phase days (2-3) and local-phase days (5-7) — only the daily entry's phase marker and comparison field differ.

- [ ] **Step 4.1: During the day — use Hermes as async teammate for real work**

Whenever something async comes up that Hermes could handle (research task, draft, chase a link, summarize a doc, etc.), message it via Telegram. No synthetic tasks. If nothing comes up for 2 consecutive days, see kill criterion #3 in the spec (§ "Kill criteria").

Watch for:
- **Multi-turn memory:** does Hermes reference earlier conversation context within the same day and across days?
- **Skill behavior:** does Hermes auto-create a skill for something you asked for more than once?
- **Latency / quality:** note if either is materially bad.

- [ ] **Step 4.2: At end of day — append a daily entry**

Append to `docs/observations/${PROBE_DATE}-hermes-probe-log.md`:

For **cloud-phase days (Day 2, Day 3)**:

```markdown
## Day <N> — <YYYY-MM-DD>  (cloud phase)

**Offloaded today:** <what you messaged Hermes about, 1–3 lines>
**Came back useful:** <yes/partial/no, brief note>
**Memory observed:** <yes/no/not-tested today — did Hermes remember anything from an earlier session?>
**Skill auto-created:** <yes/no/not-observed — anything that looked like a new skill persisting?>
**habit:** <Y/N — did I message Hermes unprompted for real work today?>
```

For **local-phase days (Day 5, Day 6, Day 7)**:

```markdown
## Day <N> — <YYYY-MM-DD>  (local phase)

**Offloaded today:** <what you messaged Hermes about>
**Came back useful:** <yes/partial/no, brief note>
**Memory observed:** <yes/no/not-tested>
**Skill auto-created:** <yes/no/not-observed>
**Materially different from cloud:** <yes/no + one line — latency, quality, failed where cloud succeeded, etc.>
**habit:** <Y/N>
```

- [ ] **Step 4.3: Commit the day's entry**

```bash
git add docs/observations/${PROBE_DATE}-hermes-probe-log.md
git commit -m "observations: Hermes probe log — day <N>"
```

- [ ] **Step 4.4: Check kill criteria**

Before ending the day, check:
- Messaging gateway broken >24h and unfixable? → hard-kill (skip to Task 7 with kill reason).
- End of day 2 with zero unprompted messages to Hermes? → hard-kill (kill criterion #3).
- End of day 2 with zero tasks completed usefully on cloud? → hard-kill (kill criterion #2).

If none of the above fire, continue to the next day.

---

## Task 5: Model swap — cloud → local (Day 4)

**Files:** None.

- [ ] **Step 5.1: Verify 3090 is available**

```bash
nvidia-smi --query-gpu=name,memory.total,memory.used,memory.free --format=csv
```

Expected: Roughly 24 GB total on the 3090 with ≥20 GB free. If significantly less free: stop other GPU-consuming processes before continuing.

- [ ] **Step 5.2: Pull the local model**

```bash
ollama pull qwen3.5:27b
```

Expected: Model downloads (several GB). Note approximate download size.

- [ ] **Step 5.3: Reconfigure Hermes to use local model**

Via Hermes's setup wizard or config file (exact path depends on Hermes's layout — likely `~/.hermes/config.*` from Task 2):

```bash
hermes setup
# Select: change primary model
# New primary: qwen3.5:27b
```

If the wizard doesn't have a "change primary model" path, edit the config file directly — replace `qwen3.5:cloud` with `qwen3.5:27b` in the model field.

- [ ] **Step 5.4: Smoke-test memory persistence across the swap**

Via Telegram, send: `what did we talk about on day 1?`

Expected: Hermes references something from day-1 conversation. Whether this works is the first data point of the local phase.

- [ ] **Step 5.5: Smoke-test model responsiveness**

Via Telegram, send a modest task (a paragraph summarization of something non-trivial). Wait up to 60 seconds.

Expected: A coherent reply. Fallback logic:
- If OOM or crash (check Ollama logs via `journalctl --user -u ollama` or wherever your Ollama logs go, OR `ollama ps`): run `ollama pull gemma4:26b` and redo Step 5.3 with `gemma4:26b` in place of `qwen3.5:27b`. Record the fall-back as a finding in Step 5.6.
- If unusably slow (>60s with no output, or output that takes >2 min per message): same as OOM — fall back to `gemma4:26b`.
- If `gemma4:26b` also fails both checks: record "local stack could not carry either candidate model" as a finding, revert primary to `qwen3.5:cloud`, continue the probe on cloud for days 5-7 with the finding documented.

- [ ] **Step 5.6: Append day-4 entry + commit**

Append to `docs/observations/${PROBE_DATE}-hermes-probe-log.md`:

```markdown
## Day 4 — <YYYY-MM-DD>  (swap)

- Local model: qwen3.5:27b (or gemma4:26b fallback / or reverted to cloud — record actual)
- VRAM usage (nvidia-smi after smoke test): <X GB of 24 GB>
- Memory-persistence smoke test: <pass/fail + brief>
- Responsiveness smoke test: <pass/fail + brief, include rough latency>
- Fallback taken: <none / gemma4:26b / reverted to cloud — with reason>
- **habit:** <Y/N>
```

Commit:

```bash
git add docs/observations/${PROBE_DATE}-hermes-probe-log.md
git commit -m "observations: Hermes probe log — day 4 model swap"
```

---

## Task 6: Day 7 review gate — write observations doc

**Files:**
- Create: `docs/observations/<PROBE_DATE>-hermes-probe.md`

This task fires on Day 7 (or earlier if a hard-kill criterion fired — in that case, the observations doc records the kill reason in place of the normal verdict).

- [ ] **Step 6.1: Compute habit count**

From the probe log, count the `habit: Y` entries across days 2-7 (six possible days; day 1 was structurally `N` setup).

- ≥3 Y → habit forming → evidence supports "yes"
- ≤1 Y → no habit → evidence supports "no"
- 2 Y → ambiguous; checklist evidence breaks tie; if still ambiguous, default to "extend"

Record the count and the derived habit signal.

- [ ] **Step 6.2: Compile checklist evidence**

From the probe log, assemble evidence for each property:

| Property | Status | Evidence |
|---|---|---|
| Multi-turn memory across sessions | observed / not observed | <brief, cite which day(s)> |
| Auto-created skill persisting | observed / not observed | <brief, cite which day(s)> |
| Local model carrying workload | yes / no / partial / not-tested | <from days 4-7; cite model actually used> |
| OTEL surface | emits gen_ai / emits bespoke OTEL / no surface | <cite capture file or Task 2 finding> |

- [ ] **Step 6.3: Decide verdict**

Combine habit signal (§ 6.1) with checklist evidence (§ 6.2):

- Habit "yes" + checklist has ≥2 observed properties → **close, verdict yes**
- Habit "no" + checklist has ≤1 observed property → **close, verdict no**
- Mixed signals OR habit ambiguous → **extend** (requires naming the delta question the extension will answer)
- Any hard-kill fired during the probe → **close, verdict (kill reason)** with the kill's probe-log entry as the primary finding

- [ ] **Step 6.4: Write the observations doc**

Create `docs/observations/${PROBE_DATE}-hermes-probe.md`:

```markdown
# Hermes Probe — Observations

**Probe spec:** `docs/superpowers/specs/2026-04-20-hermes-probe-design.md`
**Probe log:** `docs/observations/<PROBE_DATE>-hermes-probe-log.md`
**OTEL capture:** `docs/observations/<PROBE_DATE>-hermes-otel-capture.md` (if applicable)
**Duration:** <actual days — 7 for full run, fewer if hard-killed, more if extended>

---

## Verdict

**<yes / no / extend / killed-<reason>>**

<one paragraph of reasoning, drawing on the habit signal and checklist>

## Habit log

| Day | Phase | habit |
|---|---|---|
| 1 | setup | N |
| 2 | cloud | <Y/N> |
| 3 | cloud | <Y/N> |
| 4 | swap | <Y/N> |
| 5 | local | <Y/N> |
| 6 | local | <Y/N> |
| 7 | local | <Y/N> |

**Total Y days (days 2-7):** <count> / 6
**Signal:** <habit forming / no habit / ambiguous>

## Checklist evidence

<the table from Step 6.2>

## Findings

<3-7 bullets: what surprised you, positive or negative. Include:>
- <one bullet on whether the async-teammate UX felt right>
- <one bullet on the local-vs-cloud comparison>
- <one bullet on OTEL surface character (connects to OTEL workstream context)>
- <any other durable observations>

## Next step (if applicable)

- **If verdict = yes:** trigger follow-up brainstorm "minimum chitin-governs-Hermes setup" — the OTEL finding from this probe decides adapter shape.
- **If verdict = no:** re-open local-stack question in a separate brainstorm; the 3090 remains underexercised.
- **If verdict = extend:** named duration (max 1 week), named delta question, explicit re-review. If second review is still ambiguous, default to no.
- **If verdict = killed:** document the killed cause and whether re-probing is warranted once the blocker clears.
```

- [ ] **Step 6.5: Commit the observations doc**

```bash
git add docs/observations/${PROBE_DATE}-hermes-probe.md
git commit -m "observations: Hermes probe — verdict <yes/no/extend/killed>"
```

---

## Task 7: Update strategic-roadmap memory

**Files:**
- Modify: `~/.claude/projects/-home-red-workspace-chitin/memory/project_strategic_roadmap.md`

This is OUTSIDE the chitin repo (it's in the user's Claude Code memory directory) — no git commit in chitin, just a file edit.

- [ ] **Step 7.1: Read the current memory**

```bash
cat ~/.claude/projects/-home-red-workspace-chitin/memory/project_strategic_roadmap.md
```

Note the current "Local-first is part of the swarm-member primitive" paragraph — the last sentence mentions Hermes as "pending a cheap-probe run."

- [ ] **Step 7.2: Edit the memory based on verdict**

**If verdict = yes:**

Replace the "pending a cheap-probe run" sentence with a sentence reflecting the verdict, e.g.:
> Hermes on Ollama (`ollama launch hermes`) validated as swarm-node primitive via probe <PROBE_DATE> — see `docs/observations/<PROBE_DATE>-hermes-probe.md`. Added to durable stack.

Add a new bullet/line to the strategic arc if the swarm step warrants specific reference to Hermes as the node.

**If verdict = no:**

Replace the "pending a cheap-probe run" sentence with:
> Hermes on Ollama probed <PROBE_DATE>, not adopted — see `docs/observations/<PROBE_DATE>-hermes-probe.md` for reason. Local-stack question remains open; separate brainstorm needed for what exercises the 3090.

**If verdict = extend:**

No edit yet. Memory update happens after the extension's re-review.

**If verdict = killed:**

Replace the "pending a cheap-probe run" sentence with:
> Hermes on Ollama probe <PROBE_DATE> did not complete — killed because <reason>. Re-probe when <unblocking condition>. See `docs/observations/<PROBE_DATE>-hermes-probe.md`.

- [ ] **Step 7.3: Confirm the memory update is consistent with its description field**

The memory file's frontmatter `description` currently mentions "north star is autonomous swarm + self-building product." That stays correct regardless of verdict — no edit needed there. Only the body paragraph about the probe status changes.

---

## Self-review

**Spec coverage:**

- § "Scope / In scope" — all items covered by Tasks 1-7:
  - Install Hermes → Task 1
  - Messaging gateway → Task 1 (Steps 1.5, 1.6)
  - qwen3.5:cloud days 1-3 → Task 1 + Task 4 cloud-phase protocol
  - Swap to qwen3.5:27b local days 4-7 → Task 5 + Task 4 local-phase protocol
  - Day-1 OTEL investigation with 30-min cap → Task 2
  - Daily probe-log entries → Task 4
  - Observations doc at review gate → Task 6
- § "Operational plan / Day 1-7" — all phases covered
- § "Kill criteria" — checked in Task 4 Step 4.4; exit path wired to Task 6 Step 6.3
- § "Exit deliverables" — three deliverables covered: probe doc (Task 6), OTEL capture (Task 2.4b, conditional), memory update (Task 7)
- § "Verdict-triggered follow-ups" — named in Task 6 Step 6.4's "Next step" section of the observations doc template

**Placeholder scan:**

- Every `<PROBE_DATE>` and `<DATE>` is explicitly defined (set once in Task 3 Step 3.1, referenced thereafter).
- Every `<N>` in daily entries is the day number (2, 3, 4, 5, 6, 7).
- Every `<Y/N>`, `<yes/no/partial>`, `<...>` inside the templated markdown is user-fill content at probe time, which is the correct shape for a runbook template (the engineer fills in the values when the day runs).
- No literal `TBD`, `TODO`, `fill in details`, or "similar to task N" text.

**Consistency:**

- Model names match between spec (`qwen3.5:cloud`, `qwen3.5:27b`, `gemma4:26b` fallback) and plan.
- Filename convention `<PROBE_DATE>-hermes-probe-log.md` / `<PROBE_DATE>-hermes-probe.md` / `<PROBE_DATE>-hermes-otel-capture.md` is the same across all tasks.
- The habit-count math in Task 6.1 (days 2-7 = 6 possible Y days, ≥3 = yes, ≤1 = no) matches the spec's § "Habit-verdict mechanics" (the spec said ≥3 out of 7 — this plan corrects to ≥3 out of 6 because day 1 is structurally N setup-day; the spec thresholds still work because ≥3 is the same and ≤1 collapses identically).
- Kill-criteria gates in Task 4 Step 4.4 match spec § "Hard kill" #1-3.
- Fallback logic in Task 5 Step 5.5 matches spec § "Soft kill" #4.
