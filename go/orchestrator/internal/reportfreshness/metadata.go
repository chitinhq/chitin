package reportfreshness

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"time"
)

const metadataReadLimit = 4096

var (
	ErrMetadataNotFound    = errors.New("report metadata not found")
	ErrMetadataUnparseable = errors.New("report metadata unparseable")
	metadataRE             = regexp.MustCompile(`(?is)<!--\s*chitin-report-meta:\s*({[^}]*})\s*-->`)
)

type Metadata struct {
	GeneratedAt       time.Time `json:"generated_at"`
	SourceWindowStart string    `json:"source_window_start,omitempty"`
	SourceWindowEnd   string    `json:"source_window_end,omitempty"`
	SourceCommands    []string  `json:"source_commands,omitempty"`
	FreshnessSLAHours int       `json:"freshness_sla_hours,omitempty"`
	BoardDBPath       string    `json:"board_db_path,omitempty"`
	BoardSlug         string    `json:"board_slug,omitempty"`
}

func ParseMetadataFile(path string) (*Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseMetadata(f)
}

func ParseMetadata(r io.Reader) (*Metadata, error) {
	buf := make([]byte, metadataReadLimit)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	window := buf[:n]
	match := metadataRE.FindSubmatch(window)
	if len(match) < 2 {
		return nil, ErrMetadataNotFound
	}
	var raw struct {
		GeneratedAt       string   `json:"generated_at"`
		SourceWindowStart string   `json:"source_window_start"`
		SourceWindowEnd   string   `json:"source_window_end"`
		SourceCommands    []string `json:"source_commands"`
		FreshnessSLAHours int      `json:"freshness_sla_hours"`
		BoardDBPath       string   `json:"board_db_path"`
		BoardSlug         string   `json:"board_slug"`
	}
	if err := json.Unmarshal(match[1], &raw); err != nil {
		return nil, ErrMetadataUnparseable
	}
	generatedAt, err := time.Parse(time.RFC3339, raw.GeneratedAt)
	if err != nil {
		generatedAt, err = time.Parse(time.RFC3339Nano, raw.GeneratedAt)
	}
	if err != nil {
		return nil, ErrMetadataUnparseable
	}
	return &Metadata{
		GeneratedAt:       generatedAt,
		SourceWindowStart: raw.SourceWindowStart,
		SourceWindowEnd:   raw.SourceWindowEnd,
		SourceCommands:    raw.SourceCommands,
		FreshnessSLAHours: raw.FreshnessSLAHours,
		BoardDBPath:       raw.BoardDBPath,
		BoardSlug:         raw.BoardSlug,
	}, nil
}
