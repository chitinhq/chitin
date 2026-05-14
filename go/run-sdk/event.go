package manifest

type Event struct {
	SchemaVersion    string            `json:"schema_version"`
	RunID            string            `json:"run_id"`
	SessionID        string            `json:"session_id"`
	Surface          string            `json:"surface"`
	DriverIdentity   DriverIdentity    `json:"driver_identity"`
	AgentInstanceID  string            `json:"agent_instance_id"`
	ParentAgentID    *string           `json:"parent_agent_id"`
	AgentFingerprint string            `json:"agent_fingerprint"`
	EventType        string            `json:"event_type"`
	ChainID          string            `json:"chain_id"`
	ChainType        string            `json:"chain_type"`
	ParentChainID    *string           `json:"parent_chain_id"`
	Seq              int64             `json:"seq"`
	PrevHash         *string           `json:"prev_hash"`
	ThisHash         string            `json:"this_hash"`
	Ts               string            `json:"ts"`
	Labels           map[string]string `json:"labels"`
	Payload          any               `json:"payload"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Model struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Version       string `json:"version,omitempty"`
	ContextWindow int64  `json:"context_window,omitempty"`
}

type SessionStartPayload struct {
	Cwd               string     `json:"cwd"`
	ClientInfo        ClientInfo `json:"client_info"`
	Model             Model      `json:"model"`
	SystemPromptHash  string     `json:"system_prompt_hash"`
	ToolAllowlistHash string     `json:"tool_allowlist_hash"`
	AgentVersion      string     `json:"agent_version"`
}

type Totals struct {
	TurnCount         int64 `json:"turn_count"`
	ToolCallCount     int64 `json:"tool_call_count"`
	TotalInputTokens  int64 `json:"total_input_tokens"`
	TotalOutputTokens int64 `json:"total_output_tokens"`
	TotalDurationMs   int64 `json:"total_duration_ms"`
}

type SessionEndPayload struct {
	Reason string `json:"reason"`
	Totals Totals `json:"totals"`
}
