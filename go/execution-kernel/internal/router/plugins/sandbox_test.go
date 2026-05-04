package plugins

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestApplySandbox_OffMode_PassThrough(t *testing.T) {
	cmd, args := applySandbox(SandboxConfig{}, "test-plugin", "/tmp/x.py", "python3", []string{"-u", "/tmp/x.py"}, nil)
	if cmd != "python3" {
		t.Errorf("Mode=\"\" should pass cmd through; got %q", cmd)
	}
	if len(args) != 2 {
		t.Errorf("Mode=\"\" should pass args through; got %v", args)
	}
}

func TestApplySandbox_UnsupportedMode_PassThroughWithWarn(t *testing.T) {
	var buf bytes.Buffer
	cfg := SandboxConfig{Mode: "firecracker"}
	cmd, _ := applySandbox(cfg, "test-plugin", "/tmp/x.py", "python3", nil, &buf)
	if cmd != "python3" {
		t.Errorf("unsupported mode should pass through; got %q", cmd)
	}
	if !strings.Contains(buf.String(), "unsupported sandbox.mode") {
		t.Errorf("expected warning about unsupported mode; got %q", buf.String())
	}
}

// TestApplySandbox_MissingBwrap_FallsOpen forces exec.LookPath
// to fail by clearing PATH and asserts the wrapper passes
// (cmd, args) through unchanged with a warning. This is the
// fall-open path that earlier comments referenced — now actually
// covered.
func TestApplySandbox_MissingBwrap_FallsOpen(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only path")
	}
	t.Setenv("PATH", "")
	var buf bytes.Buffer
	cfg := SandboxConfig{Mode: "bwrap"}
	cmd, args := applySandbox(cfg, "test-plugin", "/tmp/x.py", "python3", []string{"-u", "/tmp/x.py"}, &buf)
	if cmd != "python3" {
		t.Errorf("missing-bwrap should pass cmd through; got %q", cmd)
	}
	if len(args) != 2 {
		t.Errorf("missing-bwrap should pass args through; got %v", args)
	}
	if !strings.Contains(buf.String(), "bwrap binary not found") {
		t.Errorf("expected bwrap-missing warning; got %q", buf.String())
	}
}

func TestApplySandbox_BwrapMode_WrapsCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bwrap is Linux-only")
	}
	// If bwrap isn't installed on this CI host, skip — the
	// fall-open path is covered by TestApplySandbox_MissingBwrap_FallsOpen.
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed; skipping wrap test")
	}
	var buf bytes.Buffer
	cfg := SandboxConfig{Mode: "bwrap"}
	cmd, args := applySandbox(cfg, "test-plugin", "/tmp/x.py", "python3", []string{"-u", "/tmp/x.py"}, &buf)
	if cmd != "bwrap" {
		t.Errorf("expected cmd=bwrap; got %q", cmd)
	}
	// bwrap arg list must contain --unshare-all (network drop) and
	// the wrapped python3 invocation must come AFTER a "--"
	// separator.
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--unshare-all") {
		t.Errorf("expected --unshare-all in wrapped args; got %q", joined)
	}
	if !strings.Contains(joined, "-- python3") {
		t.Errorf("expected '-- python3' separator in wrapped args; got %q", joined)
	}
}

func TestApplySandbox_AllowNetwork_AddsShareNet(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	cfg := SandboxConfig{Mode: "bwrap", AllowNetwork: true}
	_, args := applySandbox(cfg, "p", "/tmp/x.py", "python3", nil, nil)
	if !contains(args, "--share-net") {
		t.Errorf("AllowNetwork=true should add --share-net; got %v", args)
	}
}

func TestApplySandbox_NetworkDroppedByDefault(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	cfg := SandboxConfig{Mode: "bwrap"}
	_, args := applySandbox(cfg, "p", "/tmp/x.py", "python3", nil, nil)
	if contains(args, "--share-net") {
		t.Errorf("AllowNetwork unset should NOT add --share-net; got %v", args)
	}
	// --unshare-all already drops net.
	if !contains(args, "--unshare-all") {
		t.Errorf("expected --unshare-all default; got %v", args)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

