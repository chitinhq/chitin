# Dogfood-Driven Governance-Debt Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make chitin always-on observability on this RTX 3090 Linux box, add the CLI tooling needed for a weekly governance-debt-ledger review, ship a GH Actions composite stub, and complete the openclaw-adapter investigation up to a design-addendum spec (implementation deferred to a follow-up plan per Socrates/Knuth gates).

**Architecture:** Generic `install --surface <name>` subcommand in the Go kernel (claude-code surface implemented; pattern documented for future surfaces). User-level install writes into `~/.claude/settings.json` pointing at a stable binary path (`~/.local/bin/chitin-kernel`). Orphan sessions (no enclosing `.chitin/`) land in `~/.chitin/`. Ledger lives at `chitin/docs/observations/governance-debt-ledger.md`; tooling (`chitin health`, `chitin review`, `chitin ledger new|lint`) supports the weekly review protocol. Private work repos that the user clones onto the same box add `.chitin/` to their gitignore on first session (onboarding note for any specific private target is tracked in a private workspace, not in this OSS repo).

**Tech Stack:** Go 1.25 (kernel), TypeScript + Node (CLI and adapter), pnpm + Nx workspace, Vitest (TS tests), `go test` (Go tests), `commander` (CLI), `better-sqlite3` (ledger lint trace resolution).

**Spec:** `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md` (commit `5c0f9b2`).

**Active soul during planning:** da Vinci.

---

## File Structure

### New files

**Go kernel:**
- `go/execution-kernel/internal/hookinstall/global.go` — global `~/.claude/settings.json` install/uninstall with merge semantics
- `go/execution-kernel/internal/hookinstall/global_test.go`
- `go/execution-kernel/internal/chitindir/resolve.go` — walk-up resolver, `~/.chitin/` orphan fallback
- `go/execution-kernel/internal/chitindir/resolve_test.go`
- `go/execution-kernel/internal/health/health.go` — metrics gathering
- `go/execution-kernel/internal/health/health_test.go`

**TypeScript:**
- `libs/contracts/src/chitindir-resolve.ts` — TS mirror of the resolver used by adapter hot path
- `libs/contracts/src/chitindir-resolve.test.ts`
- `libs/adapters/claude-code/bin/cli.ts` — stdin hook JSON → `runHook` entrypoint
- `libs/adapters/claude-code/bin/cli.test.ts`
- `apps/cli/src/commands/install.ts` — `chitin install --surface <name>` CLI
- `apps/cli/src/commands/uninstall.ts` — matching uninstall
- `apps/cli/src/commands/health.ts` — `chitin health` CLI
- `apps/cli/src/commands/review.ts` — `chitin review --last <window>` CLI
- `apps/cli/src/commands/ledger-new.ts` — `chitin ledger new <lane>` CLI
- `apps/cli/src/commands/ledger-lint.ts` — `chitin ledger lint` CLI

**Docs and meta:**
- `docs/observations/governance-debt-ledger.md` — seeded ledger (empty table of entries)
- `docs/observations/retrospectives/.gitkeep`
- `.github/actions/observe/action.yml` — composite action stub
- `scripts/install-kernel-symlink.sh` — symlink `dist/.../chitin-kernel` to `~/.local/bin/chitin-kernel`

**openclaw workstream outputs (Phase F):**
- `libs/adapters/openclaw/README.md` — graduated from SPIKE.md with answered questions
- `docs/superpowers/specs/2026-04-XX-openclaw-adapter-implementation-design.md` — addendum spec authored after investigation

### Modified files

