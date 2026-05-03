package plugins

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// applySandbox wraps (cmd, args) into a bubblewrap invocation when
// SandboxConfig.Mode == "bwrap". Returns the (possibly-wrapped)
// (cmd, args) — caller passes them straight to exec.CommandContext.
//
// Falls open on:
//   - Mode == "" (off): returns (cmd, args) unchanged
//   - Linux-only sandboxing requested on non-Linux: returns
//     unchanged + writes a one-line warning to errOut so the
//     operator notices their config is a no-op
//   - bwrap binary missing: same — unchanged + warning
//
// Does NOT fall open on:
//   - bwrap exec failure at run time (e.g., AppArmor restricting
//     unprivileged user namespaces on Ubuntu 24+, hardened kernel
//     blocking userns clone). Those surface through the normal
//     exec error path in loader.go's Run() — caller treats the
//     plugin as no-signal and logs the error. Operator response
//     is documented in docs/router/plugin-sandbox.md (configure
//     apparmor profile or set kernel.apparmor_restrict_
//     unprivileged_userns=0).
//
// Why fall open on missing-binary but not on exec-failure: missing
// binary = operator hasn't installed bwrap yet (an obvious config
// gap they can fix). Exec failure at run time = bwrap is present
// but kernel/apparmor refuses — a host-policy decision that the
// kernel should respect by surfacing the failure, not silently
// running unsandboxed.
func applySandbox(cfg SandboxConfig, manifestName, modulePath, cmd string, args []string, errOut io.Writer) (string, []string) {
	if cfg.Mode == "" {
		return cmd, args
	}
	if cfg.Mode != "bwrap" {
		warn(errOut, manifestName, fmt.Sprintf("unsupported sandbox.mode %q (want bwrap); running unsandboxed", cfg.Mode))
		return cmd, args
	}
	if runtime.GOOS != "linux" {
		warn(errOut, manifestName, "sandbox.mode=bwrap is Linux-only; running unsandboxed")
		return cmd, args
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		warn(errOut, manifestName, "bwrap binary not found on PATH; running unsandboxed (apt install bubblewrap)")
		return cmd, args
	}

	// bwrap: build a minimal namespace.
	//
	//   --ro-bind /usr /usr        : interpreters, libs (python3, node)
	//   --ro-bind /etc /etc        : ld.so.conf, certs, /etc/resolv.conf
	//   --ro-bind /lib /lib        : implicit on most distros via /usr
	//   --ro-bind /lib64 /lib64    : same
	//   --tmpfs /tmp               : scratch space
	//   --proc /proc               : python's `os.getpid` etc need /proc
	//   --dev /dev                 : minimal /dev (urandom, null, ...)
	//   --unshare-all              : new IPC/PID/UTS/cgroup namespaces
	//   --unshare-net (default)    : drop network namespace
	//   --die-with-parent          : kernel parent dies → bwrap dies
	//   --new-session              : detach from operator tty
	bwrapArgs := []string{
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc", "/etc",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
		"--unshare-all",
		"--die-with-parent",
		"--new-session",
	}

	// /lib + /lib64 are symlinks → /usr/lib on most modern distros,
	// but standalone on Debian/Ubuntu. Bind both if present; bwrap
	// silently no-ops the missing one... actually no, it fails.
	// Use --ro-bind-try (bwrap 0.4+) which is the no-op-on-missing
	// variant.
	bwrapArgs = append(bwrapArgs,
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/sbin", "/sbin",
	)

	// Bind the plugin module path (the actual .py / .ts / .sh).
	if modulePath != "" {
		modAbs, err := filepath.Abs(modulePath)
		if err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", modAbs, modAbs)
		}
	}

	// Bind the runtime command's binary path (e.g., /usr/bin/python3
	// is in /usr already, but operators sometimes have a venv
	// python at $HOME/.venv/...). exec.LookPath resolves whichever
	// the runtime resolution will use.
	if abs, err := exec.LookPath(cmd); err == nil {
		// Only add if not already covered by /usr or /bin.
		if !strings.HasPrefix(abs, "/usr/") && !strings.HasPrefix(abs, "/bin/") && !strings.HasPrefix(abs, "/sbin/") {
			bwrapArgs = append(bwrapArgs, "--ro-bind", abs, abs)
		}
	}

	for _, b := range cfg.ExtraReadOnlyBinds {
		abs, err := filepath.Abs(b)
		if err != nil {
			continue
		}
		bwrapArgs = append(bwrapArgs, "--ro-bind", abs, abs)
	}

	if cfg.AllowNetwork {
		bwrapArgs = append(bwrapArgs, "--share-net")
	}

	if cfg.AllowWrite {
		// Operator opted into write — bind home rw. Coarse but
		// honest: if you say AllowWrite, expect plugin can mutate
		// your home dir. Operators wanting finer control should
		// use ExtraReadOnlyBinds + a tmpfs for the writable spot.
		// For MVP, this is enough.
		bwrapArgs = append(bwrapArgs, "--bind-try", homeDir(), homeDir())
	}

	// Terminator: "--" separates bwrap args from the wrapped command.
	bwrapArgs = append(bwrapArgs, "--", cmd)
	bwrapArgs = append(bwrapArgs, args...)
	return "bwrap", bwrapArgs
}

func warn(w io.Writer, plugin, msg string) {
	if w == nil {
		return
	}
	fmt.Fprintf(w,
		"{\"ts\":%q,\"level\":\"warn\",\"component\":\"router-plugin-sandbox\",\"plugin\":%q,\"msg\":%q}\n",
		time.Now().UTC().Format(time.RFC3339), plugin, msg,
	)
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "/"
}
