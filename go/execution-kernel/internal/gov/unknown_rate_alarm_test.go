package gov

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const defaultUnknownRateThreshold = 0.01

var intentionalUnknownTools = map[string]struct{}{
	"clarify":        {},
	"image_generate": {},
	"text_to_speech": {},
	"vision_analyze": {},
	"cronjob":        {},
}

type unknownRateSummary struct {
	Agent           string
	Day             string
	Total           int
	Unknown         int
	Rate            float64
	TopUnknownTools []unknownToolCount
	Samples         []unknownSample
}

type unknownToolCount struct {
	Tool  string
	Count int
}

type unknownSample struct {
	Tool     string
	Locator  string
	RuleID   string
	Filename string
	Line     int
}

type unknownDecisionWire struct {
	Agent        string `json:"agent"`
	ActionType   string `json:"action_type"`
	ActionTarget string `json:"action_target"`
	RuleID       string `json:"rule_id"`
	Ts           string `json:"ts"`
	ChainID      string `json:"chain_id"`
	Seq          *int64 `json:"seq"`
}

func TestUnknownRateAlarmFixtures(t *testing.T) {
	cases := []struct {
		name       string
		fixture    string
		wantAlarm  bool
		wantRows   int
		assertions func(t *testing.T, rows []unknownRateSummary, alarms []unknownRateSummary)
	}{
		{
			name:      "empty file",
			fixture:   "empty-file",
			wantAlarm: false,
			wantRows:  0,
		},
		{
			name:      "all known",
			fixture:   "all-known",
			wantAlarm: false,
			wantRows:  2,
		},
		{
			name:      "all unknown",
			fixture:   "all-unknown",
			wantAlarm: true,
			wantRows:  1,
			assertions: func(t *testing.T, rows []unknownRateSummary, alarms []unknownRateSummary) {
				t.Helper()
				if len(alarms) != 1 {
					t.Fatalf("want 1 alarm, got %d", len(alarms))
				}
				got := alarms[0]
				if got.Agent != "hermes" || got.Day != "2026-05-07" {
					t.Fatalf("wrong alarm bucket: %+v", got)
				}
				if got.Total != 4 || got.Unknown != 4 || got.Rate != 1 {
					t.Fatalf("wrong all-unknown rate: %+v", got)
				}
				wantTools := []unknownToolCount{{Tool: "kanban_show", Count: 2}, {Tool: "kanban_block", Count: 1}}
				if len(got.TopUnknownTools) < len(wantTools) {
					t.Fatalf("top tools too short: %+v", got.TopUnknownTools)
				}
				for i, want := range wantTools {
					if got.TopUnknownTools[i] != want {
						t.Fatalf("top tool %d: got %+v want %+v", i, got.TopUnknownTools[i], want)
					}
				}
				if len(got.Samples) == 0 || got.Samples[0].Locator != "chain-hermes:7" {
					t.Fatalf("sample locator should prefer chain_id:seq, got %+v", got.Samples)
				}
				_ = rows
			},
		},
		{
			name:      "mixed with whitelist",
			fixture:   "mixed-with-whitelist",
			wantAlarm: true,
			wantRows:  1,
			assertions: func(t *testing.T, rows []unknownRateSummary, alarms []unknownRateSummary) {
				t.Helper()
				if len(rows) != 1 {
					t.Fatalf("want one row, got %+v", rows)
				}
				got := rows[0]
				if got.Total != 4 || got.Unknown != 1 {
					t.Fatalf("whitelisted unknowns must be excluded from total and unknown counts: %+v", got)
				}
				if len(got.TopUnknownTools) != 1 || got.TopUnknownTools[0] != (unknownToolCount{Tool: "skills_list", Count: 1}) {
					t.Fatalf("top unknowns should exclude intentional tools, got %+v", got.TopUnknownTools)
				}
				for _, sample := range got.Samples {
					if _, ok := intentionalUnknownTools[sample.Tool]; ok {
						t.Fatalf("sample includes whitelisted tool: %+v", sample)
					}
				}
				if len(alarms) != 1 {
					t.Fatalf("want one alarm, got %+v", alarms)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := analyzeUnknownRateDir(filepath.Join("testdata", "unknown_rate_alarm", tc.fixture), 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.wantRows {
				t.Fatalf("row count: got %d want %d (%+v)", len(rows), tc.wantRows, rows)
			}
			alarms := rowsExceedingUnknownRate(rows, defaultUnknownRateThreshold)
			if gotAlarm := len(alarms) > 0; gotAlarm != tc.wantAlarm {
				t.Fatalf("alarm presence: got %v want %v; rows=%+v", gotAlarm, tc.wantAlarm, rows)
			}
			if tc.assertions != nil {
				tc.assertions(t, rows, alarms)
			}
		})
	}
}

func TestUnknownRateAlarmFailureMessageIncludesTopToolsAndSamples(t *testing.T) {
	rows, err := analyzeUnknownRateDir(filepath.Join("testdata", "unknown_rate_alarm", "all-unknown"), 2)
	if err != nil {
		t.Fatal(err)
	}
	alarms := rowsExceedingUnknownRate(rows, defaultUnknownRateThreshold)
	msg := formatUnknownRateAlarm(alarms, defaultUnknownRateThreshold)

	for _, want := range []string{
		"hermes 2026-05-07 unknown_rate=100.00%",
		"top_unknown_tools=kanban_show=2, kanban_block=1",
		"samples=chain-hermes:7 kanban_show",
		"chain-hermes:8 process",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("failure message missing %q:\n%s", want, msg)
		}
	}
}

func TestUnknownRateAlarmRejectsMalformedJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gov-decisions-2026-05-11.jsonl")
	if err := os.WriteFile(path, []byte("{not-json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := analyzeUnknownRateDir(dir, 3)
	if err == nil {
		t.Fatal("expected malformed JSONL to fail closed")
	}
	if !strings.Contains(err.Error(), "malformed JSON") || !strings.Contains(err.Error(), ":1:") {
		t.Fatalf("error should identify malformed JSON line, got %v", err)
	}
}

func TestUnknownRateAlarmCurrentChainOptIn(t *testing.T) {
	dir := os.Getenv("CHITIN_UNKNOWN_RATE_DIR")
	if dir == "" {
		t.Skip("set CHITIN_UNKNOWN_RATE_DIR to a chitin state dir to run the live unknown-rate alarm")
	}
	rows, err := analyzeUnknownRateDir(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if day := os.Getenv("CHITIN_UNKNOWN_RATE_DAY"); day != "" {
		rows = filterUnknownRateRowsByDay(rows, day)
	}
	alarms := rowsExceedingUnknownRate(rows, defaultUnknownRateThreshold)
	if len(alarms) > 0 {
		t.Fatal(formatUnknownRateAlarm(alarms, defaultUnknownRateThreshold))
	}
}

func analyzeUnknownRateDir(dir string, topN int) ([]unknownRateSummary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	if topN <= 0 {
		topN = 1
	}

	buckets := make(map[string]*unknownRateAccumulator)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "gov-decisions-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := scanUnknownRateFile(path, name, topN, buckets); err != nil {
			return nil, err
		}
	}

	rows := make([]unknownRateSummary, 0, len(buckets))
	for _, acc := range buckets {
		row := acc.summary(topN)
		if row.Total == 0 {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Day != rows[j].Day {
			return rows[i].Day < rows[j].Day
		}
		return rows[i].Agent < rows[j].Agent
	})
	return rows, nil
}

type unknownRateAccumulator struct {
	agent         string
	day           string
	total         int
	unknown       int
	unknownByTool map[string]int
	samples       []unknownSample
}

func (a *unknownRateAccumulator) summary(topN int) unknownRateSummary {
	tools := make([]unknownToolCount, 0, len(a.unknownByTool))
	for tool, count := range a.unknownByTool {
		tools = append(tools, unknownToolCount{Tool: tool, Count: count})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Count != tools[j].Count {
			return tools[i].Count > tools[j].Count
		}
		return tools[i].Tool < tools[j].Tool
	})
	if len(tools) > topN {
		tools = tools[:topN]
	}
	rate := 0.0
	if a.total > 0 {
		rate = float64(a.unknown) / float64(a.total)
	}
	return unknownRateSummary{
		Agent:           a.agent,
		Day:             a.day,
		Total:           a.total,
		Unknown:         a.unknown,
		Rate:            rate,
		TopUnknownTools: tools,
		Samples:         append([]unknownSample(nil), a.samples...),
	}
}