- `go/execution-kernel/cmd/chitin-kernel/main.go` — add `install`, `uninstall`, `health` subcommand dispatch
- `go/execution-kernel/internal/hookinstall/install.go` — export `SubscribedHooks` so global install reuses the list
- `apps/cli/src/main.ts` — register `install`, `uninstall`, `health`, `review`, `ledger` subcommands
- `package.json` (root) — add `install-kernel` script that builds + symlinks
- `libs/adapters/claude-code/package.json` — add `bin` entry
- `.github/workflows/ci.yml` — add observe composite action step (Phase E2)
- `.gitignore` (chitin repo) — unchanged (chitin's own `.chitin/` stays committed per spec)

---

## Phase A — Foundation: Binary Placement and Orphan-Events Resolver

### Task A1: Add a script that symlinks the kernel binary into ~/.local/bin

**Files:**
- Create: `scripts/install-kernel-symlink.sh`
- Modify: `package.json` (root) — add script entry

- [ ] **Step 1: Write the script**

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_SRC="$REPO_ROOT/dist/go/execution-kernel/chitin-kernel"
BIN_DST_DIR="${CHITIN_BIN_DIR:-$HOME/.local/bin}"
BIN_DST="$BIN_DST_DIR/chitin-kernel"

if [[ ! -x "$BIN_SRC" ]]; then
  echo "error: $BIN_SRC does not exist or is not executable. Run: pnpm nx build execution-kernel" >&2
  exit 1
fi

mkdir -p "$BIN_DST_DIR"

# If $BIN_DST exists and is NOT a symlink, abort (safety).
if [[ -e "$BIN_DST" && ! -L "$BIN_DST" ]]; then
  echo "error: $BIN_DST exists and is not a symlink. Refusing to overwrite." >&2
  exit 1
fi

ln -sf "$BIN_SRC" "$BIN_DST"
echo "symlinked $BIN_DST -> $BIN_SRC"
```

Create the file at `scripts/install-kernel-symlink.sh` with the content above, then `chmod +x scripts/install-kernel-symlink.sh`.

- [ ] **Step 2: Add pnpm script to root package.json**

Edit `package.json` — replace the empty `"scripts": {}` with:

```json
  "scripts": {
    "install-kernel": "pnpm nx build execution-kernel && bash scripts/install-kernel-symlink.sh"
  },
```

- [ ] **Step 3: Run it to verify**

Run: `pnpm install-kernel`
Expected: kernel builds, then stdout prints `symlinked /home/red/.local/bin/chitin-kernel -> .../dist/go/execution-kernel/chitin-kernel`.
Verify: `ls -l ~/.local/bin/chitin-kernel` shows the symlink; `chitin-kernel` on PATH runs and prints usage (`no_subcommand`).

- [ ] **Step 4: Commit**

```bash
git add scripts/install-kernel-symlink.sh package.json
git commit -m "build: install-kernel symlink to ~/.local/bin for stable path"
```

---

### Task A2: Go-side `chitindir` resolver with walk-up + orphan fallback

**Files:**
- Create: `go/execution-kernel/internal/chitindir/resolve.go`
- Test: `go/execution-kernel/internal/chitindir/resolve_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/execution-kernel/internal/chitindir/resolve_test.go`:

```go
package chitindir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_FindsExistingChitinDirInParent(t *testing.T) {
	root := t.TempDir()
	chitin := filepath.Join(root, ".chitin")
	if err := os.MkdirAll(chitin, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(nested, root)
	if err != nil {
		t.Fatal(err)
	}
	if got != chitin {
		t.Fatalf("want %s, got %s", chitin, got)
	}
}

func TestResolve_OrphanFallbackWhenNoChitinDirFound(t *testing.T) {
	cwd := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := Resolve(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("orphan dir not created: %v", err)
	}
}

func TestResolve_StopsAtWorkspaceBoundary(t *testing.T) {
	boundary := t.TempDir()
	outside := filepath.Dir(boundary)
	// .chitin exists ABOVE the boundary — must not be found.
	if err := os.MkdirAll(filepath.Join(outside, ".chitin"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(boundary, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	got, err := Resolve(nested, boundary)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeHome, ".chitin")
	if got != want {
		t.Fatalf("expected orphan fallback at %s, got %s", want, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd go/execution-kernel && go test ./internal/chitindir/...`
Expected: FAIL — `package chitindir has no Go files` or `undefined: Resolve`.

- [ ] **Step 3: Write the resolver**

Create `go/execution-kernel/internal/chitindir/resolve.go`:

```go
// Package chitindir resolves the .chitin state dir for a given cwd.
//
// Walk-up semantics: walk from cwd upward, stopping at workspaceBoundary (if
// given, exclusive — we do not leave the workspace); if a .chitin/ dir is
// found along the way, return it. Otherwise fall back to $HOME/.chitin/
// (orphan sessions), creating it if missing.
package chitindir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Resolve returns the absolute path to the .chitin state dir for cwd.
//
// If workspaceBoundary is non-empty, the walk stops at that boundary (the
// boundary itself IS inspected; ancestors of the boundary are NOT).
// Returns the orphan path ($HOME/.chitin) and creates it on-demand if no
// enclosing .chitin/ is found.
func Resolve(cwd, workspaceBoundary string) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("abs cwd: %w", err)
	}
	absBoundary := ""
	if workspaceBoundary != "" {
		absBoundary, err = filepath.Abs(workspaceBoundary)
		if err != nil {
			return "", fmt.Errorf("abs boundary: %w", err)
		}
	}

	dir := absCwd
	for {
		candidate := filepath.Join(dir, ".chitin")
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() {
			return candidate, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) && statErr != nil {
			return "", fmt.Errorf("stat %s: %w", candidate, statErr)
		}
		if absBoundary != "" && dir == absBoundary {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	orphan := filepath.Join(home, ".chitin")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		return "", fmt.Errorf("mkdir orphan: %w", err)
	}
	return orphan, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd go/execution-kernel && go test ./internal/chitindir/... -v`
Expected: `--- PASS: TestResolve_FindsExistingChitinDirInParent`, `--- PASS: TestResolve_OrphanFallbackWhenNoChitinDirFound`, `--- PASS: TestResolve_StopsAtWorkspaceBoundary`.

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/chitindir/
git commit -m "feat(kernel): chitindir resolver with walk-up and orphan fallback"
```

---

### Task A3: TS mirror of the resolver for the adapter hot path

**Files:**
- Create: `libs/contracts/src/chitindir-resolve.ts`
- Test: `libs/contracts/src/chitindir-resolve.test.ts`

- [ ] **Step 1: Write the failing test**

Create `libs/contracts/src/chitindir-resolve.test.ts`:

```typescript
import { mkdtempSync, mkdirSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { resolveChitinDir } from './chitindir-resolve';

describe('resolveChitinDir', () => {
  const originalHome = process.env.HOME;

  afterEach(() => {
    process.env.HOME = originalHome;
  });

  it('finds an existing .chitin dir in a parent', () => {
    const root = mkdtempSync(join(tmpdir(), 'cd-test-'));
    const chitin = join(root, '.chitin');
    mkdirSync(chitin);
    const nested = join(root, 'a', 'b');
    mkdirSync(nested, { recursive: true });

    const got = resolveChitinDir(nested, root);
    expect(got).toBe(chitin);
  });

  it('falls back to $HOME/.chitin when none found, creating on-demand', () => {
    const cwd = mkdtempSync(join(tmpdir(), 'cd-cwd-'));
    const fakeHome = mkdtempSync(join(tmpdir(), 'cd-home-'));
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(cwd, '');
    const want = join(fakeHome, '.chitin');
    expect(got).toBe(want);
    expect(statSync(want).isDirectory()).toBe(true);
  });

  it('stops at workspace boundary', () => {
    const boundary = mkdtempSync(join(tmpdir(), 'cd-bound-'));
    const outside = dirname(boundary);
    mkdirSync(join(outside, '.chitin'), { recursive: true });
    const nested = join(boundary, 'sub');
    mkdirSync(nested);
    const fakeHome = mkdtempSync(join(tmpdir(), 'cd-home2-'));
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(nested, boundary);
    expect(got).toBe(join(fakeHome, '.chitin'));
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm nx test contracts -- chitindir-resolve`
Expected: FAIL — `Cannot find module './chitindir-resolve'`.

- [ ] **Step 3: Write the resolver**

Create `libs/contracts/src/chitindir-resolve.ts`:

```typescript
import { existsSync, statSync, mkdirSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { homedir } from 'node:os';

/**
 * Resolve the .chitin state dir for a given cwd.
 *
 * Walks up from cwd looking for an existing `.chitin/` directory. Stops at
 * workspaceBoundary (inspected; not crossed). Falls back to `$HOME/.chitin/`
 * (creating it on demand) when no enclosing dir is found.
 */
export function resolveChitinDir(cwd: string, workspaceBoundary: string): string {
  const absCwd = resolve(cwd);
  const absBoundary = workspaceBoundary ? resolve(workspaceBoundary) : '';

  let dir = absCwd;
  while (true) {
    const candidate = join(dir, '.chitin');
    if (existsSync(candidate) && statSync(candidate).isDirectory()) {
      return candidate;
    }
    if (absBoundary && dir === absBoundary) break;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  const orphan = join(homedir(), '.chitin');
  if (!existsSync(orphan)) {
    mkdirSync(orphan, { recursive: true });
  }
  return orphan;
}
```

- [ ] **Step 4: Export from the contracts index**

Check `libs/contracts/src/index.ts` for the existing export pattern. Append:

```typescript
export { resolveChitinDir } from './chitindir-resolve.js';
```

(If the index uses `.ts` extensions instead of `.js`, match that style.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `pnpm nx test contracts -- chitindir-resolve`
Expected: 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add libs/contracts/src/chitindir-resolve.ts libs/contracts/src/chitindir-resolve.test.ts libs/contracts/src/index.ts
git commit -m "feat(contracts): resolveChitinDir TS helper mirrors Go resolver"
```

---

## Phase B — Claude Code Global Install

### Task B1: Export SubscribedHooks from the hookinstall package

**Files:**
- Modify: `go/execution-kernel/internal/hookinstall/install.go`

- [ ] **Step 1: Rename the unexported slice to an exported one**

Replace (in `install.go`):

```go
var subscribedHooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PreCompact",
	"SubagentStop",
	"SessionEnd",
}
```

with:

```go
// SubscribedHooks is the canonical list of Claude Code hook event names
// that chitin forwards to the kernel. Shared between session-scoped and
// global installs.
var SubscribedHooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PreCompact",
	"SubagentStop",
	"SessionEnd",
}
```

and replace `range subscribedHooks` with `range SubscribedHooks` in `Install()`.

- [ ] **Step 2: Run existing tests to ensure no regression**

Run: `cd go/execution-kernel && go test ./internal/hookinstall/...`
Expected: existing 2 tests still pass.

- [ ] **Step 3: Commit**

```bash
git add go/execution-kernel/internal/hookinstall/install.go
git commit -m "refactor(kernel): export SubscribedHooks for reuse by global install"
```

---

### Task B2: Global install/uninstall in hookinstall with merge semantics

**Files:**
- Create: `go/execution-kernel/internal/hookinstall/global.go`
- Test: `go/execution-kernel/internal/hookinstall/global_test.go`

- [ ] **Step 1: Write the failing tests**

Create `go/execution-kernel/internal/hookinstall/global_test.go`:

```go
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallGlobal_WritesHooksIntoEmptyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	if err := InstallGlobal("/usr/local/bin/chitin-claude-code-adapter"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("expected hooks map, got %T", s["hooks"])
	}
	for _, h := range SubscribedHooks {
		if _, ok := hooks[h]; !ok {
			t.Errorf("missing hook %s", h)
		}
	}
}

func TestInstallGlobal_MergesIntoExistingSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	settingsPath := filepath.Join(claudeDir, "settings.json")

	existing := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settingsPath, b, 0o644)

	if err := InstallGlobal("/usr/local/bin/chitin-claude-code-adapter"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(raw, &s)
	if s["theme"] != "dark" {
		t.Errorf("theme lost, got %v", s["theme"])
	}
	hooks := s["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("expected 2 PreToolUse entries (existing + chitin), got %d", len(pre))
	}
}

func TestUninstallGlobal_RemovesOnlyChitinEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	settingsPath := filepath.Join(claudeDir, "settings.json")

	adapterPath := "/usr/local/bin/chitin-claude-code-adapter"
	existing := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"type": "command", "command": "/usr/local/bin/other-tool"},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(settingsPath, b, 0o644)

	if err := InstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}
	if err := UninstallGlobal(adapterPath); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var s map[string]any
	json.Unmarshal(raw, &s)
	if s["theme"] != "dark" {
		t.Errorf("theme lost after uninstall")
	}
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("want 1 PreToolUse after uninstall, got %d", len(pre))
	}
	entry := pre[0].(map[string]any)
	if entry["command"] != "/usr/local/bin/other-tool" {
		t.Errorf("uninstall removed the wrong entry: %v", entry)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd go/execution-kernel && go test ./internal/hookinstall/... -run Global`
Expected: FAIL — `undefined: InstallGlobal` and `undefined: UninstallGlobal`.

- [ ] **Step 3: Write the implementation**

Create `go/execution-kernel/internal/hookinstall/global.go`:

```go
package hookinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// globalSettingsPath returns $HOME/.claude/settings.json.
func globalSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// InstallGlobal merges chitin's hook entries into ~/.claude/settings.json.
// adapterBinary is the absolute path to the Claude Code adapter CLI.
// Pre-existing non-chitin hook entries are preserved.
func InstallGlobal(adapterBinary string) error {
	path, err := globalSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	settings, err := loadSettings(path)
	if err != nil {
		return err
	}
	hooks := ensureHooksMap(settings)

	chitinEntry := map[string]any{"type": "command", "command": adapterBinary}
	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		if !containsAdapter(list, adapterBinary) {
			list = append(list, chitinEntry)
		}
		hooks[h] = list
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

// UninstallGlobal removes entries whose command equals adapterBinary.
// Leaves unrelated hook entries intact.
func UninstallGlobal(adapterBinary string) error {
	path, err := globalSettingsPath()
	if err != nil {
		return err
	}
	settings, err := loadSettings(path)
	if err != nil {
		return err
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil // nothing to uninstall
	}
	for _, h := range SubscribedHooks {
		list := toAnySlice(hooks[h])
		filtered := make([]any, 0, len(list))
		for _, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				filtered = append(filtered, e)
				continue
			}
			if m["command"] == adapterBinary {
				continue
			}
			filtered = append(filtered, m)
		}
		if len(filtered) == 0 {
			delete(hooks, h)
		} else {
			hooks[h] = filtered
		}
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

func loadSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s == nil {
		s = map[string]any{}
	}
	return s, nil
}

func writeSettings(path string, s map[string]any) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func ensureHooksMap(settings map[string]any) map[string]any {
	h, ok := settings["hooks"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return h
}

func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func containsAdapter(list []any, adapterBinary string) bool {
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if m["command"] == adapterBinary {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd go/execution-kernel && go test ./internal/hookinstall/... -v`
Expected: all 5 tests pass (2 original + 3 new).

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/hookinstall/global.go go/execution-kernel/internal/hookinstall/global_test.go
git commit -m "feat(kernel): InstallGlobal / UninstallGlobal with merge semantics"
```

---

### Task B3: Kernel `install` and `uninstall` subcommands with --surface dispatch

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/main.go`

- [ ] **Step 1: Add subcommand cases to main dispatch**

In `go/execution-kernel/cmd/chitin-kernel/main.go`, inside `switch sub { ... }`, add before the `default` case:

```go
	case "install":
		cmdInstall(args)
	case "uninstall":
		cmdUninstall(args)
```

- [ ] **Step 2: Add the handler functions**

Append to the end of `main.go` (before `func exitErr`):

```go
func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	surface := fs.String("surface", "", "surface to install (claude-code)")
	global := fs.Bool("global", false, "install into user-level settings (always-on)")
	adapter := fs.String("adapter", os.Getenv("CHITIN_ADAPTER_BINARY"), "adapter binary path")
	fs.Parse(args)
	if *surface == "" {
		exitErr("missing_surface", "--surface required")
	}
	if !*global {
		exitErr("not_implemented", "non-global install is not yet supported via `install`; use `install-hook` for session-scoped")
	}
	if *adapter == "" {
		exitErr("missing_adapter", "--adapter or CHITIN_ADAPTER_BINARY required")
	}
	switch *surface {
	case "claude-code":
		if err := hookinstall.InstallGlobal(*adapter); err != nil {
			exitErr("install_global", err.Error())
		}
	default:
		exitErr("unknown_surface", *surface)
	}
	fmt.Println(`{"ok":true}`)
}

func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	surface := fs.String("surface", "", "surface to uninstall (claude-code)")
	global := fs.Bool("global", false, "uninstall from user-level settings")
	adapter := fs.String("adapter", os.Getenv("CHITIN_ADAPTER_BINARY"), "adapter binary path")
	fs.Parse(args)
	if *surface == "" {
		exitErr("missing_surface", "--surface required")
	}
	if !*global {
		exitErr("not_implemented", "non-global uninstall not supported via `uninstall`")
	}
	if *adapter == "" {
		exitErr("missing_adapter", "--adapter or CHITIN_ADAPTER_BINARY required")
	}
	switch *surface {
	case "claude-code":
		if err := hookinstall.UninstallGlobal(*adapter); err != nil {
			exitErr("uninstall_global", err.Error())
		}
	default:
		exitErr("unknown_surface", *surface)
	}
	fmt.Println(`{"ok":true}`)
}
```

- [ ] **Step 3: Verify build**

Run: `pnpm nx build execution-kernel`
Expected: build succeeds.

- [ ] **Step 4: Smoke-run the new subcommand**

```bash
HOME=$(mktemp -d) ./dist/go/execution-kernel/chitin-kernel install --surface claude-code --global --adapter /usr/local/bin/fake-adapter
```
Expected stdout: `{"ok":true}`.
Verify: `cat "$HOME/.claude/settings.json"` shows hooks wired for `/usr/local/bin/fake-adapter`.

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/main.go
git commit -m "feat(kernel): install / uninstall subcommands with --surface dispatch"
```

---

### Task B4: Create the Claude Code adapter CLI entrypoint

**Files:**
- Create: `libs/adapters/claude-code/bin/cli.ts`
- Test: `libs/adapters/claude-code/bin/cli.test.ts`
- Modify: `libs/adapters/claude-code/package.json` — add `bin` entry

The Claude Code hook in `~/.claude/settings.json` will be a shell-command string. The adapter bin takes hook JSON on stdin, resolves chitinDir from `cwd`, builds an AdapterContext, and calls `runHook`.

- [ ] **Step 1: Write the failing test**

Create `libs/adapters/claude-code/bin/cli.test.ts`:

```typescript
import { mkdtempSync, mkdirSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect } from 'vitest';

function adapterEntry(): string {
  // Resolve to the bin file under test.
  return join(__dirname, 'cli.ts');
}

describe('claude-code adapter CLI bin', () => {
  it('resolves chitinDir from cwd via walk-up, emits an event', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'adp-'));
    mkdirSync(join(workspace, '.chitin'));
    const cwd = join(workspace, 'a', 'b');
    mkdirSync(cwd, { recursive: true });

    const hookInput = JSON.stringify({
      hook_event_name: 'SessionStart',
      session_id: 'sess-test-1',
      cwd,
    });

    const res = spawnSync(
      'pnpm',
      ['exec', 'tsx', adapterEntry()],
      {
        input: hookInput,
        cwd,
        encoding: 'utf8',
        env: {
          ...process.env,
          CHITIN_KERNEL_BINARY: process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel',
        },
      },
    );
    expect(res.status).toBe(0);
    expect(existsSync(join(workspace, '.chitin'))).toBe(true);
  });
});
```

Note: this test is a smoke test — it verifies the adapter entry resolves chitinDir and exits cleanly. It does NOT require the kernel to be present; if the kernel is absent the adapter should still exit 0 and log a warning, because silent-drop is a Lane-① finding we want to observe, not a hard fail.

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm nx test adapter-claude-code -- cli.test`
Expected: FAIL — the bin file doesn't exist.

- [ ] **Step 3: Write the adapter entry**

Create `libs/adapters/claude-code/bin/cli.ts`:

```typescript
#!/usr/bin/env node
/**
 * Claude Code adapter entrypoint.
 *
 * Reads hook JSON from stdin, resolves .chitin/ via walk-up (or orphan
 * fallback at $HOME/.chitin/), builds an AdapterContext from env + cwd,
 * calls runHook(), and exits. Hook failure is non-fatal to Claude Code —
 * chitin must never break the user's session.
 */
import { readFileSync } from 'node:fs';
import { resolveChitinDir } from '@chitin/contracts';
import { runHook, type AdapterContext } from '../src/hook-runner.js';
import { buildAdapterContext } from '../src/adapter-context.js';

function readStdinSync(): string {
  // process.stdin.fd is 0; reading synchronously is acceptable for hook entry.
  try {
    return readFileSync(0, 'utf8');
  } catch {
    return '';
  }
}

async function main(): Promise<void> {
  const raw = readStdinSync();
  if (!raw.trim()) {
    process.exit(0);
  }
  let input: Record<string, unknown>;
  try {
    input = JSON.parse(raw);
  } catch (err) {
    console.error('chitin-adapter: invalid hook JSON on stdin', err);
    process.exit(0);
  }

  const hookCwd = typeof input.cwd === 'string' ? input.cwd : process.cwd();
  const chitinDir = resolveChitinDir(hookCwd, '');

  const ctx: AdapterContext = buildAdapterContext({
    surface: 'claude-code',
    chitinDir,
  });

  try {
    runHook(input as Parameters<typeof runHook>[0], ctx);
  } catch (err) {
    console.error('chitin-adapter: runHook failed (non-fatal)', err);
  }
}

main().catch((err) => {
  console.error('chitin-adapter: top-level error (non-fatal)', err);
  process.exit(0);
});
```

- [ ] **Step 4: Create the shared adapter-context builder used by bin and tests**

Check if `libs/adapters/claude-code/src/adapter-context.ts` exists. If not, create it by re-exporting the buildAdapterContext used by `apps/cli/src/ctx.ts`:

```typescript
import { buildAdapterContext as build } from '../../../../apps/cli/src/ctx.js';

export const buildAdapterContext = build;
export type { AdapterContext, AdapterContextInput } from '../../../../apps/cli/src/ctx.js';
```

If the relative-import path is fragile, copy `buildAdapterContext` inline into this file verbatim from `apps/cli/src/ctx.ts`.

- [ ] **Step 5: Add `bin` entry to package.json**

Edit `libs/adapters/claude-code/package.json`, inside the top-level object:

```json
  "bin": {
    "chitin-claude-code-adapter": "./bin/cli.ts"
  },
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `pnpm nx test adapter-claude-code -- cli.test`
Expected: the smoke test passes (status 0, `.chitin/` exists).

- [ ] **Step 7: Commit**

```bash
git add libs/adapters/claude-code/bin/ libs/adapters/claude-code/package.json libs/adapters/claude-code/src/adapter-context.ts
git commit -m "feat(adapter-cc): stdin-based CLI entry for user-level hook install"
```

---

### Task B5: Add `chitin install` CLI command

**Files:**
- Create: `apps/cli/src/commands/install.ts`
- Modify: `apps/cli/src/main.ts` — register the command

- [ ] **Step 1: Write the install command**

Create `apps/cli/src/commands/install.ts`:

```typescript
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';

export function registerInstall(program: Command): void {
  program
    .command('install')
    .description('Install chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to install (claude-code)')
    .option('--global', 'install user-level (always-on)', false)
    .option('--adapter <path>', 'adapter binary path (default: resolve from workspace)')
    .action((opts: { surface: string; global: boolean; adapter?: string }) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapterBin = opts.adapter ?? resolveAdapterBin(opts.surface);

      const args = ['install', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapterBin) args.push('--adapter', adapterBin);

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);

      if (opts.surface === 'claude-code' && opts.global) {
        verifyClaudeCodeInstall(adapterBin);
      }
    });

  program
    .command('uninstall')
    .description('Remove chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to uninstall (claude-code)')
    .option('--global', 'uninstall from user-level settings', false)
    .option('--adapter <path>', 'adapter binary path (must match install)')
    .action((opts: { surface: string; global: boolean; adapter?: string }) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapterBin = opts.adapter ?? resolveAdapterBin(opts.surface);

      const args = ['uninstall', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapterBin) args.push('--adapter', adapterBin);

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);
    });
}

function resolveAdapterBin(surface: string): string {
  if (surface !== 'claude-code') return '';
  // Expect the adapter's bin to have been pnpm-linked; fall back to repo-local tsx invocation.
  const env = process.env.CHITIN_ADAPTER_BINARY;
  if (env) return env;
  const repoLocal = findRepoRoot();
  if (!repoLocal) return '';
  const cliPath = join(repoLocal, 'libs/adapters/claude-code/bin/cli.ts');
  // Wrap as a shell command that invokes tsx on the TS entry. The settings.json
  // command field accepts a full shell invocation.
  return existsSync(cliPath) ? `node --import tsx ${cliPath}` : '';
}

function findRepoRoot(): string | null {
  let dir = process.cwd();
  while (dir !== '/') {
    if (existsSync(join(dir, 'pnpm-workspace.yaml'))) return dir;
    dir = join(dir, '..');
  }
  return null;
}

function verifyClaudeCodeInstall(adapterBin: string): void {
  const home = process.env.HOME;
  if (!home) return;
  const settingsPath = join(home, '.claude', 'settings.json');
  if (!existsSync(settingsPath)) {
    console.error(`verify: settings.json not found at ${settingsPath}`);
    process.exit(4);
  }
  const s = JSON.parse(readFileSync(settingsPath, 'utf8'));
  const hooks = s.hooks ?? {};
  const expected = ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd'];
  for (const h of expected) {
    const list = hooks[h] ?? [];
    if (!list.some((e: { command: string }) => e.command === adapterBin)) {
      console.error(`verify: hook ${h} missing chitin entry pointing at ${adapterBin}`);
      process.exit(4);
    }
  }
  console.log(`verify: OK — chitin adapter wired for ${expected.join(', ')} ...`);
}
```

- [ ] **Step 2: Register the command in main.ts**

In `apps/cli/src/main.ts`, add import at top:

```typescript
import { registerInstall } from './commands/install.js';
```

and call it alongside `registerRun(program)`:

```typescript
registerInstall(program);
```

- [ ] **Step 3: Smoke test the install command**

Run (with a throwaway HOME):

```bash
pnpm nx build execution-kernel
HOME=$(mktemp -d) CHITIN_KERNEL_BINARY=./dist/go/execution-kernel/chitin-kernel \
  pnpm exec tsx apps/cli/src/main.ts install --surface claude-code --global \
  --adapter '/tmp/fake-adapter'
```

Expected: `{"ok":true}` from kernel, then `verify: OK — chitin adapter wired for ...` from CLI.

- [ ] **Step 4: Commit**

```bash
git add apps/cli/src/commands/install.ts apps/cli/src/main.ts
git commit -m "feat(cli): chitin install / uninstall --surface commands"
```

---

### Task B6: End-to-end install verification test (throwaway HOME)

**Files:**
- Create: `apps/cli/tests/install-e2e.test.ts`

- [ ] **Step 1: Write the end-to-end test**

Create `apps/cli/tests/install-e2e.test.ts`:

```typescript
import { mkdtempSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect } from 'vitest';

const repoRoot = join(__dirname, '..', '..', '..');
const kernelBin = join(repoRoot, 'dist/go/execution-kernel/chitin-kernel');
const cliEntry = join(repoRoot, 'apps/cli/src/main.ts');

function run(args: string[], env: Record<string, string>) {
  return spawnSync('pnpm', ['exec', 'tsx', cliEntry, ...args], {
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

describe('chitin install --surface claude-code --global (e2e)', () => {
  it('writes hooks into throwaway HOME and uninstall removes them cleanly', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping e2e: ${kernelBin} missing. Run: pnpm nx build execution-kernel`);
      return;
    }
    const fakeHome = mkdtempSync(join(tmpdir(), 'chitin-e2e-'));
    const fakeAdapter = '/tmp/fake-adapter-bin';

    const installRes = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(installRes.status).toBe(0);

    const settingsPath = join(fakeHome, '.claude', 'settings.json');
    expect(existsSync(settingsPath)).toBe(true);
    const s = JSON.parse(readFileSync(settingsPath, 'utf8'));
    for (const h of ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd']) {
      const list = s.hooks[h] ?? [];
      expect(list.some((e: { command: string }) => e.command === fakeAdapter)).toBe(true);
    }

    const uninstallRes = run(
      ['uninstall', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(uninstallRes.status).toBe(0);

    const s2 = JSON.parse(readFileSync(settingsPath, 'utf8'));
    for (const h of ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd']) {
      const list = s2.hooks?.[h] ?? [];
      expect(list.some((e: { command: string }) => e.command === fakeAdapter)).toBe(false);
    }
  });
});
```

- [ ] **Step 2: Run the test**

Run: `pnpm nx build execution-kernel && pnpm nx test cli -- install-e2e`
Expected: test passes.

- [ ] **Step 3: Commit**

```bash
git add apps/cli/tests/install-e2e.test.ts
git commit -m "test(cli): e2e install/uninstall against throwaway HOME"
```

---

## Phase C — Health CLI

### Task C1: Kernel `health` metrics gatherer

**Files:**
- Create: `go/execution-kernel/internal/health/health.go`
- Test: `go/execution-kernel/internal/health/health_test.go`

- [ ] **Step 1: Write the failing test**

Create `go/execution-kernel/internal/health/health_test.go`:

```go
package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGather_CountsEventsInLastWindow(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "events.jsonl")
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)
	lines := []string{
		`{"schema_version":"2","ts":"` + old + `","event_type":"session_start","surface":"claude-code"}`,
		`{"schema_version":"2","ts":"` + recent + `","event_type":"session_start","surface":"claude-code"}`,
		`{"schema_version":"2","ts":"` + recent + `","event_type":"session_end","surface":"claude-code"}`,
	}
	os.WriteFile(jsonl, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EventsByWindow["claude-code"] != 2 {
		t.Errorf("want 2 recent claude-code events, got %d", rep.EventsByWindow["claude-code"])
	}
	if rep.HookFailureCount != 0 {
		t.Errorf("want 0 hook failures, got %d", rep.HookFailureCount)
	}
}

