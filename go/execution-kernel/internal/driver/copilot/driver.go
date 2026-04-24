package copilot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	copilotsdk "github.com/github/copilot-sdk/go"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// RunOpts configures a Run() invocation.
type RunOpts struct {
	Cwd         string
	Interactive bool
	Verbose     bool
}

// PreflightOpts configures a Preflight check.
type PreflightOpts struct {
	Cwd string
}

// Run starts one Copilot session, dispatches the prompt, and returns when
// the session ends (naturally, via lockdown, or via error).
//
// Invariant: returns nil when the session completes cleanly OR when lockdown
// terminates it (lockdown is correct operation, not an error).
// Returns non-nil for startup failures, SDK errors, or timeouts.
func Run(ctx context.Context, prompt string, opts RunOpts) error {
	// 1. Load policy (chitin.yaml search from cwd upward).
	policy, _, err := gov.LoadWithInheritance(opts.Cwd)
	if err != nil {
		return fmt.Errorf("policy load: %w", err)
	}

	// 2. Open Counter (SQLite escalation state in ~/.chitin/gov.db).
	chitinDir := defaultChitinDir()
	if err := os.MkdirAll(chitinDir, 0755); err != nil {
		return fmt.Errorf("chitin dir: %w", err)
	}
	counter, err := gov.OpenCounter(filepath.Join(chitinDir, "gov.db"))
	if err != nil {
		return fmt.Errorf("counter open: %w", err)
	}
	defer counter.Close()

	// 3. Assemble Gate (struct literal — no constructor in gov package).
	gate := &gov.Gate{
		Policy:  policy,
		Counter: counter,
		LogDir:  chitinDir,
		Cwd:     opts.Cwd,
	}

	// 4. Construct and start client.
	client, err := NewClient(ClientOpts{})
	if err != nil {
		return fmt.Errorf("client init: %w", err)
	}
	defer client.Close()

	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("client start: %w", err)
	}

	// 5. Wire handler. LockdownCh receives lockdown signals from
	// OnPermissionRequest so Run can detect and terminate cleanly — the
	// SDK's executePermissionAndRespond discards handler errors, so the
	// signal must travel via a side channel.
	lockdownCh := make(chan *LockdownError, 1)
	handler := &Handler{
		Gate:       gate,
		Agent:      "copilot-cli",
		Cwd:        opts.Cwd,
		LockdownCh: lockdownCh,
	}

	// 6. Create session with handler registered.
	// Model: "gpt-4.1" is required for reliable tool-use in headless ACP mode.
	// The spike's Rung 4 confirmed this model calls the shell tool in response
	// to shell-action prompts; the default model applies its own safety filters
	// and declines tool calls for risky prompts (rm -rf, curl|bash) without
	// ever firing OnPermissionRequest, which bypasses chitin governance entirely.
	// AvailableTools: nil = all built-in tools remain available so the model
	// can call shell, file-read, file-write, and other Copilot built-ins.
	session, err := client.SDKClient().CreateSession(ctx, &copilotsdk.SessionConfig{
		Model:               "gpt-4.1",
		OnPermissionRequest: handler.OnPermissionRequest,
	})
	if err != nil {
		return fmt.Errorf("session create: %w", err)
	}
	defer session.Disconnect() //nolint:errcheck

	// 7. Dispatch.
	if opts.Interactive {
		return runInteractive(ctx, session)
	}

	// Run the prompt via SendAndWait. While it blocks, the permission
	// handler may fire and signal lockdown via lockdownCh. We run
	// SendAndWait in a goroutine and race it against the lockdown channel.
	type sendResult struct {
		event *copilotsdk.SessionEvent
		err   error
	}
	sendDone := make(chan sendResult, 1)
	go func() {
		evt, err := session.SendAndWait(ctx, copilotsdk.MessageOptions{Prompt: prompt})
		sendDone <- sendResult{evt, err}
	}()

	select {
	case lde := <-lockdownCh:
		// Lockdown fired during the session — terminate cleanly.
		printLockdownSummary(lde)
		return nil

	case res := <-sendDone:
		if res.err != nil {
			// Lockdown may also surface here (e.g. pre-session lockdown
			// detected by the permission handler before Send returns).
			var lde *LockdownError
			if errors.As(res.err, &lde) {
				printLockdownSummary(lde)
				return nil
			}
			return fmt.Errorf("session: %w", res.err)
		}
		return nil
	}
}

// Preflight runs 5 startup validations in order.
//
// Invariant: returns ("...", nil) iff all 5 checks pass and the last line of
// the report is "preflight OK\n". Returns a partial report plus non-nil error
// at the first failing check.
func Preflight(opts PreflightOpts) (string, error) {
	var sb strings.Builder

	// 1. copilot binary on PATH.
	if _, err := exec.LookPath("copilot"); err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] copilot binary: %v\n", err))
		return sb.String(), fmt.Errorf("copilot binary: %w", err)
	}
	sb.WriteString("  [OK]   copilot binary\n")

	// 2. gh auth status (proves GitHub token is present).
	ghCmd := exec.Command("gh", "auth", "status")
	if err := ghCmd.Run(); err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] gh auth status: %v\n", err))
		return sb.String(), fmt.Errorf("gh auth status: %w", err)
	}
	sb.WriteString("  [OK]   gh auth status\n")

	// 3. Policy load (chitin.yaml search from opts.Cwd upward).
	if _, _, err := gov.LoadWithInheritance(opts.Cwd); err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] policy load: %v\n", err))
		return sb.String(), fmt.Errorf("policy load: %w", err)
	}
	sb.WriteString("  [OK]   policy load\n")

	// 4. ~/.chitin/ writable (create dir + probe file).
	chitinDir := defaultChitinDir()
	if err := os.MkdirAll(chitinDir, 0755); err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] ~/.chitin/ writable: %v\n", err))
		return sb.String(), fmt.Errorf("~/.chitin/ writable: %w", err)
	}
	probe := filepath.Join(chitinDir, ".preflight-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0644); err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] ~/.chitin/ writable: %v\n", err))
		return sb.String(), fmt.Errorf("~/.chitin/ writable: %w", err)
	}
	_ = os.Remove(probe)
	sb.WriteString("  [OK]   ~/.chitin/ writable\n")

	// 5. gov.db path accessible — open and immediately close the Counter to
	// confirm SQLite can create the file. This is a lightweight open; the
	// real Run() will re-open it for the session.
	dbPath := filepath.Join(chitinDir, "gov.db")
	counter, err := gov.OpenCounter(dbPath)
	if err != nil {
		sb.WriteString(fmt.Sprintf("  [FAIL] gov.db open: %v\n", err))
		return sb.String(), fmt.Errorf("gov.db open: %w", err)
	}
	_ = counter.Close()
	sb.WriteString("  [OK]   gov.db accessible\n")

	sb.WriteString("preflight OK\n")
	return sb.String(), nil
}

// defaultChitinDir returns ~/.chitin, the runtime state directory.
func defaultChitinDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chitin")
}

func printLockdownSummary(lde *LockdownError) {
	fmt.Fprintf(os.Stderr, "\n=== Session terminated: %s ===\n", lde.Error())
}

// runInteractive is stubbed — Task 14 implements the full REPL.
func runInteractive(_ context.Context, _ *copilotsdk.Session) error {
	return errors.New("interactive mode not yet implemented — Task 14 adds REPL")
}
