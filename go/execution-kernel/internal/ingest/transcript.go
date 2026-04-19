// Package ingest reads Claude Code transcripts and extracts assistant_turn events.
package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// Usage mirrors the assistant_turn payload's usage block.
type Usage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
	ThinkingTokens           *int64 `json:"thinking_tokens,omitempty"`
}

// AssistantTurn is the extracted payload-ready form.
type AssistantTurn struct {
	Text      string `json:"text"`
	Thinking  string `json:"thinking,omitempty"`
	Usage     Usage  `json:"usage"`
	ModelName string `json:"model_name"`
	Ts        string `json:"ts"`
	MessageID string `json:"message_id"`
}

type rawContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

type rawAssistantMessage struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Usage   Usage             `json:"usage"`
	Content []rawContentBlock `json:"content"`
}

type rawLine struct {
	Type      string              `json:"type"`
	Timestamp string              `json:"timestamp"`
	Message   rawAssistantMessage `json:"message"`
}

// ParseAssistantTurns parses a Claude Code transcript (JSONL) and returns all
// assistant_turn extracts in transcript order. user/tool_use/other messages are skipped.
func ParseAssistantTurns(data []byte) ([]AssistantTurn, error) {
	var turns []AssistantTurn
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024*16)
	for scanner.Scan() {
		var line rawLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// Malformed line tolerance: skip.
			continue
		}
		if line.Type != "assistant" {
			continue
		}
		t := AssistantTurn{
			Ts:        line.Timestamp,
			MessageID: line.Message.ID,
			ModelName: line.Message.Model,
			Usage:     line.Message.Usage,
		}
		var textParts, thinkParts []string
		for _, c := range line.Message.Content {
			switch c.Type {
			case "text":
				textParts = append(textParts, c.Text)
			case "thinking":
				thinkParts = append(thinkParts, c.Thinking)
			}
		}
		t.Text = strings.Join(textParts, "")
		t.Thinking = strings.Join(thinkParts, "")
		turns = append(turns, t)
	}
	return turns, scanner.Err()
}

// CheckpointEntry is a single transcript's ingest state.
type CheckpointEntry struct {
	TranscriptPath   string `json:"transcript_path"`
	LastIngestOffset int64  `json:"last_ingest_offset"`
	Status           string `json:"status"` // "complete" | "partial"
}

// LoadCheckpoint reads .chitin/transcript_checkpoint.json.
func LoadCheckpoint(path string) (map[string]CheckpointEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]CheckpointEntry{}, nil
		}
		return nil, err
	}
	if len(b) == 0 || string(b) == "{}" {
		return map[string]CheckpointEntry{}, nil
	}
	var cp map[string]CheckpointEntry
	if err := json.Unmarshal(b, &cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// SaveCheckpoint writes the checkpoint atomically (write temp, rename).
func SaveCheckpoint(path string, cp map[string]CheckpointEntry) error {
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