func TestGather_DetectsHookFailureRecords(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "kernel-errors.log")
	os.WriteFile(log, []byte(`{"ts":"2026-04-19T10:00:00Z","error":"emit","message":"parse_event"}`+"\n"), 0o644)

	rep, err := Gather(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HookFailureCount != 1 {
		t.Errorf("want 1 hook failure, got %d", rep.HookFailureCount)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

Run: `cd go/execution-kernel && go test ./internal/health/...`
Expected: FAIL — `package health has no Go files`.

- [ ] **Step 3: Write the implementation**

Create `go/execution-kernel/internal/health/health.go`:

```go
// Package health gathers dogfooding health metrics.
package health

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report is the shape `chitin health` presents.
type Report struct {
	WindowStart       time.Time      `json:"window_start"`
	EventsByWindow    map[string]int `json:"events_by_window"`
	EventsTotal       int            `json:"events_total"`
	HookFailureCount  int            `json:"hook_failure_count"`
	SchemaDriftCount  int            `json:"schema_drift_count"`
	OrphanedChains    int            `json:"orphaned_chains"`
}

// Gather scans a single .chitin directory and produces a Report for the
// window ending now and lasting `window` duration.
func Gather(chitinDir string, window time.Duration) (Report, error) {
	r := Report{
		WindowStart:    time.Now().Add(-window).UTC(),
		EventsByWindow: map[string]int{},
	}

	entries, err := os.ReadDir(chitinDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("read .chitin dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if err := scanJSONL(filepath.Join(chitinDir, name), &r); err != nil {
			return r, err
		}
	}

	errLog := filepath.Join(chitinDir, "kernel-errors.log")
	if err := scanErrorLog(errLog, &r); err != nil {
		return r, err
	}
	return r, nil
}

func scanJSONL(path string, r *Report) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // missing jsonl is fine
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24) // allow long lines
	for sc.Scan() {
		var ev struct {
			TS      string `json:"ts"`
			Surface string `json:"surface"`
			Schema  string `json:"schema_version"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			r.SchemaDriftCount++
			continue
		}
		if ev.Schema != "" && ev.Schema != "2" {
			r.SchemaDriftCount++
		}
		t, err := time.Parse(time.RFC3339, ev.TS)
		if err != nil {
			continue
		}
		if t.Before(r.WindowStart) {
			continue
		}
		r.EventsTotal++
		r.EventsByWindow[ev.Surface]++
	}
	return sc.Err()
}

