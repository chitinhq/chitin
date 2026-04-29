package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/govhookinstall"
)

// cmdInstallClaudeCodeHook handles `install claude-code-hook [--global|
// --project] [--dry-run] [--envelope=<id>] [--require-policy]`.
//
// Emits the canonical hook command into ~/.claude/settings.json (global)
// or .claude/settings.json (project). Writes a one-time pre-chitin
// backup the first time it touches the file. Idempotent on re-run.
//
// Flags worth knowing:
//
//	--envelope=<id>     embeds the envelope into the hook command line.
//	                    Pins until reinstall; for switchable scope use
//	                    `chitin-kernel envelope use <id>` and the
//	                    ~/.chitin/current-envelope file pattern.
//	--require-policy    embeds --require-policy into the hook command,
//	                    flipping no-policy-in-cwd from fail-open to
//	                    fail-closed (block). Default off so operators
//	                    can run `claude` in arbitrary dirs.
func cmdInstallClaudeCodeHook(args []string) {
	fs := flag.NewFlagSet("install claude-code-hook", flag.ExitOnError)
	global := fs.Bool("global", false, "install to ~/.claude/settings.json")
	project := fs.Bool("project", false, "install to ./.claude/settings.json (cwd)")
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	envelope := fs.String("envelope", "", "envelope ID to embed in the hook command (pins until reinstall)")
	requirePolicy := fs.Bool("require-policy", false, "embed --require-policy in the hook command (no-policy cwd → block, not allow)")
	fs.Parse(args)

	if *global == *project {
		exitErr("install_scope", "exactly one of --global or --project required")
	}

	scope := govhookinstall.ScopeGlobal
	if *project {
		scope = govhookinstall.ScopeProject
	}

	command := govhookinstall.HookCommand
	if *envelope != "" {
		command += " --envelope=" + *envelope
	}
	if *requirePolicy {
		command += " --require-policy"
	}

	cwd, err := os.Getwd()
	if err != nil {
		exitErr("install_cwd", err.Error())
	}

	if *dryRun {
		plan, err := govhookinstall.DryRun(scope, cwd, command)
		if err != nil {
			exitErr("install_dry_run", err.Error())
		}
		emitJSON(map[string]any{
			"ok":              true,
			"dry_run":         true,
			"path":            plan.Path,
			"backup":          plan.Backup,
			"backup_exists":   plan.BackupExists,
			"would_write":     plan.WouldWrite,
			"preserved_count": plan.PreservedCount,
			"command":         plan.WrapperCommand,
			"notes":           installNotes(*envelope, *requirePolicy),
		})
		return
	}

	path, backup, err := govhookinstall.Install(scope, cwd, command)
	if err != nil {
		exitErr("install_failed", err.Error())
	}
	emitJSON(map[string]any{
		"ok":      true,
		"path":    path,
		"backup":  backup,
		"command": command,
		"notes":   installNotes(*envelope, *requirePolicy),
	})
}

// installNotes is the loud-message channel: surfaced on stdout JSON
// alongside the install result so the operator sees the consequences
// of their flag choices (or non-choices) without having to read the
// docs first. Reviewer-driven (PR #65 review #1, #4, #7).
func installNotes(envelope string, requirePolicy bool) []string {
	var notes []string
	if !requirePolicy {
		notes = append(notes, "no-policy fallback: tool calls in directories without chitin.yaml will be ALLOWED with a stderr warning. Reinstall with --require-policy to fail closed.")
	} else {
		notes = append(notes, "strict mode: tool calls in directories without chitin.yaml will be BLOCKED. Scaffold a chitin.yaml in any cwd you run `claude` from.")
	}
	if envelope != "" {
		notes = append(notes, "envelope pinned in hook command: this envelope is in effect for every claude session until you reinstall the hook. For switchable contexts, drop --envelope and use `chitin-kernel envelope use <id>` instead.")
	}
	notes = append(notes, "coexistence: this hook does NOT replace the chain-recording adapter installed via `chitin-kernel install --surface=claude-code`. Both can coexist; both will fire on each tool call.")
	return notes
}

// cmdUninstallClaudeCodeHook handles `uninstall claude-code-hook
// [--global|--project]`.
func cmdUninstallClaudeCodeHook(args []string) {
	fs := flag.NewFlagSet("uninstall claude-code-hook", flag.ExitOnError)
	global := fs.Bool("global", false, "uninstall from ~/.claude/settings.json")
	project := fs.Bool("project", false, "uninstall from ./.claude/settings.json (cwd)")
	fs.Parse(args)

	if *global == *project {
		exitErr("uninstall_scope", "exactly one of --global or --project required")
	}

	scope := govhookinstall.ScopeGlobal
	if *project {
		scope = govhookinstall.ScopeProject
	}
	cwd, err := os.Getwd()
	if err != nil {
		exitErr("uninstall_cwd", err.Error())
	}
	path, err := govhookinstall.Uninstall(scope, cwd)
	if err != nil {
		exitErr("uninstall_failed", err.Error())
	}
	emitJSON(map[string]any{"ok": true, "path": path})
}

func emitJSON(v map[string]any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
