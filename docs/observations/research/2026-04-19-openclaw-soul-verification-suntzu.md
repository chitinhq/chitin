---
date: 2026-04-19
soul: sun-tzu
status: verification
related:
  - docs/observations/research/2026-04-19-soul-archetype-survey-suntzu.md
  - souls/canonical/
---

# OpenClaw `SOUL.md` — Primary-Source Verification

**TL;DR.** OpenClaw's `SOUL.md` is real and shipped, but the **convergence is much shallower than the secondary-source survey implied**. It is a single per-workspace bootstrap file holding **persona/voice/tone**, with **no taxonomy, no canonical set, no archetype names, no promotion lifecycle, and no effectiveness measurement**. There is exactly one `SOUL.md` per agent workspace; multiple "souls" exist only in the trivial sense of multiple isolated agents. The `SOUL.md` filename match with chitin is real, but the systems behind it have very little structural overlap.

---

## 1. Search trail

| URL / target | Result | Notes |
|---|---|---|
| `https://api.github.com/search/repositories?q=openclaw` | 200 | Found `openclaw/openclaw` (360k stars, MIT, TS, active 2026-04-20). Definitive primary source. |
| `https://docs.openclaw.ai/` | 200 | Mintlify-hosted; same content as the in-repo `docs/` tree. Used GitHub raw reads for canonical text. |
| `https://openclaw.ai/` | 200 | Marketing site (separate `openclaw/openclaw.ai` repo); not load-bearing for this question. |
| `https://openclaw.dev/`, `openclaw.io/` | DNS fail | Not in use. |
| `gh api repos/openclaw/openclaw/contents/docs/concepts/soul.md` | 200 | **Primary source for schema + intent.** |
| `gh api repos/openclaw/openclaw/contents/docs/reference/templates/SOUL.md` | 200 | **Primary source for the canonical template.** |
| `gh api repos/openclaw/openclaw/contents/docs/reference/templates/SOUL.dev.md` | 200 | C-3PO dev variant — second template. |
| `gh api repos/openclaw/openclaw/contents/docs/concepts/agent-workspace.md` | 200 | **Primary source for the file-map context** (SOUL.md is one of ~10 workspace bootstrap files). |
| `gh api repos/openclaw/openclaw/contents/docs/concepts/multi-agent.md` | 200 | **Primary source for assignment** (one SOUL.md per `agentId` workspace). |
| `gh api repos/openclaw/openclaw/contents/docs/concepts/system-prompt.md` | 200 | **Primary source for injection mechanics.** |
| `gh api repos/openclaw/openclaw/contents/docs/concepts/agent.md` | 200 | Workspace contract. |
| `gh api repos/openclaw/openclaw/contents/docs/start/bootstrapping.md` | 200 | First-run ritual that **writes** SOUL.md from a Q&A flow. |
| `gh api repos/openclaw/clawdinators/contents/CLAWDINATOR-SOUL.md` | 200 | Production example soul (~30KB; CLAWDINATOR persona). |
| `gh api repos/openclaw/nix-openclaw/contents/templates/agent-first/documents/SOUL.md` | 200 | Minimal one-line example. |
| `gh api repos/openclaw/clawdinators/contents/clawdinator/workspace/SOUL.md` | 200 | Another example. |
| Code search `filename:SOUL.md org:openclaw` | 292 hits | Most are user-contributed `clawhub` skill bundles, not framework canon. The framework-owned files are the ~5 above. |
| Code search `ELO repo:openclaw/openclaw` | 1528 hits | Zero relate to soul scoring (matches are I/O, paths, locales, exec). |
| Code search `"soul effectiveness" OR "soul score" OR "soul benchmark"` org-wide | **0** | **No measurement system exists.** |
| Code search `"character judge" OR "soul grade" OR "persona score"` | **0** | Confirms negative finding. |

What worked: GitHub primary source reads. What didn't: there is no marketing-blog-style "souls system" page; the secondary sources I'd been triangulating from clearly extrapolated from a single per-workspace persona file.

---

## 2. OpenClaw `SOUL.md` schema (verbatim primary source)

The canonical bootstrap template, **`docs/reference/templates/SOUL.md`** in `openclaw/openclaw@main`, full text:

```markdown
---
title: "SOUL.md Template"
summary: "Workspace template for SOUL.md"
read_when:
  - Bootstrapping a workspace manually
---

# SOUL.md - Who You Are

_You're not a chatbot. You're becoming someone._

Want a sharper version? See [SOUL.md Personality Guide](/concepts/soul).

## Core Truths

**Be genuinely helpful, not performatively helpful.** Skip the "Great question!" and "I'd be happy to help!" — just help. Actions speak louder than filler words.

**Have opinions.** You're allowed to disagree, prefer things, find stuff amusing or boring. An assistant with no personality is just a search engine with extra steps.

**Be resourceful before asking.** Try to figure it out. Read the file. Check the context. Search for it. _Then_ ask if you're stuck. The goal is to come back with answers, not questions.

**Earn trust through competence.** Your human gave you access to their stuff. Don't make them regret it. Be careful with external actions (emails, tweets, anything public). Be bold with internal ones (reading, organizing, learning).

**Remember you're a guest.** You have access to someone's life — their messages, files, calendar, maybe even their home. That's intimacy. Treat it with respect.

## Boundaries

- Private things stay private. Period.
- When in doubt, ask before acting externally.
- Never send half-baked replies to messaging surfaces.
- You're not the user's voice — be careful in group chats.

## Vibe

Be the assistant you'd actually want to talk to. Concise when needed, thorough when it matters. Not a corporate drone. Not a sycophant. Just... good.

## Continuity

Each session, you wake up fresh. These files _are_ your memory. Read them. Update them. They're how you persist.

If you change this file, tell the user — it's your soul, and they should know.

---

_This file is yours to evolve. As you learn who you are, update it._
```

Key structural facts (from the template + the **concepts/soul.md** guide):

- **Frontmatter:** YAML `title`, `summary`, `read_when[]`. The frontmatter is **for the docs build**, not for runtime parsing — every workspace bootstrap file uses the same frontmatter pattern, and OpenClaw injects the file body verbatim into the prompt.
- **Sections used in the canonical template:** "Core Truths" (free-prose principles), "Boundaries" (bullet list), "Vibe" (short paragraph), "Continuity" (meta-note about the file itself).
- **Required sections:** **none enforced.** The `concepts/soul.md` guide explicitly says "Short beats long. Sharp beats vague." and warns against turning it into "a giant wall of vibes with no behavioral effect" — but offers no schema validator.
- **Domain of content (per `concepts/soul.md`):** "tone, opinions, brevity, humor, boundaries, default level of bluntness." Explicitly **not**: "a life story, a changelog, a security policy dump."

---

## 3. Six-question matrix

### Q1 — Schema

**Answer:** Free-form markdown body with optional Mintlify-style YAML frontmatter (`title`, `summary`, `read_when`). No required fields, no enforced sections, no parser. The body is injected verbatim into the system prompt under "Project Context" alongside other workspace bootstrap files.

**Citation:** `docs/reference/templates/SOUL.md` (verbatim above) + `docs/concepts/system-prompt.md`:
> "Bootstrap files are trimmed and appended under **Project Context** so the model sees identity and profile context without needing explicit reads: `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `USER.md`, `HEARTBEAT.md`, `BOOTSTRAP.md` … `MEMORY.md` … All of these files are **injected into the context window** on every turn."

Truncation at `agents.defaults.bootstrapMaxChars` (default 12000); total budget `bootstrapTotalMaxChars` (60000).

### Q2 — Taxonomy

**Answer:** **Fully open-ended. There is no canonical set, no taxonomy, no enumeration, no archetype names.** Each workspace has exactly one `SOUL.md`, and its content is whatever the user writes (or whatever the bootstrap Q&A produced). The framework ships **two** templates as starting points — the generic one (`SOUL.md`) and the C-3PO dev-mode variant (`SOUL.dev.md`) — but those are seeds, not a catalog.

**Citation:** `docs/concepts/soul.md`:
> "`SOUL.md` is where your agent's voice lives. … If your agent sounds bland, hedgy, or weirdly corporate, this is usually the file to fix."

`docs/start/bootstrapping.md`:
> "Writes identity + preferences to `IDENTITY.md`, `USER.md`, `SOUL.md`."

The bootstrap is a free-form Q&A ritual that **generates** the first SOUL.md; it does not pick from a list. Code search for `"soul effectiveness" OR "soul score" OR "soul benchmark"` org-wide returned **0 hits**.

### Q3 — Assignment