func scanErrorLog(path string, r *Report) error {
	f, err := os.Open(path)
	if err != nil {
		return nil // missing is fine
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		r.HookFailureCount++
	}
	return sc.Err()
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd go/execution-kernel && go test ./internal/health/... -v`
Expected: both tests pass.

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/internal/health/
git commit -m "feat(kernel): health.Gather — events, hook failures, schema drift"
```

---

### Task C2: Kernel `health` subcommand

**Files:**
- Modify: `go/execution-kernel/cmd/chitin-kernel/main.go`

- [ ] **Step 1: Add the case to the dispatch**

In the switch in `main()`:

```go
	case "health":
		cmdHealth(args)
```

- [ ] **Step 2: Add the handler**

Add at end of `main.go`:

```go
func cmdHealth(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	dir := fs.String("dir", ".chitin", "path to .chitin state dir")
	windowHours := fs.Int("window-hours", 24, "window size in hours")
	fs.Parse(args)
	absDir, _ := filepath.Abs(*dir)
	rep, err := health.Gather(absDir, time.Duration(*windowHours)*time.Hour)
	if err != nil {
		exitErr("health", err.Error())
	}
	out, _ := json.Marshal(rep)
	fmt.Println(string(out))
}
```

And add the imports at the top of `main.go` if missing:

```go
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/health"
```

- [ ] **Step 3: Verify build**

Run: `pnpm nx build execution-kernel`
Expected: build succeeds.

- [ ] **Step 4: Smoke run**

```bash
mkdir -p /tmp/chk/.chitin
echo '{"schema_version":"2","ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","event_type":"session_start","surface":"claude-code"}' > /tmp/chk/.chitin/events.jsonl
./dist/go/execution-kernel/chitin-kernel health --dir /tmp/chk/.chitin
```

Expected: JSON line containing `"events_total":1`, `"events_by_window":{"claude-code":1}`.

- [ ] **Step 5: Commit**

```bash
git add go/execution-kernel/cmd/chitin-kernel/main.go
git commit -m "feat(kernel): health subcommand over .chitin JSONL + error log"
```

---

### Task C3: `chitin health` CLI wrapper with pass/warn/fail coloring

**Files:**
- Create: `apps/cli/src/commands/health.ts`
- Modify: `apps/cli/src/main.ts`

- [ ] **Step 1: Write the command**

Create `apps/cli/src/commands/health.ts`:

```typescript
import { spawnSync } from 'node:child_process';
import { resolveChitinDir } from '@chitin/contracts';
import type { Command } from 'commander';

interface HealthReport {
  events_total: number;
  events_by_window: Record<string, number>;
  hook_failure_count: number;
  schema_drift_count: number;
  orphaned_chains: number;
}

export function registerHealth(program: Command): void {
  program
    .command('health')
    .description('Dogfooding health metrics for the current .chitin')
    .option('--window-hours <n>', 'window size in hours', '24')
    .option('--chitin-dir <path>', 'override .chitin dir (default: resolve from cwd)')
    .action((opts: { windowHours: string; chitinDir?: string }) => {
      const chitinDir = opts.chitinDir ?? resolveChitinDir(process.cwd(), '');
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const res = spawnSync(
        kernelBin,
        ['health', '--dir', chitinDir, '--window-hours', opts.windowHours],
        { encoding: 'utf8' },
      );
      if (res.status !== 0) {
        console.error(res.stderr);
        process.exit(res.status ?? 3);
      }
      const report = JSON.parse(res.stdout) as HealthReport;
      printReport(report, chitinDir);
      process.exit(exitCode(report));
    });
}

function printReport(r: HealthReport, chitinDir: string): void {
  const line = (label: string, value: string | number, status: 'pass' | 'warn' | 'fail') => {
    const tag = status === 'pass' ? '[PASS]' : status === 'warn' ? '[WARN]' : '[FAIL]';
    console.log(`${tag}  ${label.padEnd(28)} ${value}`);
  };

  console.log(`chitin health — ${chitinDir}`);
  line('events total', r.events_total, r.events_total > 0 ? 'pass' : 'warn');
  for (const [surface, count] of Object.entries(r.events_by_window)) {
    line(`  events / ${surface}`, count, count > 0 ? 'pass' : 'warn');
  }
  line('hook failures', r.hook_failure_count, r.hook_failure_count === 0 ? 'pass' : 'fail');
  line('schema drift', r.schema_drift_count, r.schema_drift_count === 0 ? 'pass' : 'fail');
  line('orphaned chains', r.orphaned_chains, r.orphaned_chains === 0 ? 'pass' : 'warn');
}

function exitCode(r: HealthReport): number {
  if (r.hook_failure_count > 0 || r.schema_drift_count > 0) return 1;
  return 0;
}
```

- [ ] **Step 2: Register in main.ts**

Add to `apps/cli/src/main.ts`:

```typescript
import { registerHealth } from './commands/health.js';
```

And call it:

```typescript
registerHealth(program);
```

- [ ] **Step 3: Smoke test**

Run (from the chitin repo root): `pnpm exec tsx apps/cli/src/main.ts health`
Expected: a `[PASS]` / `[WARN]` / `[FAIL]` table; exit code 0 or 1 depending on state.

- [ ] **Step 4: Commit**

```bash
git add apps/cli/src/commands/health.ts apps/cli/src/main.ts
git commit -m "feat(cli): chitin health with pass/warn/fail output"
```

---

## Phase D — Ledger Tooling

### Task D1: Seed the ledger directory and file

**Files:**
- Create: `docs/observations/governance-debt-ledger.md`
- Create: `docs/observations/retrospectives/.gitkeep`

- [ ] **Step 1: Create the ledger stub**

Create `docs/observations/governance-debt-ledger.md`:

```markdown
# Governance-Debt Ledger

Living ledger of findings produced by the weekly dogfooding review
(see `docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md`).

Every entry cites a captured trace (`chain_id:seq` or content-addressed
`this_hash`), classifies the finding into a triage lane, and tracks
graduation. No speculative entries.

---

## Entries

<!-- Append new entries here. IDs are stable; never renumber. -->

<!-- GDL-000 reserved — do not use. -->

<!-- First real entry starts at GDL-001. -->
```

- [ ] **Step 2: Create retrospectives placeholder**

```bash
mkdir -p docs/observations/retrospectives
touch docs/observations/retrospectives/.gitkeep
```

- [ ] **Step 3: Write an onboarding note for any private integration target — in a private workspace, not in this repo**

The originally-planned repo-specific onboarding note was removed from this repo per the chitin-is-OSS boundary rule (no private-company-specific content on any branch). For each private repo the user clones onto this box that will capture chitin events, an onboarding note belongs in the user's own private workspace (not in `chitinhq/chitin`). The note should cover:

1. Add `.chitin/` to the private repo's `.gitignore` before the first session.
2. Verify after first session: `git status -s .chitin` should report nothing staged.
3. Ledger entries referencing private sessions use stable refs (`chain_id:seq` or `this_hash`); do not paste verbatim trace content that could identify internal logic. Paraphrase.

No chitin-side install is needed in the private repo — the Claude Code hook is user-level, so capture happens automatically.

- [ ] **Step 4: Commit**

```bash
git add docs/observations/
git commit -m "docs(observations): seed ledger file"
```

---

### Task D2: `chitin ledger new <lane>` CLI command

**Files:**
- Create: `apps/cli/src/commands/ledger-new.ts`
- Modify: `apps/cli/src/main.ts`

- [ ] **Step 1: Write the command**

Create `apps/cli/src/commands/ledger-new.ts`:

```typescript
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';

const LEDGER_REL = 'docs/observations/governance-debt-ledger.md';

export function registerLedgerNew(program: Command): void {
  const ledger = program.command('ledger').description('Governance-debt ledger tools');

  ledger
    .command('new')
    .description('Append a new stub entry to the ledger')
    .argument('<lane>', 'lane: 1 | 2 | 3 (fix / determinism / soul-routing)')
    .option('--chain <id>', 'chain_id reference (stable)')
    .option('--seq <n>', 'seq number within chain')
    .option('--hash <sha>', 'this_hash (first 12 chars are kept)')
    .option('--surface <s>', 'surface (e.g. claude-code)', 'claude-code')
    .option('--repo <name>', 'repo name (e.g. chitin)', 'chitin')
    .option('--soul <id>', 'active soul id', 'davinci')
    .option('--ledger <path>', 'ledger path (default: repo-root)', LEDGER_REL)
    .action(
      (
        laneArg: string,
        opts: {
          chain?: string;
          seq?: string;
          hash?: string;
          surface: string;
          repo: string;
          soul: string;
          ledger: string;
        },
      ) => {
        const laneLabel = laneLabelFor(laneArg);
        const path = opts.ledger;
        const body = readFileSync(path, 'utf8');
        const nextId = nextGDL(body);
        const today = new Date().toISOString().slice(0, 10);
        const entry = [
          '',
          `### GDL-${pad(nextId)} — <one-line what the platform should have caught>`,
          '',
          `- **Observed:** ${today}, chain \`${opts.chain ?? '<chain_id>'}\`, seq \`${opts.seq ?? '<n>'}\`, hash \`${(opts.hash ?? '<this_hash>').slice(0, 12)}\``,
          `- **Surface / repo:** ${opts.surface} / ${opts.repo}`,
          '- **Finding:** <what happened, one paragraph>',
          `- **Lane:** ${laneLabel}`,
          '- **Severity:** low / medium / high',
          '- **Graduated:** <null>',
          `- **Soul active:** ${opts.soul} @ <soul_hash[:8]>`,
          '',
        ].join('\n');
        writeFileSync(path, body.trimEnd() + '\n' + entry);
        console.log(`appended GDL-${pad(nextId)} to ${path}`);
      },
    );
}