func scanUnknownRateFile(path, filename string, topN int, buckets map[string]*unknownRateAccumulator) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	fallbackDay := dayFromDecisionFilename(filename)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row unknownDecisionWire
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return fmt.Errorf("malformed JSON in %s:%d: %w", path, lineNo, err)
		}
		if row.ActionType == "" {
			continue
		}
		if row.ActionType == string(ActUnknown) {
			if _, ok := intentionalUnknownTools[row.ActionTarget]; ok {
				continue
			}
		}

		agent := row.Agent
		if agent == "" {
			agent = "(unknown-agent)"
		}
		day := dayFromDecision(row.Ts, fallbackDay)
		key := agent + "\x00" + day
		acc := buckets[key]
		if acc == nil {
			acc = &unknownRateAccumulator{
				agent:         agent,
				day:           day,
				unknownByTool: make(map[string]int),
			}
			buckets[key] = acc
		}
		acc.total++
		if row.ActionType != string(ActUnknown) {
			continue
		}
		acc.unknown++
		acc.unknownByTool[row.ActionTarget]++
		if len(acc.samples) < topN {
			acc.samples = append(acc.samples, unknownSample{
				Tool:     row.ActionTarget,
				Locator:  sampleLocator(row, filename, lineNo),
				RuleID:   row.RuleID,
				Filename: filename,
				Line:     lineNo,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func rowsExceedingUnknownRate(rows []unknownRateSummary, threshold float64) []unknownRateSummary {
	var alarms []unknownRateSummary
	for _, row := range rows {
		if row.Total > 0 && row.Rate >= threshold {
			alarms = append(alarms, row)
		}
	}
	return alarms
}

func filterUnknownRateRowsByDay(rows []unknownRateSummary, day string) []unknownRateSummary {
	out := make([]unknownRateSummary, 0, len(rows))
	for _, row := range rows {
		if row.Day == day {
			out = append(out, row)
		}
	}
	return out
}

func formatUnknownRateAlarm(rows []unknownRateSummary, threshold float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown-rate alarm: threshold must stay under %.2f%%\n", threshold*100)
	for _, row := range rows {
		fmt.Fprintf(&b, "%s %s unknown_rate=%.2f%% unknown=%d total=%d",
			row.Agent, row.Day, row.Rate*100, row.Unknown, row.Total)
		if len(row.TopUnknownTools) > 0 {
			fmt.Fprintf(&b, " top_unknown_tools=%s", formatUnknownToolCounts(row.TopUnknownTools))
		}
		if len(row.Samples) > 0 {
			fmt.Fprintf(&b, " samples=%s", formatUnknownSamples(row.Samples))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatUnknownToolCounts(counts []unknownToolCount) string {
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", count.Tool, count.Count))
	}
	return strings.Join(parts, ", ")
}

func formatUnknownSamples(samples []unknownSample) string {
	parts := make([]string, 0, len(samples))
	for _, sample := range samples {
		parts = append(parts, fmt.Sprintf("%s %s", sample.Locator, sample.Tool))
	}
	return strings.Join(parts, ", ")
}

func sampleLocator(row unknownDecisionWire, filename string, lineNo int) string {
	if row.ChainID != "" && row.Seq != nil {
		return fmt.Sprintf("%s:%d", row.ChainID, *row.Seq)
	}
	return fmt.Sprintf("%s:%d", filename, lineNo)
}

func dayFromDecision(ts string, fallback string) string {
	if ts == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fallback
	}
	return parsed.UTC().Format("2006-01-02")
}

func dayFromDecisionFilename(name string) string {
	day := strings.TrimPrefix(name, "gov-decisions-")
	day = strings.TrimSuffix(day, ".jsonl")
	if day == name {
		return "unknown-day"
	}
	return day
}
