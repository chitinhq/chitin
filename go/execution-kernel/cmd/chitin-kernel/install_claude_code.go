package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/govhookinstall"
)

// cmdInstallClaudeCodeHook handles `install claude-code-hook [--global|
// --project] [--dry-run] [--envelope=<id>]`.
//
// Emits the canonical hook command into ~/.claude/settings.json (global)
// or .claude/settings.json (project). Writes a one-time pre-chitin
// backup the first time it touches the file. Idempotent on re-run.
func cmdInstallClaudeCodeHook(args []string) {
	fs := flag.NewFlagSet("install claude-code-hook", flag.ExitOnError)
	global := fs.Bool("global", false, "install to ~/.claude/settings.json")
	project := fs.Bool("project", false, "install to ./.claude/settings.json (cwd)")
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	envelope := fs.String("envelope", "", "envelope ID to embed in the hook command (optional)")
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
	})
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
