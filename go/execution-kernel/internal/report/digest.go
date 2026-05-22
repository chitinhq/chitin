package report

import (
	"fmt"
	"time"
)

// TelemetryDigest is a composed daily operator digest (spec 085 US2) — exactly
// four sections, in a fixed order, each either available or marked unavailable.
type TelemetryDigest struct {
	TS       time.Time
	Window   time.Duration
	OnDemand bool
	Sections []Section
}

// GatherDigest composes the digest from live telemetry. It never returns an
// error: each of the four section gatherers self-degrades to an unavailable
// Section on a source failure, so the digest is always composable (FR-009).
// The four sections are always present, in this order: orchestration, kernel,
// driver activity, PRs shipped.
func GatherDigest(s DigestSources, onDemand bool) TelemetryDigest {
	return TelemetryDigest{
		TS:       time.Now().UTC(),
		Window:   s.Window,
		OnDemand: onDemand,
		Sections: []Section{
			orchestrationSection(s),
			kernelSection(s),
			driversSection(s),
			prsSection(s),
		},
	}
}

// DigestMessage renders a TelemetryDigest into a Message for the shared
// renderer. Pure — the message is a deterministic function of the digest.
func DigestMessage(d TelemetryDigest) Message {
	scope := "daily"
	if d.OnDemand {
		scope = "on-demand"
	}
	heading := fmt.Sprintf("\U0001F4CA chitin telemetry digest (%s) — %s, last %s",
		scope, d.TS.Format("2006-01-02 15:04 MST"), d.Window)
	return Message{Heading: heading, Sections: d.Sections}
}
