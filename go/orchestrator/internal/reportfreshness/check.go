package reportfreshness

import (
	"context"
	"errors"
	"os"
	"time"
)

const (
	AgeSourceEmbedded = "embedded"
	AgeSourceMTime    = "mtime"
)

type Status string

const (
	StatusFresh   Status = "fresh"
	StatusStale   Status = "stale"
	StatusMissing Status = "missing"
)

type ReportStatus struct {
	Path        string    `json:"path"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	AgeHours    float64   `json:"age_hours"`
	SLAHours    int       `json:"sla_hours"`
	AgeSource   string    `json:"age_source,omitempty"`
	Status      Status    `json:"status"`
}

type StaleReport = ReportStatus

type Result struct {
	Checked   int            `json:"checked"`
	Fresh     []ReportStatus `json:"fresh"`
	Stale     []StaleReport  `json:"stale"`
	Missing   []string       `json:"missing"`
	Rows      []ReportStatus `json:"rows"`
	ClockSkew bool           `json:"clock_skew"`
}

func Check(ctx context.Context, paths []WatchedPath, now time.Time) (Result, error) {
	var out Result
	for _, watched := range paths {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		out.Checked++
		row := ReportStatus{Path: watched.Path, SLAHours: watched.SLAHours}
		info, err := os.Stat(watched.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				row.Status = StatusMissing
				out.Missing = append(out.Missing, watched.Path)
				out.Rows = append(out.Rows, row)
				continue
			}
			return out, err
		}
		if info.IsDir() {
			row.Status = StatusMissing
			out.Missing = append(out.Missing, watched.Path)
			out.Rows = append(out.Rows, row)
			continue
		}
		generatedAt := info.ModTime()
		ageSource := AgeSourceMTime
		slaHours := watched.SLAHours
		if meta, err := ParseMetadataFile(watched.Path); err == nil {
			generatedAt = meta.GeneratedAt
			ageSource = AgeSourceEmbedded
			if meta.FreshnessSLAHours > 0 {
				slaHours = meta.FreshnessSLAHours
			}
		} else if err != nil && !errors.Is(err, ErrMetadataNotFound) && !errors.Is(err, ErrMetadataUnparseable) {
			return out, err
		}
		ageHours := now.Sub(generatedAt).Hours()
		if ageHours < 0 {
			out.ClockSkew = true
		}
		row.GeneratedAt = generatedAt.UTC()
		row.AgeHours = ageHours
		row.AgeSource = ageSource
		row.SLAHours = slaHours
		if ageHours > float64(slaHours) {
			row.Status = StatusStale
			out.Stale = append(out.Stale, row)
		} else {
			row.Status = StatusFresh
			out.Fresh = append(out.Fresh, row)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}
