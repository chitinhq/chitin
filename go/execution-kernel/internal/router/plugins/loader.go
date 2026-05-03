// Package plugins implements the router's plugin loader. Plugins are
// declared in chitin.yaml under `router.plugins:` and run as
// subprocess invocations per tool call (when policy says so).
//
// Wire protocol:
//   stdin  : a single line of JSON — the HookInput shape
//   stdout : a single line of JSON — the HeuristicScore shape
//            ({"score": 0.0-1.0, "fired": bool, "reason": "..."})
//   stderr : structured warning logs (forwarded to operator stderr)
//
// Runtimes supported (chosen via chitin.yaml's `runtime:` field):
//   python3  →  python plugins (faster cold-start, ~50-100ms)
//   node     →  TypeScript / JavaScript plugins (~500ms)
//   bun      →  TypeScript via Bun (~50ms cold, but operator must
//                have bun installed)
//   bash     →  shell scripts (fastest, ~10ms; for prototyping)
//
// Cost model: per-call subprocess spawn. Most tool calls SKIP
// plugins (gated by policy.advisor.when triggers). Plugin authors
// should keep their work fast — heavy compute belongs in advisor
// LLM calls, not heuristic plugins.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// PluginManifest — what chitin.yaml declares for one plugin.
// Lives under `router.plugins[]`.
type PluginManifest struct {
	// Name identifies the plugin in telemetry (operator-readable).
	Name string `yaml:"name" json:"name"`
	// Type — heuristic | advisor. Heuristics fire pre-advisor; advisors
	// receive the heuristic outcome and return a verdict + nudge.
	Type string `yaml:"type" json:"type"`
	// Runtime — which interpreter to spawn (python3, node, bun, bash).
	Runtime string `yaml:"runtime" json:"runtime"`
	// Module — entrypoint for the runtime to invoke. For node/bun: a
	// path to a .ts or .js file. For python3: a path to a .py file
	// (callable as `python3 -u <path>`). For bash: an executable
	// script path.
	Module string `yaml:"module" json:"module"`
	// Config — plugin-specific JSON object passed to the plugin via
	// the `config` field of its stdin payload.
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
	// Timeout — wall-clock cap on the subprocess. Plugins exceeding
	// this are killed and treated as no-signal (fire=false). Default
	// 5s — heuristics should be fast.
	TimeoutMs int `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
}

// PluginInput is what the plugin sees on stdin.
type PluginInput struct {
	HookInput map[string]interface{} `json:"hook_input"`
	Config    map[string]interface{} `json:"config"`
}

// PluginOutput is what the plugin writes to stdout.
type PluginOutput struct {
	Score  float64                `json:"score"`
	Fired  bool                   `json:"fired"`
	Reason string                 `json:"reason"`
	Axis   map[string]interface{} `json:"axis,omitempty"`
}

// runtimeCommand maps a runtime label to the exec.Command shape.
// Returns (cmd, args, err). cmd may be empty for unsupported.
func runtimeCommand(runtime, module string) (string, []string, error) {
	switch runtime {
	case "python3", "python":
		return "python3", []string{"-u", module}, nil
	case "node":
		return "node", []string{"--experimental-strip-types", module}, nil
	case "bun":
		return "bun", []string{"run", module}, nil
	case "bash", "sh":
		return module, nil, nil // execute the script directly
	default:
		return "", nil, fmt.Errorf("plugins: unsupported runtime %q (want python3|node|bun|bash)", runtime)
	}
}

// Run a plugin against a hook input. Returns (output, nil) on
// success, (nil, err) on any failure (timeout, malformed output,
// missing binary). Caller decides fallback (typically: treat as
// no-signal + log warn).
func Run(ctx context.Context, manifest PluginManifest, hookInput map[string]interface{}, errOut io.Writer) (*PluginOutput, error) {
	if manifest.TimeoutMs == 0 {
		manifest.TimeoutMs = 5000
	}
	cmd, args, err := runtimeCommand(manifest.Runtime, manifest.Module)
	if err != nil {
		return nil, err
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(manifest.TimeoutMs)*time.Millisecond)
	defer cancel()

	pInput := PluginInput{HookInput: hookInput, Config: manifest.Config}
	stdin, err := json.Marshal(pInput)
	if err != nil {
		return nil, fmt.Errorf("plugins: marshal input: %w", err)
	}
	stdin = append(stdin, '\n')

	c := exec.CommandContext(cctx, cmd, args...)
	c.Stdin = strings.NewReader(string(stdin))
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}
	c.Stdout = stdoutBuf
	c.Stderr = stderrBuf
	runErr := c.Run()

	// Forward plugin stderr to operator stderr for visibility
	if stderrBuf.Len() > 0 && errOut != nil {
		fmt.Fprintf(errOut,
			"{\"ts\":%q,\"level\":\"info\",\"component\":\"router-plugin\",\"plugin\":%q,\"msg\":\"plugin-stderr\",\"text\":%q}\n",
			time.Now().UTC().Format(time.RFC3339),
			manifest.Name,
			truncate(stderrBuf.String(), 500),
		)
	}

	if runErr != nil {
		return nil, fmt.Errorf("plugins: %s exec: %w", manifest.Name, runErr)
	}

	// Parse last newline-delimited JSON from stdout
	out := strings.TrimSpace(stdoutBuf.String())
	if out == "" {
		return nil, fmt.Errorf("plugins: %s produced no output", manifest.Name)
	}
	lines := strings.Split(out, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	var po PluginOutput
	if err := json.Unmarshal([]byte(last), &po); err != nil {
		return nil, fmt.Errorf("plugins: %s parse output: %w (last line: %s)", manifest.Name, err, truncate(last, 200))
	}
	return &po, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
