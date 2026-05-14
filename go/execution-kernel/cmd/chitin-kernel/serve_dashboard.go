package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

func cmdServe(args []string) {
	if len(args) < 1 {
		exitErr("serve_no_subcommand", "usage: chitin-kernel serve dashboard [--port=9090]")
	}
	switch args[0] {
	case "dashboard":
		cmdServeDashboard(args[1:])
	default:
		exitErr("serve_unknown_subcommand", args[0])
	}
}

func cmdServeDashboard(args []string) {
	port := 9090
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--port="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--port="))
			if err != nil || n <= 0 {
				exitErr("serve_dashboard_port", "--port must be > 0")
			}
			port = n
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, "Usage: chitin-kernel serve dashboard [--port=9090]")
			os.Exit(0)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleDashboardIndex)
	mux.HandleFunc("/session/", handleDashboardIndex)
	mux.HandleFunc("/api/sessions", handleDashboardSessions)
	mux.HandleFunc("/api/session/", handleDashboardSession)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Fprintf(os.Stdout, "dashboard listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		exitErr("serve_dashboard_failed", err.Error())
	}
}

func handleDashboardSessions(w http.ResponseWriter, r *http.Request) {
	recent := 20
	if raw := r.URL.Query().Get("recent"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			recent = n
		}
	}
	sessions, err := replay.ListRecentSessions(recent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func handleDashboardSession(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/session/")
	if sessionID == "" || sessionID == r.URL.Path {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	if sessionID == "latest" {
		latest, err := replay.FindMostRecentSession()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		sessionID = latest
	}
	timeline, err := replay.BuildTimeline(replay.ReplayOptions{SessionID: sessionID})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, timeline)
}

func handleDashboardIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Chitin Dashboard</title>
<style>
  :root {
    --bg: #f2ede3;
    --panel: rgba(255,255,255,0.78);
    --ink: #1b1a18;
    --muted: #635b51;
    --line: rgba(27,26,24,0.12);
    --accent: #9a412f;
    --accent-2: #45624f;
    --accent-3: #b38a3b;
    --shadow: 0 14px 40px rgba(43,31,17,0.08);
    --mono: ui-monospace, SFMono-Regular, Menlo, monospace;
    --sans: "Iowan Old Style", Georgia, serif;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    font-family: var(--sans);
    color: var(--ink);
    background:
      radial-gradient(circle at top left, rgba(154,65,47,0.10), transparent 25%),
      radial-gradient(circle at 85% 18%, rgba(69,98,79,0.10), transparent 20%),
      linear-gradient(180deg, #f6f1e7, #ece4d4);
  }
  .shell { width: min(1200px, calc(100vw - 28px)); margin: 0 auto; padding: 20px 0 32px; }
  .topbar { display:flex; justify-content:space-between; gap:16px; align-items:center; margin-bottom:18px; }
  .title { font-size: clamp(2rem, 4vw, 3.6rem); letter-spacing: -0.05em; }
  .muted, table { color: var(--muted); }
  .grid { display:grid; gap:16px; }
  .cards { grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); }
  .panel {
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 22px;
    box-shadow: var(--shadow);
    padding: 18px;
  }
  .metric { font-family: var(--mono); font-size: 1.65rem; color: var(--ink); }
  .label { font-size: 0.84rem; text-transform: uppercase; letter-spacing: 0.08em; }
  .layout { display:grid; grid-template-columns: 320px minmax(0,1fr); gap:16px; align-items:start; }
  .session-list { max-height: 78vh; overflow:auto; }
  .session-link { display:block; padding:12px 0; border-bottom:1px solid var(--line); color:inherit; text-decoration:none; }
  .session-link:last-child { border-bottom:0; }
  .session-link strong { display:block; color:var(--ink); font-family: var(--mono); }
  .chart { width:100%; min-height:260px; }
  .section-title { margin:0 0 12px; font-size:1.2rem; }
  .row-2 { display:grid; grid-template-columns: 1.4fr 1fr; gap:16px; }
  .table { width:100%; border-collapse: collapse; font-size: 0.94rem; }
  .table th, .table td { text-align:left; padding:8px 0; border-bottom:1px solid var(--line); vertical-align: top; }
  .pill { display:inline-block; padding:3px 8px; border-radius:999px; font-family:var(--mono); font-size:0.78rem; background:#eee2d8; }
  .timeline { max-height: 360px; overflow:auto; }
  .timeline tr:hover { background: rgba(154,65,47,0.05); }
  svg text { font-family: var(--mono); fill: var(--muted); font-size: 12px; }
  @media (max-width: 900px) {
    .layout, .row-2 { grid-template-columns: 1fr; }
    .session-list { max-height: none; }
  }
</style>
</head>
<body>
<div class="shell">
  <div class="topbar">
    <div>
      <div class="title">Session Cost Replay</div>
      <div class="muted">Timeline-aligned token and spend breakdown across drivers, tools, and models.</div>
    </div>
    <a href="/session/latest" class="pill">latest session</a>
  </div>
  <div class="layout">
    <section class="panel session-list">
      <h2 class="section-title">Recent Sessions</h2>
      <div id="sessions"></div>
    </section>
    <main class="grid">
      <section class="grid cards" id="cards"></section>
      <section class="panel">
        <h2 class="section-title">Cost Over Time by Driver</h2>
        <div id="areaChart" class="chart"></div>
      </section>
      <section class="row-2">
        <div class="panel">
          <h2 class="section-title">Per-Tool Cost</h2>
          <div id="barChart" class="chart"></div>
        </div>
        <div class="panel">
          <h2 class="section-title">Driver × Model Heatmap</h2>
          <div id="heatmap" class="chart"></div>
        </div>
      </section>
      <section class="panel">
        <h2 class="section-title">Timeline Steps</h2>
        <div class="timeline">
          <table class="table">
            <thead>
              <tr><th>Time</th><th>Type</th><th>Driver</th><th>Tool</th><th>Tokens</th><th>Cost</th><th>Decision</th></tr>
            </thead>
            <tbody id="steps"></tbody>
          </table>
        </div>
      </section>
    </main>
  </div>
</div>
<script>
const palette = ["#9a412f","#45624f","#b38a3b","#31506f","#7a4e7b","#bb6d2e"];
const money = new Intl.NumberFormat("en-US",{style:"currency",currency:"USD",maximumFractionDigits:4});
const number = new Intl.NumberFormat("en-US");

function pathSessionId() {
  const parts = window.location.pathname.split("/").filter(Boolean);
  if (parts[0] === "session" && parts[1]) return decodeURIComponent(parts[1]);
  return "latest";
}

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"]/g, (ch) => ({ "&":"&amp;", "<":"&lt;", ">":"&gt;", '"':"&quot;" }[ch]));
}

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

function renderCards(summary) {
  const cards = [
    ["Total Cost", money.format(summary.total_cost_usd || 0)],
    ["Total Tokens", number.format(summary.total_tokens || 0)],
    ["Dispatches", number.format(summary.dispatch_count || 0)],
    ["Success Rate", ((summary.success_rate || 0) * 100).toFixed(1) + "%"],
  ];
  document.getElementById("cards").innerHTML = cards.map(([label, value]) =>
    '<section class="panel"><div class="label">'+label+'</div><div class="metric">'+escapeHtml(value)+'</div></section>'
  ).join("");
}

function renderSessions(sessions, active) {
  document.getElementById("sessions").innerHTML = sessions.map((session) => {
    const href = "/session/" + encodeURIComponent(session.session_id);
    const selected = session.session_id === active ? ' style="border-left:3px solid var(--accent); padding-left:10px;"' : "";
    return '<a class="session-link"'+selected+' href="'+href+'"><strong>'+escapeHtml(session.session_id)+'</strong><span>'+escapeHtml(session.last_ts)+' · '+escapeHtml(session.driver || "-")+' · '+number.format(session.events)+' events</span></a>';
  }).join("");
}

function renderSteps(steps) {
  document.getElementById("steps").innerHTML = steps.map((step) => {
    const decision = step.decision ? (step.decision.allowed ? "allow" : "deny") : "";
    const tokens = number.format((step.tokens_in || 0) + (step.tokens_out || 0));
    return "<tr>" +
      "<td><code>"+escapeHtml(step.ts)+"</code></td>" +
      "<td>"+escapeHtml(step.type)+"</td>" +
      "<td>"+escapeHtml(step.driver || "-")+"</td>" +
      "<td>"+escapeHtml(step.tool || "-")+"</td>" +
      "<td>"+escapeHtml(tokens)+"</td>" +
      "<td>"+escapeHtml(money.format(step.cost_usd || 0))+"</td>" +
      "<td>"+escapeHtml(decision)+"</td>" +
    "</tr>";
  }).join("");
}

function svgWrap(w, h, inner) {
  return '<svg viewBox="0 0 '+w+' '+h+'" width="100%" height="100%" preserveAspectRatio="none">'+inner+'</svg>';
}

function renderArea(summary) {
  const host = document.getElementById("areaChart");
  const points = summary.cost_timeline || [];
  const drivers = Object.keys(summary.cost_by_driver || {});
  if (!points.length || !drivers.length) {
    host.innerHTML = "<div class='muted'>No cost data in this session.</div>";
    return;
  }
  const w = 900, h = 260, left = 36, right = 16, top = 16, bottom = 28;
  const maxY = Math.max(...points.map((point) => point.cumulative_usd || 0), 0.0001);
  const xFor = (index) => left + ((w - left - right) * index / Math.max(points.length - 1, 1));
  const yFor = (value) => h - bottom - ((h - top - bottom) * value / maxY);
  const stacked = drivers.map((driver, driverIndex) => {
    const upper = points.map((point, pointIndex) => {
      const driverCosts = point.driver_costs_usd || {};
      const lowerValue = drivers.slice(0, driverIndex).reduce((sum, name) => sum + (driverCosts[name] || 0), 0);
      return [xFor(pointIndex), yFor(lowerValue + (driverCosts[driver] || 0))];
    });
    const lower = points.map((point, pointIndex) => {
      const driverCosts = point.driver_costs_usd || {};
      const lowerValue = drivers.slice(0, driverIndex).reduce((sum, name) => sum + (driverCosts[name] || 0), 0);
      return [xFor(pointIndex), yFor(lowerValue)];
    });
    const path = "M " + upper.map((pair) => pair.join(" ")).join(" L ") + " L " + lower.reverse().map((pair) => pair.join(" ")).join(" L ") + " Z";
    return { driver, path };
  });
  let inner = "";
  stacked.forEach((entry, index) => {
    inner += '<path d="'+entry.path+'" fill="'+palette[index % palette.length]+'" fill-opacity="0.34" stroke="'+palette[index % palette.length]+'" stroke-width="1.4"></path>';
  });
  inner += '<line x1="'+left+'" y1="'+(h-bottom)+'" x2="'+(w-right)+'" y2="'+(h-bottom)+'" stroke="#7d7268" stroke-width="1"></line>';
  inner += '<text x="'+left+'" y="'+(top+4)+'">'+escapeHtml(money.format(maxY))+'</text>';
  inner += '<text x="'+(w-right-70)+'" y="'+(h-8)+'">timeline</text>';
  host.innerHTML = svgWrap(w, h, inner) + '<div class="muted">' + drivers.map((driver, index) => '<span class="pill" style="margin-right:6px;background:'+palette[index % palette.length]+'22;border:1px solid '+palette[index % palette.length]+'55;">'+escapeHtml(driver)+'</span>').join("") + '</div>';
}

function renderBars(summary) {
  const host = document.getElementById("barChart");
  const entries = Object.entries(summary.cost_by_tool || {}).sort((a,b) => b[1]-a[1]).slice(0, 8);
  if (!entries.length) {
    host.innerHTML = "<div class='muted'>No tool spend recorded.</div>";
    return;
  }
  const w = 640, h = 260, left = 120, right = 16, top = 14, bottom = 24;
  const max = Math.max(...entries.map(([, value]) => value), 0.0001);
  const barH = (h - top - bottom) / entries.length - 6;
  let inner = "";
  entries.forEach(([tool, value], idx) => {
    const y = top + idx * ((h - top - bottom) / entries.length);
    const width = ((w - left - right) * value / max);
    inner += '<text x="6" y="'+(y+barH)+'">'+escapeHtml(tool)+'</text>';
    inner += '<rect x="'+left+'" y="'+y+'" width="'+width+'" height="'+barH+'" rx="8" fill="'+palette[idx % palette.length]+'"></rect>';
    inner += '<text x="'+(left + width + 6)+'" y="'+(y+barH)+'">'+escapeHtml(money.format(value))+'</text>';
  });
  host.innerHTML = svgWrap(w, h, inner);
}

function renderHeatmap(summary) {
  const host = document.getElementById("heatmap");
  const cells = summary.cost_heatmap || [];
  if (!cells.length) {
    host.innerHTML = "<div class='muted'>No driver/model spend recorded.</div>";
    return;
  }
  const drivers = [...new Set(cells.map((cell) => cell.driver || "-"))];
  const models = [...new Set(cells.map((cell) => cell.model || "-"))];
  const max = Math.max(...cells.map((cell) => cell.cost_usd || 0), 0.0001);
  const w = 480, h = 260, left = 90, top = 42, cellW = 90, cellH = 34;
  let inner = "";
  models.forEach((model, idx) => { inner += '<text x="'+(left + idx * cellW + 8)+'" y="18">'+escapeHtml(model)+'</text>'; });
  drivers.forEach((driver, row) => {
    inner += '<text x="6" y="'+(top + row * cellH + 22)+'">'+escapeHtml(driver)+'</text>';
    models.forEach((model, col) => {
      const cell = cells.find((item) => (item.driver || "-") === driver && (item.model || "-") === model);
      const value = cell ? cell.cost_usd || 0 : 0;
      const alpha = Math.max(0.12, value / max);
      inner += '<rect x="'+(left + col * cellW)+'" y="'+(top + row * cellH)+'" width="'+(cellW - 6)+'" height="'+(cellH - 6)+'" rx="8" fill="rgba(154,65,47,'+alpha.toFixed(3)+')"></rect>';
      inner += '<text x="'+(left + col * cellW + 8)+'" y="'+(top + row * cellH + 22)+'">'+escapeHtml(money.format(value))+'</text>';
    });
  });
  host.innerHTML = svgWrap(w, Math.max(h, top + drivers.length * cellH + 16), inner);
}

async function boot() {
  const active = pathSessionId();
  const [sessions, timeline] = await Promise.all([
    fetchJSON("/api/sessions?recent=20"),
    fetchJSON("/api/session/" + encodeURIComponent(active)),
  ]);
  renderSessions(sessions, timeline.session_id);
  renderCards(timeline.summary);
  renderArea(timeline.summary);
  renderBars(timeline.summary);
  renderHeatmap(timeline.summary);
  renderSteps(timeline.steps || []);
}

boot().catch((err) => {
  document.querySelector("main").innerHTML = '<section class="panel"><strong>Dashboard load failed</strong><div class="muted">'+escapeHtml(err.message)+'</div></section>';
});
</script>
</body>
</html>`
