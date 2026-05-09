---
name: nx-conventions-audit
description: Audit the chitin Nx workspace against docs/architecture/nx-workspace-conventions.md, report tag/folder/dependency drift, and then use spec-driven-development to create or update a concrete fix spec before making structural changes. Use when the user asks to audit Nx state, enforce Nx conventions, restructure apps/libs, cull stale Nx projects, or start solving convention drift.
---

# Nx Conventions Audit

## Workflow

1. Read `docs/architecture/nx-workspace-conventions.md`.
2. Run `scripts/audit-nx-conventions.mjs` from this skill.
3. Inspect the raw Nx state with `pnpm exec nx show projects --json` and
   `pnpm exec nx graph --print` when a finding needs confirmation.
4. Classify findings:
   - **Cull:** projects or folders outside the allowed `AGENTS.md` buckets.
   - **Move:** valid code in the wrong Nx scope or folder.
   - **Tag:** missing or stale `type:*`, `scope:*`, or `lang:*` metadata.
   - **Boundary:** dependency rules, ESLint constraints, or inferred graph edges
     disagree with the convention doc.
   - **Docs:** active docs still describe culled surfaces as live behavior.
5. Invoke `spec-driven-development` before editing structure. If the skill is
   available, load `/home/red/.codex/skills/spec-driven-development/SKILL.md`.
   If unavailable, write an equivalent short spec manually.
6. Use the spec to choose the first small implementation slice. Prefer culling
   dead projects before path moves, and prefer metadata/tag fixes before large
   code motion.

## Audit Script

Run:

```bash
node .agents/skills/nx-conventions-audit/scripts/audit-nx-conventions.mjs
```

The script emits Markdown with:

- detected Nx projects and tags
- missing `type:*`, `scope:*`, `lang:*` tags
- stale `layer:*` tags
- app-folder violations
- known chitin convention mismatches from the local convention doc
- workspace and ESLint configuration drift

Treat script output as a starting point, not a final diagnosis. Confirm risky
deletions with `git ls-files`, docs references, and project graph output.

## Spec Requirements

Create or update a spec under `docs/superpowers/plans/` unless the user names a
different location. The spec should include:

- audit date and commands run
- current graph summary
- issue list grouped by `Cull`, `Move`, `Tag`, `Boundary`, and `Docs`
- implementation phases with acceptance criteria
- verification commands for each phase
- explicit non-goals that preserve `AGENTS.md` boundaries

Do not make broad file moves until the spec identifies dependencies and
verification. For a very small fix, a compact spec section is enough.

## Fix Order

Default order:

1. Remove or supersede obviously dead, out-of-bound projects.
2. Normalize Nx tags to `type:*`, `scope:*`, and `lang:*`.
3. Move valid packages into the target `apps/` and `libs/` shape.
4. Update `pnpm-workspace.yaml`, `eslint.config.mjs`, and project metadata.
5. Add `implicitDependencies` for non-inferred cross-language edges.
6. Verify with Nx graph, project targets, and relevant Go/Python/TS tests.

Use Nx generators for real Nx project moves/removals when available and safe.
For non-project scratch folders, direct deletion is acceptable after confirming
the files are not active behavior.