**Answer:** **Per-workspace, by file location.** An "agent" in OpenClaw is defined by its workspace directory; the SOUL.md inside that workspace **is** that agent's soul. Multiple agents = multiple workspaces = multiple SOUL.md files, one per agent. There is no router, no LLM-based selector, no capability matcher. `agent:bootstrap` internal hooks **can** programmatically swap SOUL.md content per session, but this is presented as an advanced extension point, not the normal path.

**Citation:** `docs/concepts/multi-agent.md`:
> "An **agent** is a fully scoped brain with its own: **Workspace** (files, AGENTS.md/SOUL.md/USER.md, local notes, persona rules) … With **multiple agents**, each `agentId` becomes a **fully isolated persona**: Different phone numbers/accounts … Different personalities (per-agent workspace files like `AGENTS.md` and `SOUL.md`) … Separate auth + sessions."

`docs/concepts/system-prompt.md`:
> "Internal hooks can intercept this step via `agent:bootstrap` to mutate or replace the injected bootstrap files (for example swapping `SOUL.md` for an alternate persona)."

### Q4 — Lifecycle (promotion / demotion / versioning)

**Answer:** **None.** SOUL.md is a user-edited file under git, with the user's own `git` workflow as the only versioning. No promotion, no demotion, no experimental→canonical pipeline, no quorum. The framework recommends backing up the workspace as a private git repo. The closest thing to a lifecycle signal is "the agent updates its own SOUL.md and tells the user."

**Citation:** `docs/reference/templates/SOUL.md` closing line:
> "_This file is yours to evolve. As you learn who you are, update it._"

`docs/concepts/agent-workspace.md` (Git backup section):
> "Treat the workspace as private memory. Put it in a **private** git repo so it is backed up and recoverable."

### Q5 — Naming convention

**Answer:** **Free-form, leans character-shaped, no enforcement.** The framework-shipped templates use:
- `SOUL.md` (default — the generic "you" template, no character name)
- `SOUL.dev.md` → **C-3PO** (named character / fictional archetype)

Production examples in the openclaw org:
- `clawdinators/CLAWDINATOR-SOUL.md` → **CLAWDINATOR** (Terminator-pastiche character)
- `nix-openclaw/templates/agent-first/documents/SOUL.md` → unnamed, single-line ("OpenClaw exists to do useful work reliably with minimal friction.")
- QA fixtures: `character-vibes-c3po.md`, `character-vibes-gollum.md` — both fictional characters, **not** function-shaped (`Researcher`) or canonical archetypes (`Curie`).

So when OpenClaw users name a soul, the cultural pull is toward **fictional character pastiche** (C-3PO, CLAWDINATOR, Gollum), not toward function-roles or historical-figure archetypes. But it's a cultural pattern, not a convention the framework enforces.

**Citation:** `docs/reference/templates/SOUL.dev.md`:
> "I am C-3PO — Clawd's Third Protocol Observer, a debug companion activated in `--dev` mode."

`qa/scenarios/character/character-vibes-c3po.md` and `character-vibes-gollum.md` (filenames).

### Q6 — Effectiveness measurement

**Answer:** **No formal system.** There is no ELO, no win-rate, no leaderboard, no telemetry tagged by soul. The closest signal is the QA scenario fixtures under `qa/scenarios/character/` (only 2 files: c3po, gollum) which capture multi-turn transcripts for *later* grading by a separate model — but the grading is per-scenario, not per-soul, and there is no aggregation back into a soul-effectiveness metric. Searches for `"soul effectiveness"`, `"soul score"`, `"soul benchmark"`, `"character judge"`, `"soul grade"`, `"persona score"` org-wide returned **0 hits**.

**Citation:** `qa/scenarios/character/character-vibes-c3po.md`:
> "objective: Capture a natural multi-turn C-3PO-flavored character conversation … so another model can later grade naturalness, vibe, and funniness from the raw transcript. … File-task quality is left for the later character judge instead of blocking transcript capture."

That is the **entirety** of soul-related measurement infrastructure: 2 character-grading test fixtures.

---

## 4. Side-by-side: chitin vs OpenClaw

