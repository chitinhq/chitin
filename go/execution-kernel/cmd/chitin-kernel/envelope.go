package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// cmdEnvelope dispatches `envelope <op> [flags]`. Operator-facing surface
// for managing BudgetEnvelopes (Milestone E of cost-governance kernel v3).
//
// Subcommands:
//   create   --calls=N --bytes=N [--usd=N]   → emits ULID
//   use      <id>                            → atomic write of ~/.chitin/current-envelope
//   inspect  <id>                            → JSON snapshot
//   list     [--limit=N]                     → JSON array (most recent first)
//   grant    <id> [--calls=N] [--bytes=N] [--usd=N] [--reason=...]
//                                            → raise caps + reopen + audit row
//   close    <id>                            → idempotent close
//
// All subcommands share the same gov.db at ~/.chitin/gov.db; CHITIN_HOME
// override exists for tests.
func cmdEnvelope(args []string) {
	if len(args) < 1 {
		exitErr("envelope_no_subcommand", "usage: chitin-kernel envelope {create|use|inspect|list|grant|close} [flags]")
	}
	op := args[0]
	rest := args[1:]
	switch op {
	case "create":
		cmdEnvelopeCreate(rest)
	case "use":
		cmdEnvelopeUse(rest)
	case "inspect":
		cmdEnvelopeInspect(rest)
	case "list":
		cmdEnvelopeList(rest)
	case "grant":
		cmdEnvelopeGrant(rest)
	case "close":
		cmdEnvelopeClose(rest)
	case "tail":
		cmdEnvelopeTail(rest)
	default:
		exitErr("envelope_unknown_subcommand", op)
	}
}

// openBudgetStore opens ~/.chitin/gov.db, creating the parent dir if
// needed. Mirrors openStateCounter; both touch the same db file under WAL.
func openBudgetStore() (*gov.BudgetStore, string, error) {
	dir := chitinDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, dir, fmt.Errorf("mkdir state dir: %w", err)
	}
	store, err := gov.OpenBudgetStore(filepath.Join(dir, "gov.db"))
	if err != nil {
		return nil, dir, err
	}
	return store, dir, nil
}

// splitPositionalID returns the leading positional id (first arg if it
// doesn't start with "-") plus the remaining flag args. Returns "" id
// when no positional is present, signaling the caller to surface a
// usage error.
func splitPositionalID(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	if strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

func cmdEnvelopeCreate(args []string) {
	fs := flag.NewFlagSet("envelope create", flag.ExitOnError)
	calls := fs.Int64("calls", 0, "MaxToolCalls cap (0 = uncapped)")
	bytes := fs.Int64("bytes", 0, "MaxInputBytes cap (0 = uncapped)")
	usd := fs.Float64("usd", 0, "BudgetUSD informational cap")
	fs.Parse(args)
	if *calls < 0 || *bytes < 0 || *usd < 0 {
		exitErr("envelope_create_negative", "--calls, --bytes, --usd must be non-negative")
	}
	store, _, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_create_open", err.Error())
	}
	defer store.Close()
	env, err := store.Create(gov.BudgetLimits{
		MaxToolCalls:  *calls,
		MaxInputBytes: *bytes,
		BudgetUSD:     *usd,
	})
	if err != nil {
		exitErr("envelope_create", err.Error())
	}
	// One ULID per line on stdout — scripting-friendly. The newline lets
	// `envelope use $(envelope create ...)` work without a chomp.
	fmt.Println(env.ID)
}

func cmdEnvelopeUse(args []string) {
	fs := flag.NewFlagSet("envelope use", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		exitErr("envelope_use_args", "usage: chitin-kernel envelope use <id>")
	}
	id := fs.Arg(0)
	store, dir, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_use_open", err.Error())
	}
	defer store.Close()
	if _, err := store.Load(id); err != nil {
		if errors.Is(err, gov.ErrEnvelopeNotFound) {
			exitErr("envelope_not_found", id)
		}
		exitErr("envelope_use_load", err.Error())
	}
	if err := writeCurrentEnvelope(dir, id); err != nil {
		exitErr("envelope_use_write", err.Error())
	}
}

// writeCurrentEnvelope writes id+'\n' to <dir>/current-envelope atomically:
// write to a tmp file in the same directory, fsync, rename over the target.
//
// Invariant: a concurrent reader of <dir>/current-envelope sees either the
// previous content or the new content in full — never a partial/empty
// interim. rename(2) is atomic on POSIX when src and dst are on the same
// filesystem; placing tmp in the same directory guarantees that.
//
// On concurrent writers: every writer rename()s its own tmp file. The
// final visible content is whichever rename happened last. Each writer's
// content is internally consistent.
func writeCurrentEnvelope(dir, id string) error {
	target := filepath.Join(dir, "current-envelope")
	tmp, err := os.CreateTemp(dir, "current-envelope.tmp.*")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	cleanup = false
	return nil
}

