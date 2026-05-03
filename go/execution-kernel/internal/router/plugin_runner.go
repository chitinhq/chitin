package router

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router/plugins"
)

// RunPlugins invokes each declared plugin with the hook input,
// concurrently, bounded by individual plugin timeouts. Plugins
// that fail (timeout, malformed output, missing binary) are
// logged to stderr but don't block the pipeline — failed plugins
// just don't contribute a heuristic outcome.
//
// Returns a list of HeuristicScore (one per plugin that produced
// a valid output) keyed by plugin name. Concurrent execution
// keeps total wall time = max plugin timeout (not sum).
//
// Performance: each plugin spawn is ~50-500ms cold. With 5
// plugins running in parallel, overhead is roughly the slowest
// plugin's wall time, not 5x.
func RunPlugins(ctx context.Context, pluginConfigs []PluginConfig, input HookInput, errOut io.Writer) []NamedHeuristicScore {
	if len(pluginConfigs) == 0 {
		return nil
	}

	hookMap := map[string]interface{}{
		"hook_event_name": input.HookEventName,
		"tool_name":       input.ToolName,
		"tool_input":      input.ToolInput,
		"cwd":             input.Cwd,
		"session_id":      input.SessionID,
	}

	results := make([]NamedHeuristicScore, len(pluginConfigs))
	var wg sync.WaitGroup
	for i, pc := range pluginConfigs {
		wg.Add(1)
		go func(idx int, p PluginConfig) {
			defer wg.Done()
			manifest := plugins.PluginManifest{
				Name:      p.Name,
				Type:      p.Type,
				Runtime:   p.Runtime,
				Module:    p.Module,
				Config:    p.Config,
				TimeoutMs: p.TimeoutMs,
			}
			out, err := plugins.Run(ctx, manifest, hookMap, errOut)
			if err != nil {
				if errOut != nil {
					writePluginError(errOut, p.Name, err)
				}
				return
			}
			results[idx] = NamedHeuristicScore{
				Name: p.Name,
				Type: p.Type,
				Score: HeuristicScore{
					Score:  out.Score,
					Fired:  out.Fired,
					Reason: out.Reason,
				},
			}
		}(i, pc)
	}
	wg.Wait()

	// Filter out failed plugins (zero-value Name)
	out := results[:0]
	for _, r := range results {
		if r.Name != "" {
			out = append(out, r)
		}
	}
	return out
}

// NamedHeuristicScore wraps a HeuristicScore with the plugin's
// name + type so the router can route fired-plugin signals into
// advisor triggers.
type NamedHeuristicScore struct {
	Name  string
	Type  string
	Score HeuristicScore
}

func writePluginError(w io.Writer, name string, err error) {
	_, _ = w.Write([]byte("{\"ts\":\""))
	_, _ = w.Write([]byte(time.Now().UTC().Format(time.RFC3339)))
	_, _ = w.Write([]byte("\",\"level\":\"warn\",\"component\":\"router-plugin-runner\",\"plugin\":\""))
	_, _ = w.Write([]byte(name))
	_, _ = w.Write([]byte("\",\"msg\":\"plugin-failed\",\"err\":\""))
	// crude escaping
	for _, c := range err.Error() {
		if c == '"' || c == '\\' {
			_, _ = w.Write([]byte{'\\'})
		}
		_, _ = w.Write([]byte{byte(c)})
	}
	_, _ = w.Write([]byte("\"}\n"))
}
