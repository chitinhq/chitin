package manifest

import (
	"crypto/rand"
	"fmt"
	"regexp"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type DriverIdentity struct {
	User               string `json:"user"`
	MachineID          string `json:"machine_id"`
	MachineFingerprint string `json:"machine_fingerprint"`
}

type RunManifest struct {
	SchemaVersion    string            `json:"schema_version"`
	RunID            string            `json:"run_id"`
	SessionID        string            `json:"session_id"`
	Surface          string            `json:"surface"`
	DriverIdentity   DriverIdentity    `json:"driver_identity"`
	AgentInstanceID  string            `json:"agent_instance_id"`
	ParentAgentID    *string           `json:"parent_agent_id"`
	AgentFingerprint string            `json:"agent_fingerprint"`
	Labels           map[string]string `json:"labels"`
}

type RunManifestInput struct {
	RunID            string
	SessionID        string
	Surface          string
	DriverIdentity   DriverIdentity
	AgentInstanceID  string
	ParentAgentID    *string
	AgentFingerprint string
	Labels           map[string]string
}

func NewRunManifest(input RunManifestInput) (RunManifest, error) {
	runID := input.RunID
	if runID == "" {
		var err error
		runID, err = newUUID()
		if err != nil {
			return RunManifest{}, err
		}
	} else if !isUUID(runID) {
		return RunManifest{}, fmt.Errorf("run_id must be a UUID")
	}
	sessionID := input.SessionID
	if sessionID == "" {
		var err error
		sessionID, err = newUUID()
		if err != nil {
			return RunManifest{}, err
		}
	} else if !isUUID(sessionID) {
		return RunManifest{}, fmt.Errorf("session_id must be a UUID")
	}
	agentInstanceID := input.AgentInstanceID
	if agentInstanceID == "" {
		var err error
		agentInstanceID, err = newUUID()
		if err != nil {
			return RunManifest{}, err
		}
	} else if !isUUID(agentInstanceID) {
		return RunManifest{}, fmt.Errorf("agent_instance_id must be a UUID")
	}
	// Copy the ParentAgentID value rather than storing the caller's pointer:
	// a later mutation of *input.ParentAgentID would otherwise change the
	// manifest identity (and every event hashed from it) after the fact.
	var parentAgentID *string
	if input.ParentAgentID != nil {
		if !isUUID(*input.ParentAgentID) {
			return RunManifest{}, fmt.Errorf("parent_agent_id must be a UUID")
		}
		v := *input.ParentAgentID
		parentAgentID = &v
	}
	if input.Surface == "" {
		return RunManifest{}, fmt.Errorf("surface is required")
	}
	if !isLowerHex64(input.DriverIdentity.MachineFingerprint) {
		return RunManifest{}, fmt.Errorf("driver_identity.machine_fingerprint must be 64 lowercase hex chars")
	}
	if !isLowerHex64(input.AgentFingerprint) {
		return RunManifest{}, fmt.Errorf("agent_fingerprint must be 64 lowercase hex chars")
	}
	return RunManifest{
		SchemaVersion:    "2",
		RunID:            runID,
		SessionID:        sessionID,
		Surface:          input.Surface,
		DriverIdentity:   input.DriverIdentity,
		AgentInstanceID:  agentInstanceID,
		ParentAgentID:    parentAgentID,
		AgentFingerprint: input.AgentFingerprint,
		Labels:           cloneLabels(input.Labels),
	}, nil
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func isLowerHex64(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, r := range input {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func isUUID(input string) bool {
	return uuidPattern.MatchString(input)
}