func cmdEnvelopeInspect(args []string) {
	fs := flag.NewFlagSet("envelope inspect", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		exitErr("envelope_inspect_args", "usage: chitin-kernel envelope inspect <id>")
	}
	id := fs.Arg(0)
	store, _, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_inspect_open", err.Error())
	}
	defer store.Close()
	env, err := store.Load(id)
	if err != nil {
		if errors.Is(err, gov.ErrEnvelopeNotFound) {
			exitErr("envelope_not_found", id)
		}
		exitErr("envelope_inspect_load", err.Error())
	}
	st, err := env.Inspect()
	if err != nil {
		exitErr("envelope_inspect", err.Error())
	}
	b, err := json.Marshal(st)
	if err != nil {
		exitErr("envelope_inspect_marshal", err.Error())
	}
	fmt.Println(string(b))
}

func cmdEnvelopeList(args []string) {
	fs := flag.NewFlagSet("envelope list", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max envelopes to return (0 = no limit)")
	fs.Parse(args)
	if *limit < 0 {
		exitErr("envelope_list_limit", "--limit must be non-negative")
	}
	store, _, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_list_open", err.Error())
	}
	defer store.Close()
	envs, err := store.List(*limit)
	if err != nil {
		exitErr("envelope_list", err.Error())
	}
	// Always emit a JSON array, never null — empty result = "[]".
	if envs == nil {
		envs = []gov.EnvelopeState{}
	}
	b, err := json.Marshal(envs)
	if err != nil {
		exitErr("envelope_list_marshal", err.Error())
	}
	fmt.Println(string(b))
}

func cmdEnvelopeGrant(args []string) {
	// Stdlib `flag` stops parsing at the first non-flag arg, so we peel
	// the positional <id> off the front before parsing flags. This lets
	// operators write `envelope grant <id> --calls=10` (id-first feels
	// natural) without forcing flags-before-positional ordering.
	id, rest := splitPositionalID(args)
	if id == "" {
		exitErr("envelope_grant_args", "usage: chitin-kernel envelope grant <id> [--calls=N] [--bytes=N] [--usd=N] [--reason=...]")
	}
	fs := flag.NewFlagSet("envelope grant", flag.ExitOnError)
	calls := fs.Int64("calls", 0, "delta to add to MaxToolCalls")
	bytes := fs.Int64("bytes", 0, "delta to add to MaxInputBytes")
	usd := fs.Float64("usd", 0, "delta to add to BudgetUSD (informational)")
	reason := fs.String("reason", "", "operator-supplied reason recorded in envelope_grants and audit log")
	fs.Parse(rest)
	store, dir, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_grant_open", err.Error())
	}
	defer store.Close()
	env, err := store.Load(id)
	if err != nil {
		if errors.Is(err, gov.ErrEnvelopeNotFound) {
			exitErr("envelope_not_found", id)
		}
		exitErr("envelope_grant_load", err.Error())
	}
	if err := env.Grant(*calls, *bytes, *usd, *reason); err != nil {
		exitErr("envelope_grant", err.Error())
	}
	// Append an operator-grant row to the daily audit log so a later
	// `envelope tail` can surface it alongside agent decisions. Action
	// shape: type "operator.grant", target = envelope id. The closed
	// ActionType enum is for agent-proposed actions; operator-grant is
	// out-of-band, so it lands as a string here without expanding the
	// enum (audit readers treat action_type as opaque).
	audit := gov.Decision{
		Allowed:    true,
		Mode:       "enforce",
		RuleID:     "operator-grant",
		Reason:     fmtGrantDelta(*calls, *bytes, *usd, *reason),
		Action:     gov.Action{Type: gov.ActionType("operator.grant"), Target: id},
		Agent:      "operator",
		Ts:         time.Now().UTC().Format(time.RFC3339),
		EnvelopeID: id,
		ToolCalls:  *calls,
		InputBytes: *bytes,
		CostUSD:    *usd,
	}
	if err := gov.WriteLog(audit, dir); err != nil {
		// Audit-write failure isn't fatal — the grant landed in sqlite.
		// Surface on stderr so operators see it but exit 0.
		fmt.Fprintf(os.Stderr, `{"warning":"audit_write_failed","error":%q}`+"\n", err.Error())
	}
}

func fmtGrantDelta(calls, bytes int64, usd float64, reason string) string {
	parts := []string{}
	if calls != 0 {
		parts = append(parts, fmt.Sprintf("calls=%+d", calls))
	}
	if bytes != 0 {
		parts = append(parts, fmt.Sprintf("bytes=%+d", bytes))
	}
	if usd != 0 {
		parts = append(parts, fmt.Sprintf("usd=%+f", usd))
	}
	out := "grant"
	if len(parts) > 0 {
		out += " " + strings.Join(parts, " ")
	}
	if reason != "" {
		out += " (" + reason + ")"
	}
	return out
}

func cmdEnvelopeClose(args []string) {
	fs := flag.NewFlagSet("envelope close", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		exitErr("envelope_close_args", "usage: chitin-kernel envelope close <id>")
	}
	id := fs.Arg(0)
	store, _, err := openBudgetStore()
	if err != nil {
		exitErr("envelope_close_open", err.Error())
	}
	defer store.Close()
	env, err := store.Load(id)
	if err != nil {
		if errors.Is(err, gov.ErrEnvelopeNotFound) {
			exitErr("envelope_not_found", id)
		}
		exitErr("envelope_close_load", err.Error())
	}
	if err := env.CloseEnvelope(); err != nil {
		exitErr("envelope_close", err.Error())
	}
}