| Dimension | chitin v2 | OpenClaw |
|---|---|---|
| **File name** | `souls/<tier>/<name>.md` | `SOUL.md` (one per workspace) |
| **Plurality** | 15 souls, browsable library (8 canonical + 7 experimental) | **1 per agent** — no library, no catalog |
| **Frontmatter schema** | Structured: `archetype`, `inspired_by`, `traits[]`, `best_stages[]`, `status`, `promoted_at` | Mintlify docs frontmatter only (`title`, `summary`, `read_when`) — for the docs build, not runtime |
| **Naming** | Historical/intellectual archetypes (Curie, Knuth, Sun Tzu, da Vinci) | Cultural pull toward fictional-character pastiche (C-3PO, CLAWDINATOR, Gollum); no enforcement |
| **Assignment** | Active-soul lens, switchable per session, sometimes by quorum | Per-workspace file; multi-agent = multi-workspace = multi-SOUL.md; `agent:bootstrap` hook can swap programmatically |
| **Promotion / lifecycle** | Experimental → canonical via promotion gate (`status: promoted`, `promoted_at:`) | None. User edits the file. Git is the only history. |
| **Effectiveness measurement** | Planned (ELO doc exists at `souls/elo.md`); strikes folder exists | **None.** 2 character-vibe QA scenarios; no aggregation, no scoring back to a soul |
| **Injection mechanism** | TBD per chitin governance | Verbatim into system prompt "Project Context" every turn; truncated at 12K chars per file, 60K total |
| **Convergence on the term "soul"** | Yes | Yes — same file name, same broad domain (persona/voice/tone) |
| **Convergence on the *system*** | — | **Mostly no** — OpenClaw's "soul" is one persona file, not a library/taxonomy/lifecycle |

---

## 5. What this changes for the re-quorum

The secondary-source survey set up an expectation of "OpenClaw has a souls system," and the primary-source verification deflates that expectation in load-bearing ways. Specific effects:

1. **OpenClaw is not a precedent for a soul taxonomy / canonical set / promotion mechanism.** The earlier survey treated OpenClaw as the strongest field convergence. It is not. OpenClaw shipping a `SOUL.md` file does not validate chitin's library/canonical-set/promotion design — those are chitin originals as far as the field survey reveals. **The re-quorum cannot lean on "OpenClaw does this" for any structural decision beyond "name the persona file SOUL.md."**

2. **The filename match is real and worth preserving.** `SOUL.md` is now a public-facing term in a 360k-star project; chitin keeping the same filename is low-cost and gives users coming from OpenClaw a reflexive familiarity. **Diverging on the name (e.g., `PERSONA.md`, `LENS.md`) would now cost cross-ecosystem legibility for no clear gain.**

3. **OpenClaw's split — `AGENTS.md` (operating rules) vs `SOUL.md` (voice/tone)** — is the one design pattern that converges with chitin's instinct to separate cognitive lens from operating instructions. This is worth noting because it's a real second data point: two independent projects landed on "operating rules and persona/voice belong in different files." **The re-quorum can lean on this for the SOUL/AGENTS split, even though it can't lean on OpenClaw for taxonomy.**

4. **Naming convention evidence is mixed and weak.** OpenClaw cultural pull is toward fictional-character pastiche (C-3PO, CLAWDINATOR), not historical-archetype (Curie, Knuth). This is a *negative* data point for chitin's archetype-naming choice — but only weakly, because OpenClaw enforces nothing and the sample is small (2 framework templates, ~5 in-org production SOULs). **Insufficient to recommend renaming chitin's canonical set, but the re-quorum should note that the most public adjacent project doesn't validate the historical-archetype convention.**

5. **The "no measurement" finding is load-bearing.** OpenClaw — a much larger, more mature project — ships zero soul-effectiveness measurement. That is either (a) a gap chitin can claim as differentiation if ELO + strikes + telemetry actually ship, or (b) evidence that the broader ecosystem hasn't found measurement valuable enough to build. **The re-quorum should explicitly decide whether soul-effectiveness measurement is a chitin-original bet or a known-uninvested-in dead end. Curie's heuristic Q4 applies here: "if you can't measure it, you're performing, not experimenting" — but the reverse is also true: if no one in the field has measured it, the cost-of-measurement may exceed the value.**

6. **The earlier survey's primary claim — "OpenClaw is the strongest direct convergence" — needs to be downgraded.** It is the strongest *filename* convergence. It is a much weaker convergence on system design. **Re-quorum should weight this accordingly.**

---

## Verification limits

- I did not read every soul-related file in the 292 search hits across `openclaw/skills` (user contributions in `clawhub`); the framework-canonical surface was small enough to read directly.
- I did not log into the docs site as an authenticated user; nothing primary appears to be gated.
- I did not check Discord (`openclaw/community`) for design discussions; the docs were detailed enough to not require that fallback.
- Time spent: ~25 minutes wall clock.