function laneLabelFor(lane: string): string {
  if (lane === '1' || lane.toLowerCase() === 'fix') return '① FIX';
  if (lane === '2' || lane.toLowerCase() === 'determinism') return '② DETERMINISM';
  if (lane === '3' || lane.toLowerCase() === 'soul-routing' || lane.toLowerCase() === 'soul') return '③ SOUL ROUTING';
  throw new Error(`unknown lane: ${lane}`);
}

function nextGDL(body: string): number {
  const matches = body.matchAll(/^### GDL-(\d+)/gm);
  let max = 0;
  for (const m of matches) {
    const n = parseInt(m[1], 10);
    if (n > max) max = n;
  }
  return max + 1;
}

function pad(n: number): string {
  return n.toString().padStart(3, '0');
}
```

- [ ] **Step 2: Register in main.ts**

```typescript
import { registerLedgerNew } from './commands/ledger-new.js';
```

and

```typescript
registerLedgerNew(program);
```

- [ ] **Step 3: Smoke run**

```bash
pnpm exec tsx apps/cli/src/main.ts ledger new 1 \
  --chain test-chain-abc --seq 5 --hash deadbeefcafebabe
```

Expected: `appended GDL-001 to docs/observations/governance-debt-ledger.md`. Verify with `tail -20 docs/observations/governance-debt-ledger.md`.

Then revert the change (we only wanted to smoke-test):

```bash
git checkout docs/observations/governance-debt-ledger.md
```

- [ ] **Step 4: Commit**

```bash
git add apps/cli/src/commands/ledger-new.ts apps/cli/src/main.ts
git commit -m "feat(cli): chitin ledger new — stub entry scaffolder"
```

---

### Task D3: `chitin ledger lint` command

**Files:**
- Create: `apps/cli/src/commands/ledger-lint.ts`
- Modify: `apps/cli/src/commands/ledger-new.ts` — extract shared `ledger` subcommand registration (or create a unified `ledger.ts` that owns both)

To keep things DRY, reorganize as follows:

- [ ] **Step 1: Extract the shared `ledger` command registration**

Replace the content of `apps/cli/src/commands/ledger-new.ts` by moving ledger registration into a single new file `apps/cli/src/commands/ledger.ts` that owns both `new` and `lint`. Then delete `ledger-new.ts`.

Create `apps/cli/src/commands/ledger.ts`:

```typescript
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import Database from 'better-sqlite3';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import type { Command } from 'commander';

const LEDGER_REL = 'docs/observations/governance-debt-ledger.md';

export function registerLedger(program: Command): void {
  const ledger = program.command('ledger').description('Governance-debt ledger tools');

  ledger
    .command('new')
    .description('Append a new stub entry to the ledger')
    .argument('<lane>', 'lane: 1 | 2 | 3')
    .option('--chain <id>', 'chain_id reference')
    .option('--seq <n>', 'seq number within chain')
    .option('--hash <sha>', 'this_hash')
    .option('--surface <s>', 'surface', 'claude-code')
    .option('--repo <name>', 'repo', 'chitin')
    .option('--soul <id>', 'active soul', 'davinci')
    .option('--ledger <path>', 'ledger path', LEDGER_REL)
    .action(handleNew);

  ledger
    .command('lint')
    .description('Validate ledger entry integrity')
    .option('--ledger <path>', 'ledger path', LEDGER_REL)
    .option('--db <path>', 'events.db path for trace_ref resolution')
    .option('--strict', 'fail on unresolved trace refs', false)
    .action(handleLint);
}

function handleNew(
  laneArg: string,
  opts: {
    chain?: string;
    seq?: string;
    hash?: string;
    surface: string;
    repo: string;
    soul: string;
    ledger: string;
  },
): void {
  const laneLabel = laneLabelFor(laneArg);
  const body = readFileSync(opts.ledger, 'utf8');
  const nextId = nextGDL(body);
  const today = new Date().toISOString().slice(0, 10);
  const entry = [
    '',
    `### GDL-${pad(nextId)} — <one-line what the platform should have caught>`,
    '',
    `- **Observed:** ${today}, chain \`${opts.chain ?? '<chain_id>'}\`, seq \`${opts.seq ?? '<n>'}\`, hash \`${(opts.hash ?? '<this_hash>').slice(0, 12)}\``,
    `- **Surface / repo:** ${opts.surface} / ${opts.repo}`,
    '- **Finding:** <what happened, one paragraph>',
    `- **Lane:** ${laneLabel}`,
    '- **Severity:** low / medium / high',
    '- **Graduated:** <null>',
    `- **Soul active:** ${opts.soul} @ <soul_hash[:8]>`,
    '',
  ].join('\n');
  writeFileSync(opts.ledger, body.trimEnd() + '\n' + entry);
  console.log(`appended GDL-${pad(nextId)} to ${opts.ledger}`);
}

interface LintFinding {
  level: 'error' | 'warn';
  gdl: string;
  msg: string;
}

function handleLint(opts: { ledger: string; db?: string; strict: boolean }): void {
  const body = readFileSync(opts.ledger, 'utf8');
  const findings: LintFinding[] = [];

  // 1. ID uniqueness
  const ids = new Set<string>();
  const entries = [...body.matchAll(/^### (GDL-\d+)\b.*$/gm)];
  for (const m of entries) {
    const id = m[1];
    if (ids.has(id)) {
      findings.push({ level: 'error', gdl: id, msg: 'duplicate ID' });
    }
    ids.add(id);
  }

  // 2. Per-entry field presence
  const blocks = splitEntries(body);
  for (const b of blocks) {
    if (!/\*\*Observed:\*\*/.test(b.body)) {
      findings.push({ level: 'error', gdl: b.id, msg: 'missing Observed field' });
    }
    if (!/\*\*Lane:\*\*\s+[①②③]/.test(b.body)) {
      findings.push({ level: 'error', gdl: b.id, msg: 'missing or malformed Lane' });
    }
    if (!/\*\*Graduated:\*\*/.test(b.body)) {
      findings.push({ level: 'warn', gdl: b.id, msg: 'missing Graduated field' });
    }
  }

  // 3. trace_ref resolution (chain_id exists in events.db)
  if (opts.db && existsSync(opts.db)) {
    try {
      const db = new Database(opts.db, { readonly: true });
      const stmt = db.prepare('SELECT 1 FROM chain_index WHERE chain_id = ? LIMIT 1');
      for (const b of blocks) {
        const chainMatch = /chain\s+`([^`]+)`/.exec(b.body);
        if (!chainMatch) continue;
        if (chainMatch[1].startsWith('<')) continue; // placeholder
        const row = stmt.get(chainMatch[1]);
        if (!row) {
          findings.push({
            level: opts.strict ? 'error' : 'warn',
            gdl: b.id,
            msg: `chain_id not in events.db: ${chainMatch[1]}`,
          });
        }
      }
      db.close();
    } catch (err) {
      findings.push({ level: 'warn', gdl: '*', msg: `events.db open failed: ${String(err)}` });
    }
  }

  // 4. graduated markers resolve to real GH issues/PRs
  for (const b of blocks) {
    const m = /\*\*Graduated:\*\*.*?(issue|PR|souls PR)\s*#(\d+)/i.exec(b.body);
    if (!m) continue;
    const num = m[2];
    const gh = spawnSync('gh', ['issue', 'view', num, '--json', 'number'], { encoding: 'utf8' });
    if (gh.status !== 0) {
      // Try PR as fallback
      const pr = spawnSync('gh', ['pr', 'view', num, '--json', 'number'], { encoding: 'utf8' });
      if (pr.status !== 0) {
        findings.push({ level: 'warn', gdl: b.id, msg: `graduated marker #${num} not resolvable via gh` });
      }
    }
  }

  // Print findings
  for (const f of findings) {
    const tag = f.level === 'error' ? 'ERROR' : 'WARN';
    console.log(`[${tag}]  ${f.gdl.padEnd(10)} ${f.msg}`);
  }
  const errors = findings.filter((f) => f.level === 'error').length;
  console.log(`\n${findings.length} findings (${errors} errors)`);
  process.exit(errors > 0 ? 1 : 0);
}

function splitEntries(body: string): { id: string; body: string }[] {
  const out: { id: string; body: string }[] = [];
  const lines = body.split('\n');
  let current: { id: string; body: string } | null = null;
  for (const line of lines) {
    const m = /^### (GDL-\d+)\b/.exec(line);
    if (m) {
      if (current) out.push(current);
      current = { id: m[1], body: '' };
      continue;
    }
    if (current) current.body += line + '\n';
  }
  if (current) out.push(current);
  return out;
}

function laneLabelFor(lane: string): string {
  if (lane === '1' || lane.toLowerCase() === 'fix') return '① FIX';
  if (lane === '2' || lane.toLowerCase() === 'determinism') return '② DETERMINISM';
  if (lane === '3' || lane.toLowerCase() === 'soul-routing' || lane.toLowerCase() === 'soul') return '③ SOUL ROUTING';
  throw new Error(`unknown lane: ${lane}`);
}

function nextGDL(body: string): number {
  const matches = body.matchAll(/^### GDL-(\d+)/gm);
  let max = 0;
  for (const m of matches) {
    const n = parseInt(m[1], 10);
    if (n > max) max = n;
  }
  return max + 1;
}

function pad(n: number): string {
  return n.toString().padStart(3, '0');
}
```

- [ ] **Step 2: Delete `ledger-new.ts`** (superseded)

```bash
rm apps/cli/src/commands/ledger-new.ts
```

- [ ] **Step 3: Update main.ts to use the new file**

Replace `import { registerLedgerNew }` with `import { registerLedger } from './commands/ledger.js';` and call `registerLedger(program)` instead of `registerLedgerNew(program)`.

- [ ] **Step 4: Smoke run**

```bash
# Add a stub entry
pnpm exec tsx apps/cli/src/main.ts ledger new 1
# Lint
pnpm exec tsx apps/cli/src/main.ts ledger lint
# Revert stub
git checkout docs/observations/governance-debt-ledger.md
```

Expected: lint reports structural warnings for the stub (placeholder values), exits 0 since warnings-only.

- [ ] **Step 5: Commit**

```bash
git add apps/cli/src/commands/ledger.ts apps/cli/src/main.ts
git rm apps/cli/src/commands/ledger-new.ts
git commit -m "feat(cli): chitin ledger lint — uniqueness, field presence, trace resolution, gh graduation check"
```

---

### Task D4: `chitin review --last <window>` aggregator

**Files:**
- Create: `apps/cli/src/commands/review.ts`
- Modify: `apps/cli/src/main.ts`

- [ ] **Step 1: Write the command**

Create `apps/cli/src/commands/review.ts`:

```typescript
import { spawnSync } from 'node:child_process';
import { resolveChitinDir } from '@chitin/contracts';
import type { Command } from 'commander';

interface HealthReport {
  events_total: number;
  events_by_window: Record<string, number>;
  hook_failure_count: number;
  schema_drift_count: number;
  orphaned_chains: number;
}

export function registerReview(program: Command): void {
  program
    .command('review')
    .description('Weekly review skim: health + recent session list')
    .option('--last <window>', 'window (e.g. 7d, 24h)', '7d')
    .action((opts: { last: string }) => {
      const hours = parseWindow(opts.last);
      const chitinDir = resolveChitinDir(process.cwd(), '');
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';

      console.log(`# chitin review — window: ${opts.last} (${hours}h)\n`);

      // 1. Health skim
      const h = spawnSync(kernelBin, ['health', '--dir', chitinDir, '--window-hours', String(hours)], {
        encoding: 'utf8',
      });
      if (h.status === 0) {
        const r = JSON.parse(h.stdout) as HealthReport;
        console.log(`## Health (${chitinDir})`);
        console.log(`- events total:      ${r.events_total}`);
        for (const [s, c] of Object.entries(r.events_by_window)) {
          console.log(`- events / ${s}:     ${c}`);
        }
        console.log(`- hook failures:     ${r.hook_failure_count}`);
        console.log(`- schema drift:      ${r.schema_drift_count}`);
        console.log(`- orphaned chains:   ${r.orphaned_chains}`);
        console.log('');
      } else {
        console.error(`health failed: ${h.stderr}`);
      }

      // 2. Recent sessions (uses existing events list)
      console.log(`## Recent sessions`);
      const list = spawnSync(
        'pnpm',
        ['exec', 'tsx', 'apps/cli/src/main.ts', 'events', 'list', '--limit', '20'],
        { encoding: 'utf8' },
      );
      console.log(list.stdout || '(no events)');
    });
}

function parseWindow(w: string): number {
  const m = /^(\d+)([hd])$/.exec(w);
  if (!m) throw new Error(`bad window: ${w}`);
  const n = parseInt(m[1], 10);
  return m[2] === 'd' ? n * 24 : n;
}
```

- [ ] **Step 2: Register in main.ts**

```typescript
import { registerReview } from './commands/review.js';
```

and

```typescript
registerReview(program);
```

- [ ] **Step 3: Smoke run**

```bash
pnpm exec tsx apps/cli/src/main.ts review --last 7d
```

Expected: markdown-style report with Health section and Recent sessions section. Exit 0.

- [ ] **Step 4: Commit**

```bash
git add apps/cli/src/commands/review.ts apps/cli/src/main.ts
git commit -m "feat(cli): chitin review — weekly skim aggregator"
```

---

## Phase E — GH Actions Composite Stub

### Task E1: Create the composite action

**Files:**
- Create: `.github/actions/observe/action.yml`

- [ ] **Step 1: Write the action**

Create `.github/actions/observe/action.yml`:

```yaml
name: chitin observe
description: Wrap a workflow job with chitin session_start / session_end events.
inputs:
  run-id:
    description: Unique run identifier (defaults to the GH run id)
    required: false
    default: ${{ github.run_id }}
  workspace:
    description: Workspace dir (defaults to GITHUB_WORKSPACE)
    required: false
    default: ${{ github.workspace }}
runs:
  using: composite
  steps:
    - name: chitin — session_start
      shell: bash
      run: |
        set -euo pipefail
        mkdir -p "${{ inputs.workspace }}/.chitin"
        SESSION_ID="gh-${{ inputs.run-id }}-${{ github.job }}"
        TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        cat >> "${{ inputs.workspace }}/.chitin/events.jsonl" <<EOF
        {"schema_version":"2","ts":"$TS","event_type":"session_start","surface":"gh-actions","session_id":"$SESSION_ID","chain_id":"$SESSION_ID","chain_type":"session","seq":0,"prev_hash":null,"this_hash":"","payload":{"cwd":"${{ inputs.workspace }}","client_info":{"name":"gh-actions","version":"${{ github.action_ref || 'unknown' }}"}}}
        EOF
        echo "CHITIN_SESSION_ID=$SESSION_ID" >> "$GITHUB_ENV"
    - name: chitin — session_end (always)
      if: always()
      shell: bash
      run: |
        set -euo pipefail
        TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        cat >> "${{ inputs.workspace }}/.chitin/events.jsonl" <<EOF
        {"schema_version":"2","ts":"$TS","event_type":"session_end","surface":"gh-actions","session_id":"${CHITIN_SESSION_ID:-unknown}","chain_id":"${CHITIN_SESSION_ID:-unknown}","chain_type":"session","seq":0,"prev_hash":null,"this_hash":"","payload":{"reason":"gh-actions workflow end"}}
        EOF
```

This is intentionally minimal: it emits raw JSONL without the Go kernel's hash-chain linkage. Hash linkage and full-envelope emission will be added once a kernel binary can be downloaded as part of the action.

- [ ] **Step 2: Commit**

```bash
git add .github/actions/observe/action.yml
git commit -m "feat(ci): gh-actions composite stub emits session_start/end to .chitin"
```

---

### Task E2: Wire chitin's own CI to use the composite action

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Find the primary test job**

Run: `rg "^\s*test:" .github/workflows/ci.yml`
Read the file to understand the job layout.

- [ ] **Step 2: Add an `observe` step at the top of each significant job**

Add, as the FIRST step of each job (after `checkout`):

```yaml
      - uses: ./.github/actions/observe
```

- [ ] **Step 3: Verify CI passes**

Run: `git push` or, if you want to dry-run, use `act` if available:

```bash
act -j test 2>&1 | head -50
```

Expected: test job runs, `.chitin/events.jsonl` appears in the workflow workspace, CI still passes.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: wire chitin's own CI through observe composite action"
```

---

## Phase F — openclaw Workstream (investigation + addendum spec)

Phase F is gated per the structural mitigations in the spec. If at any
point the gate fires (Task F4), stop this plan and spawn a follow-up plan.

### Task F1: Install openclaw locally and smoke-verify

**Files:**
- Modify: `libs/adapters/openclaw/SPIKE.md` → graduate to `README.md` with install section filled

- [ ] **Step 1: Find the install path**

Answer first: where does openclaw come from? Check:
- `https://github.com/openclaw/openclaw` (likely)
- `brew install openclaw` / `apt-get install openclaw`
- source build

Document whatever works. Write the install instructions to
`libs/adapters/openclaw/README.md` (create by copying and extending
`SPIKE.md`; keep SPIKE.md until the README is complete).

- [ ] **Step 2: Smoke-verify**

Run: `openclaw --help` (or equivalent). Capture stdout for the README.

- [ ] **Step 3: Commit**

```bash
git add libs/adapters/openclaw/README.md libs/adapters/openclaw/SPIKE.md
git commit -m "docs(openclaw): install path documented; smoke-verified locally"
```

---

### Task F2: Answer the 4 SPIKE questions by observation

**Files:**
- Modify: `libs/adapters/openclaw/README.md` — fill in observation answers

- [ ] **Step 1: Question 1 — Plugin/hook API vs process-level wrap**

Check openclaw's docs/source for a plugin API. Look for: a `plugins/` dir,
a `config.yaml` hook schema, a documented extension interface, a
webhook/event-bus entrypoint. Write findings to the README under
"Adapter strategy".

If no plugin API exists, document process-level wrapping: stdin/stdout
pipes, sidecar logs, exit-code semantics.

- [ ] **Step 2: Question 2 — Streams produced during a session**

Run openclaw on a small task with `strace -f -e trace=openat,write` or
equivalent. Capture:
- stdout content shape (text? JSON?)
- any log files written, with their paths
- any IPC (sockets, named pipes)

Document in README under "Observable streams".

- [ ] **Step 3: Question 3 — Session boundaries**

How does openclaw identify a session? Is it per-invocation, per-daemon,
per-user? Can a session span multiple commands?

Document in README under "Session semantics".

- [ ] **Step 4: Question 4 — Tool-call boundaries**

Does openclaw support tool calls (similar to Claude Code)? If so,
where's the decision/execution split observable?

Document in README under "Tool-call surface".

- [ ] **Step 5: Commit**

```bash
git add libs/adapters/openclaw/README.md
git commit -m "docs(openclaw): answer the 4 SPIKE questions from observation"
```

---

### Task F3: Write the adapter-implementation design addendum

**Files:**
- Create: `docs/superpowers/specs/YYYY-MM-DD-openclaw-adapter-implementation-design.md` (use today's date)

- [ ] **Step 1: Write the addendum**

Template — fill in the specifics from Task F2 findings:

```markdown
# openclaw Adapter Implementation — Design Addendum

**Date:** YYYY-MM-DD
**Supplements:** docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md (Phase F).
**Status:** Ready for a follow-up implementation plan.

## One-sentence invariant (Knuth gate)

<Write ONE sentence that states what the adapter guarantees. Example:
"Every openclaw process invocation produces exactly one session_start
event at entry and one session_end event at exit, linked by chain_id.">

## Adapter strategy

<hook-api | log-tailing | process-wrap — pick based on Task F2 findings>

## Events emitted (v1 of capture)

- session_start: <when fires? what payload?>
- session_end: <when fires? what payload?>
- <optional single inner event type, only if an obvious hook point exists>

## Cost estimate (Socrates gate)

- Elapsed-effort estimate: N days (with explicit uncertainty range).
- If N > 5 days, STOP — do NOT author an implementation plan inside this
  workstream. Spawn a follow-up brainstorm → spec → plan cycle with
  scope-narrowed implementation instead.

## Out of scope for v1 capture

- Full tool-call parity with Claude Code.
- Cross-surface policy comparison (that's a Lane ② ledger finding, not a
  build task).

## Open risks

<list the top 2-3 risks from Task F2 findings>
```

- [ ] **Step 2: Commit the addendum**

```bash
git add docs/superpowers/specs/YYYY-MM-DD-openclaw-adapter-implementation-design.md
git commit -m "spec: openclaw adapter implementation design addendum"
```

---

### Task F4: Socrates gate — cost trip-wire

- [ ] **Step 1: Read the cost estimate from the addendum**

Open the addendum from F3. Read the `Cost estimate` section.

- [ ] **Step 2: If estimate > 5 days, STOP**

If the estimate exceeds 5 elapsed days:
- Do NOT add implementation tasks to this plan.
- Edit the parent spec (`docs/superpowers/specs/2026-04-19-dogfood-debt-ledger-design.md`) to mark Phase F as "split into follow-up plan" and link the addendum.
- Commit: `docs(spec): openclaw work split into follow-up plan (Socrates gate)`.
- Declare this plan complete after Phase E.

- [ ] **Step 3: If estimate ≤ 5 days, continue to Task F5**

If within budget, proceed.

---

### Task F5 (conditional on F4 passing): Implement minimum viable capture

Implementation tasks are filled in by the addendum from F3. Because the
addendum's content is knowable only after F2's investigation, this plan
does not pre-commit to a specific task list here. The implementation
tasks are added as an edit to this plan at the time F4 passes — or,
equivalently, a follow-up plan is written from the addendum.

Expected shape (tentative):
- TDD: unit test for session_start emission on entry
- TDD: unit test for session_end emission on exit (including error exit)
- Integration test: real `openclaw` invocation produces a 2-event chain
- Wire `chitin install --surface openclaw` if an install path exists

No code is written until the addendum exists.

---

## Final Verification

### Task Z1: End-to-end smoke

- [ ] **Step 1: Fresh install path**

```bash
rm -f ~/.local/bin/chitin-kernel
pnpm install-kernel
pnpm exec tsx apps/cli/src/main.ts install --surface claude-code --global
```

Expected: installs cleanly; `verify: OK` prints.

- [ ] **Step 2: Real session capture**

Run a fresh `claude -p "say hi"` from this box. Then:

```bash
pnpm exec tsx apps/cli/src/main.ts health
pnpm exec tsx apps/cli/src/main.ts events list --limit 5
```

Expected: health shows events > 0 for claude-code; events list includes a session_start for the "say hi" session.

- [ ] **Step 3: Ledger proof-of-life (GDL-001)**

Write the first real ledger entry manually as a sanity pass. The entry
should be a *meta* finding: something you noticed about the install/
capture process itself that a platform-level governor should have caught
(e.g., "install reported ok but I couldn't tell which hooks were already
taken by other tools — should warn on conflicts"). This is the first
actual dogfooding finding.

Run: `pnpm exec tsx apps/cli/src/main.ts ledger lint`
Expected: exits 0 with no errors (warnings OK for soul_hash placeholder).

- [ ] **Step 4: Commit the first entry**

```bash
git add docs/observations/governance-debt-ledger.md
git commit -m "observations: GDL-001 — first live ledger entry (dogfooding proof-of-life)"
```

---

## Self-Review

### Spec coverage check

| Spec section | Covered in plan |
|---|---|
| Capture architecture: install mechanism (claude-code) | Phase B |
| Capture architecture: events landing (walk-up + orphan) | Phase A2, A3 |
| Capture architecture: privacy (defer redaction, `.chitin/` gitignore in any private work repo) | Phase D1 (onboarding note tracked in a private workspace, not in this repo), no code needed |
| Capture architecture: openclaw workstream | Phase F |
| Capture architecture: Copilot CLI | Out of scope (documented extension path; no tasks) |
| Capture architecture: GH Actions composite | Phase E |
| Ledger: location + entry shape | Phase D1, D2 |
| Review cadence: tooling (review / ledger new / ledger lint) | Phase D2, D3, D4 |
| Trip-wires & soul handoff | Process (not code) — documented in spec; trip-wire F4 codified as Task |
| Outputs & graduation | `chitin ledger lint`'s graduation-marker check (Phase D3) |
| Validation: install verification | Phase B5 `verifyClaudeCodeInstall` + B6 e2e |
| Validation: self-telemetry (`chitin health`) | Phase C |
| Validation: ledger health | Phase D3 (`chitin ledger lint`) |
| Validation: graduation proof | Phase D3 checks graduated markers |
| Validation: quarterly audit | Process — scheduled in spec, no code |
| Anti-Hawthorne guard | Documented in spec; no code |

Gaps found in self-review:
- Spec mentions `chitin health` running as a timer/cron as a deferred option — I kept it manual, matching the spec's "lean manual for v1" note.
- Spec mentions "merge semantics for conflicting hooks" as an open question — Phase B2's `InstallGlobal` appends without overwriting; the verification prints a clean OK but does not explicitly warn on pre-existing non-chitin hooks. This is a reasonable v1. A future Lane ② ledger finding might call for surfacing the conflict.

### Placeholder scan

- No `TBD` or `TODO` literals left in the plan.
- Task F5 is explicitly conditional on Task F4 passing; per the skill rule, it does not contain pre-committed code. If F4 passes, F5 becomes a follow-up plan amendment or a separate plan.
- Task F3 contains a template with `<angle-bracket placeholders>` — those are intentional (they're what Task F2's investigation fills in). They are not plan-level TODOs.
- Task E2's `act` dry-run is optional; the real validation is CI green on push.

### Type consistency

- `resolveChitinDir(cwd, workspaceBoundary)` signature is identical in Go (`Resolve(cwd, workspaceBoundary string)`) and TS.
- `HealthReport` shape matches between Go (`Report` struct with `json:"..."` tags) and TS (`HealthReport` interface).
- `SubscribedHooks` (Go) is the single source of truth for which Claude Code events chitin forwards; the adapter bin reuses `runHook` which already handles them.
- Adapter binary path passed to `InstallGlobal` / `UninstallGlobal` is the same string used for dedup (by equality) and removal — symmetric.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-04-19-dogfood-debt-ledger.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.

**Which approach?**
