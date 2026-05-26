package reportfreshness

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	t.Run("valid metadata block parses", func(t *testing.T) {
		meta, err := ParseMetadata(strings.NewReader(`<!doctype html>
<!-- chitin-report-meta: {"generated_at":"2026-05-26T12:00:00Z","source_commands":["make report"],"freshness_sla_hours":2} -->`))
		if err != nil {
			t.Fatalf("ParseMetadata: %v", err)
		}
		if got := meta.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"); got != "2026-05-26T12:00:00Z" {
			t.Fatalf("GeneratedAt = %s", got)
		}
		if meta.FreshnessSLAHours != 2 {
			t.Fatalf("FreshnessSLAHours = %d", meta.FreshnessSLAHours)
		}
	})

	t.Run("malformed JSON returns ErrMetadataUnparseable", func(t *testing.T) {
		_, err := ParseMetadata(strings.NewReader(`<!-- chitin-report-meta: {"generated_at": } -->`))
		if !errors.Is(err, ErrMetadataUnparseable) {
			t.Fatalf("err = %v, want ErrMetadataUnparseable", err)
		}
	})

	t.Run("metadata after first 4 KiB is not found", func(t *testing.T) {
		body := strings.Repeat("x", metadataReadLimit+1) + `<!-- chitin-report-meta: {"generated_at":"2026-05-26T12:00:00Z"} -->`
		_, err := ParseMetadata(strings.NewReader(body))
		if !errors.Is(err, ErrMetadataNotFound) {
			t.Fatalf("err = %v, want ErrMetadataNotFound", err)
		}
	})

	t.Run("multiple metadata blocks first wins", func(t *testing.T) {
		body := `<!-- chitin-report-meta: {"generated_at":"2026-05-26T12:00:00Z"} -->
<!-- chitin-report-meta: {"generated_at":"2026-05-26T13:00:00Z"} -->`
		meta, err := ParseMetadata(strings.NewReader(body))
		if err != nil {
			t.Fatalf("ParseMetadata: %v", err)
		}
		if got := meta.GeneratedAt.Format("15:04"); got != "12:00" {
			t.Fatalf("first block did not win, got %s", got)
		}
	})
}
