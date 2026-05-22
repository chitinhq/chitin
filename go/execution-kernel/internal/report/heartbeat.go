package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/health"
)

// ComponentState is the liveness verdict for one heartbeat component.
type ComponentState string

const (
	StateHealthy  ComponentState = "healthy"
	StateDegraded ComponentState = "degraded"
	StateUnknown  ComponentState = "unknown"
)

// ComponentStatus is one liveness fact about one system component.
type ComponentStatus struct {
	Name   string
	State  ComponentState
	Detail string
}

// Heartbeat is a point-in-time liveness snapshot (spec 085 US1).
type Heartbeat struct {
	TS            time.Time
	Components    []ComponentStatus // kernel, gateway, agents — in that order
	WindowHours   int
	MissedReports []string
}

// HeartbeatConfig is the I/O configuration for GatherHeartbeat.
type HeartbeatConfig struct {
	ChitinDir   string        // .chitin event dir — recent-activity source
	KernelBin   string        // installed kernel binary — staleness source
	RepoDir     string        // chitin source repo — staleness source
	InstallLog  string        // install-kernel.jsonl — redeploy-health source
	DeliveryLog string        // operator-report.jsonl — missed-report source
	GatewayUnit string        // systemd --user unit name for the gateway
	Window      time.Duration // activity window
}

// kernelComponent derives the kernel's liveness from the spec-083-US2 health
// signals. An `unknown` on either side yields `unknown` — never `healthy` on
// an absent signal (FR-003). `stale` or a failed redeploy yields `degraded`.
func kernelComponent(ks health.KernelStaleness, rh health.RedeployHealth) ComponentStatus {
	c := ComponentStatus{Name: "kernel"}
	detail := fmt.Sprintf("staleness=%s redeploy=%s", ks.Status, rh.Status)
	switch {
	case ks.Status == health.StalenessUnknown || rh.Status == health.RedeployUnknown:
		c.State, c.Detail = StateUnknown, detail
	case ks.Status == health.StalenessStale || rh.Status == health.RedeployFailed:
		c.State, c.Detail = StateDegraded, detail
	default:
		c.State, c.Detail = StateHealthy, detail
	}
	return c
}

// systemctlComponent maps `systemctl --user is-active <unit>` output to a
// status. `is-active` exits non-zero for inactive/failed units, so the verdict
// keys off the printed word, not the exit code. An unrecognised word — or no
// output at all — is `unknown` (the unit may not exist on this box); it is
// never reported `healthy` without a positive "active".
func systemctlComponent(name, isActiveOutput string, runErr error) ComponentStatus {
	c := ComponentStatus{Name: name}
	switch out := strings.TrimSpace(isActiveOutput); out {
	case "active":
		c.State, c.Detail = StateHealthy, "active"
	case "inactive", "failed":
		c.State, c.Detail = StateDegraded, out
	default:
		c.State = StateUnknown
		if runErr != nil && out == "" {
			c.Detail = fmt.Sprintf("probe failed: %v", runErr)
		} else {
			c.Detail = fmt.Sprintf("unexpected is-active output: %q", out)
		}
	}
	return c
}

// agentsComponent derives swarm-agent liveness from the event surfaces that
// emitted in the activity window. At least one active surface is `healthy`;
// zero surfaces across the whole swarm is `degraded` — nothing is flowing.
func agentsComponent(activeSurfaces []string) ComponentStatus {
	c := ComponentStatus{Name: "agents"}
	if len(activeSurfaces) == 0 {
		c.State = StateDegraded
		c.Detail = "no agent surface emitted events in the window"
		return c
	}
	c.State = StateHealthy
	c.Detail = fmt.Sprintf("%d surface(s) active: %s", len(activeSurfaces), strings.Join(activeSurfaces, ", "))
	return c
}

// GatherHeartbeat composes a Heartbeat from live signals. It never returns an
// error: each component degrades to `unknown`/`degraded` on a probe failure,
// so a heartbeat is always composable (FR-003).
func GatherHeartbeat(cfg HeartbeatConfig) Heartbeat {
	hb := Heartbeat{TS: time.Now().UTC(), WindowHours: int(cfg.Window.Hours())}

	ks := health.GatherKernelStaleness(cfg.KernelBin, cfg.RepoDir)
	rh := health.GatherRedeployHealth(cfg.InstallLog)
	hb.Components = append(hb.Components, kernelComponent(ks, rh))

	out, err := exec.Command("systemctl", "--user", "is-active", cfg.GatewayUnit).Output()
	hb.Components = append(hb.Components, systemctlComponent("gateway", string(out), err))

	rep, _ := health.Gather(cfg.ChitinDir, cfg.Window)
	surfaces := make([]string, 0, len(rep.EventsByWindow))
	for s := range rep.EventsByWindow {
		surfaces = append(surfaces, s)
	}
	sort.Strings(surfaces)
	hb.Components = append(hb.Components, agentsComponent(surfaces))

	hb.MissedReports = missedReports(cfg.DeliveryLog)
	return hb
}

// HeartbeatMessage renders a Heartbeat into a Message for the shared renderer.
func HeartbeatMessage(hb Heartbeat) Message {
	lines := make([]Line, 0, len(hb.Components))
	for _, c := range hb.Components {
		lines = append(lines, Line{Text: fmt.Sprintf("%s: %s — %s", c.Name, c.State, c.Detail)})
	}
	secs := []Section{{Title: "Liveness", Available: true, Lines: lines}}
	if len(hb.MissedReports) > 0 {
		ml := make([]Line, 0, len(hb.MissedReports))
		for _, m := range hb.MissedReports {
			ml = append(ml, Line{Text: m})
		}
		secs = append(secs, Section{Title: "Missed reports since last heartbeat", Available: true, Lines: ml})
	}
	heading := fmt.Sprintf("\U0001FAC0 chitin heartbeat — %s", hb.TS.UTC().Format("2006-01-02 15:04 MST"))
	return Message{Heading: heading, Sections: secs}
}

// deliveryRec is one line of the operator-report.jsonl delivery log.
type deliveryRec struct {
	TS      string `json:"ts"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

// missedReports returns the delivery failures recorded since the last
// successful delivery — the data the next heartbeat surfaces so a missed
// report is never silent (FR-010). A missing log means nothing was missed.
func missedReports(logPath string) []string {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var recs []deliveryRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r deliveryRec
		if json.Unmarshal([]byte(line), &r) == nil {
			recs = append(recs, r)
		}
	}

	lastOK := -1
	for i, r := range recs {
		if r.Outcome == "delivered" {
			lastOK = i
		}
	}
	var missed []string
	for i := lastOK + 1; i < len(recs); i++ {
		if recs[i].Outcome == "failed" {
			missed = append(missed, fmt.Sprintf("%s at %s: %s", recs[i].Kind, recs[i].TS, recs[i].Detail))
		}
	}
	return missed
}
