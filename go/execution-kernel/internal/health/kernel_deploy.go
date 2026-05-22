package health

import (
	"bufio"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Staleness status values for KernelStaleness.Status.
const (
	StalenessCurrent = "current" // running binary's revision == source HEAD
	StalenessStale   = "stale"   // running binary predates the merged source
	StalenessUnknown = "unknown" // a revision could not be determined
)

// KernelStaleness reports whether the installed kernel binary reflects the
// current kernel source — the signal that catches a merged fix which never
// reached the running binary (spec 083 US2, FR-011).
type KernelStaleness struct {
	Status          string `json:"status"`
	RunningRevision string `json:"running_revision,omitempty"`
	SourceRevision  string `json:"source_revision,omitempty"`
	BuildModified   bool   `json:"build_modified,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

// classifyStaleness is the pure core: given a running-binary revision and a
// source-HEAD revision, decide current / stale / unknown. An empty revision on
// either side is unknown — never silently "current". Kept separate from the
// I/O so the boundary cases are unit-testable without a binary or a git repo.
func classifyStaleness(running, source string) (status, detail string) {
	if running == "" || source == "" {
		return StalenessUnknown, "missing revision"
	}
	if running == source {
		return StalenessCurrent, ""
	}
	return StalenessStale, fmt.Sprintf("running %s, source %s", shortRev(running), shortRev(source))
}

func shortRev(r string) string {
	if len(r) > 12 {
		return r[:12]
	}
	return r
}

// kernelBinaryRevision reads the VCS revision the Go toolchain stamps into a
// binary (vcs.revision / vcs.modified are embedded automatically when `go
// build` runs from a git checkout). install-kernel.sh builds inside the repo,
// so the installed kernel always carries this.
func kernelBinaryRevision(binPath string) (rev string, modified bool, err error) {
	bi, err := buildinfo.ReadFile(binPath)
	if err != nil {
		return "", false, fmt.Errorf("read build info %q: %w", binPath, err)
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return rev, modified, nil
}

// gitHeadRevision returns the HEAD commit of the kernel source checkout. After
// install-kernel.sh fast-forwards, HEAD == origin/main — the merged source.
func gitHeadRevision(repoDir string) (string, error) {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %q: %w", repoDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GatherKernelStaleness compares the kernel binary at binPath against the
// source HEAD in repoDir. It never returns an error: any I/O failure yields
// Status=unknown with a Detail — a health probe must degrade, not black-box,
// and must never report "current" when it could not actually check.
func GatherKernelStaleness(binPath, repoDir string) KernelStaleness {
	running, modified, err := kernelBinaryRevision(binPath)
	if err != nil {
		return KernelStaleness{Status: StalenessUnknown, Detail: err.Error()}
	}
	source, err := gitHeadRevision(repoDir)
	if err != nil {
		return KernelStaleness{
			Status:          StalenessUnknown,
			RunningRevision: running,
			BuildModified:   modified,
			Detail:          err.Error(),
		}
	}
	status, detail := classifyStaleness(running, source)
	return KernelStaleness{
		Status:          status,
		RunningRevision: running,
		SourceRevision:  source,
		BuildModified:   modified,
		Detail:          detail,
	}
}

// Redeploy status values for RedeployHealth.Status.
const (
	RedeployOK      = "ok"      // last install-kernel run succeeded or no-op'd
	RedeployFailed  = "failed"  // last install-kernel run failed or rolled back
	RedeployUnknown = "unknown" // no readable install-kernel log
)

// RedeployHealth reports the outcome of the most recent install-kernel run,
// read from the structured install-kernel.jsonl log. It makes a redeploy
// failure operator-visible through `chitin health` instead of leaving it as an
// unwatched line in the log (spec 083 US2, FR-010).
type RedeployHealth struct {
	Status   string    `json:"status"`
	LastKind string    `json:"last_kind,omitempty"`
	LastMsg  string    `json:"last_msg,omitempty"`
	LastTs   time.Time `json:"last_ts,omitempty"`
}

// redeployFailureKinds are the install-kernel.sh `emit` kinds that mean the
// redeploy did not succeed. `fail` precedes a non-zero exit; `rollback` means a
// build/smoke failure forced the prior binary back. `ok`, `noop`, `deferred`
// and `warn` (a non-fatal sub-step warning) are not redeploy failures.
var redeployFailureKinds = map[string]bool{
	"fail":     true,
	"rollback": true,
}

// classifyRedeploy is the pure core: map the last log line's `kind` to a
// redeploy status. An empty/unrecognised kind is unknown, not a silent ok.
func classifyRedeploy(lastKind string) string {
	if lastKind == "" {
		return RedeployUnknown
	}
	if redeployFailureKinds[lastKind] {
		return RedeployFailed
	}
	return RedeployOK
}

// GatherRedeployHealth reads the last line of the install-kernel.jsonl log and
// reports the most recent redeploy outcome. A missing log is unknown (the
// redeploy timer may simply not have run yet) — never reported as ok.
func GatherRedeployHealth(logPath string) RedeployHealth {
	last, err := lastNonEmptyLine(logPath)
	if err != nil {
		return RedeployHealth{Status: RedeployUnknown, LastMsg: err.Error()}
	}
	if last == "" {
		return RedeployHealth{Status: RedeployUnknown}
	}
	var rec struct {
		TS   string `json:"ts"`
		Kind string `json:"kind"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(last), &rec); err != nil {
		return RedeployHealth{Status: RedeployUnknown, LastMsg: "unparseable last log line"}
	}
	rh := RedeployHealth{
		Status:   classifyRedeploy(rec.Kind),
		LastKind: rec.Kind,
		LastMsg:  rec.Msg,
	}
	if t, err := time.Parse(time.RFC3339, rec.TS); err == nil {
		rh.LastTs = t
	}
	return rh
}

// lastNonEmptyLine returns the final non-blank line of a file. A missing file
// returns ("", nil) — its absence is meaningful to the caller, not an error.
func lastNonEmptyLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			last = line
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("scan %q: %w", path, err)
	}
	return last, nil
}
