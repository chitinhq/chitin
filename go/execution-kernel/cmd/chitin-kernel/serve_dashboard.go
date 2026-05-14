package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

func cmdServe(args []string) {
	if len(args) < 1 {
		exitErr("serve_no_subcommand", "usage: chitin-kernel serve {dashboard} [flags]")
	}
	switch args[0] {
	case "dashboard":
		cmdServeDashboard(args[1:])
	default:
		exitErr("serve_unknown_subcommand", args[0])
	}
}

func cmdServeDashboard(args []string) {
	fs := flag.NewFlagSet("serve dashboard", flag.ExitOnError)
	port := fs.Int("port", 9090, "port to bind")
	host := fs.String("host", "127.0.0.1", "host to bind (default: localhost-only)")
	cwd := fs.String("cwd", ".", "cwd used to resolve chitin.yaml for /policy")
	recent := fs.Int("recent", 25, "number of recent sessions to expose")
	fs.Parse(args)

	staticDir, err := dashboardStaticDir()
	if err != nil {
		exitErr("dashboard_static_dir", err.Error())
	}
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	mux := newDashboardMux(staticDir, *cwd, *recent)
	fmt.Fprintf(os.Stdout, "dashboard serving on http://%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		exitErr("dashboard_listen", err.Error())
	}
}

func newDashboardMux(staticDir, cwd string, recent int) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/sessions":
			writeDashboardJSON(w, dashboardSessionsResponse(recent))
			return
		case r.URL.Path == "/api/elo":
			writeDashboardJSON(w, dashboardEloResponse())
			return
		case strings.HasPrefix(r.URL.Path, "/api/session/"):
			sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
			writeDashboardJSON(w, dashboardTimelineResponse(sessionID))
			return
		case r.URL.Path == "/api/policy":
			writeDashboardJSON(w, dashboardPolicyResponse(cwd))
			return
		case r.URL.Path == "/":
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		case hasStaticAssetExtension(r.URL.Path):
			fileServer.ServeHTTP(w, r)
			return
		default:
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}
	})
}

func dashboardSessionsResponse(recent int) map[string]any {
	sessions, err := replay.ListDashboardSessions(recent)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"sessions": sessions}
}

func dashboardTimelineResponse(sessionID string) map[string]any {
	if sessionID == "" {
		return map[string]any{"error": "session_id is required"}
	}
	timeline, err := replay.BuildTimeline(replay.ReplayOptions{SessionID: sessionID})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"timeline": timeline}
}

func dashboardPolicyResponse(cwd string) map[string]any {
	path, body, err := replay.ReadDashboardPolicy(cwd)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"path": path, "body": body}
}

func dashboardEloResponse() map[string]any {
	rows, placeholder, err := replay.ReadDashboardElo(10)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"rows": rows, "placeholder": placeholder}
}

func writeDashboardJSON(w http.ResponseWriter, body map[string]any) {
	if _, ok := body["error"]; ok {
		w.WriteHeader(http.StatusBadRequest)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

func hasStaticAssetExtension(path string) bool {
	switch filepath.Ext(path) {
	case ".css", ".js", ".map", ".svg", ".png", ".jpg", ".jpeg", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

func dashboardStaticDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	path := filepath.Join(root, "apps", "chitin-dashboard", "dist")
	if _, err := os.Stat(filepath.Join(path, "index.html")); err != nil {
		return "", fmt.Errorf("dashboard build not found at %s", path)
	}
	return path, nil
}
