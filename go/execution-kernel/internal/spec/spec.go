// Package spec defines the UnifiedSpec model — the normalized shape that all
// framework adapters produce and L2–L7 consume (spec 061).
//
// The canonical contract is the JSON Schema at
// libs/contracts/schemas/unified-spec.schema.json; this Go package is a
// typed mirror validated against it in CI.
package spec

// SpecStatus is the lifecycle status of a spec.
type SpecStatus string

const (
	SpecStatusDraft      SpecStatus = "draft"
	SpecStatusRatified   SpecStatus = "ratified"
	SpecStatusSuperseded SpecStatus = "superseded"
)

// SourceFramework identifies which framework adapter produced this UnifiedSpec.
type SourceFramework string

const (
	SourceFrameworkSpecKit     SourceFramework = "spec-kit"
	SourceFrameworkOpenSpec    SourceFramework = "openspec"
	SourceFrameworkSuperpowers SourceFramework = "superpowers"
	SourceFrameworkHouse       SourceFramework = "house"
)

// Requirement is a single requirement (R1, R2, …) within a spec.
type Requirement struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// AcceptanceCriterion is a single acceptance criterion (AC1, AC2, …).
type AcceptanceCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Slice is a delivery slice within a spec.
type Slice struct {
	ID             string   `json:"id"`
	Scope          string   `json:"scope"`
	RequirementIDs []string `json:"requirement_ids"`
}

// Question is an open / unresolved question within a spec.
type Question struct {
	ID        string  `json:"id"`
	Text      string  `json:"text"`
	Proposed  *string `json:"proposed,omitempty"`
}

// UnifiedSpec is the normalized shape produced by all framework adapters.
// Fields map 1:1 to the JSON Schema at libs/contracts/schemas/unified-spec.schema.json.
type UnifiedSpec struct {
	SpecID          string                `json:"spec_id"`
	Title           string                `json:"title"`
	Status          SpecStatus            `json:"status"`
	SourceFramework SourceFramework       `json:"source_framework"`
	SourcePath      string                `json:"source_path"`
	Requirements    []Requirement          `json:"requirements"`
	Acceptance      []AcceptanceCriterion  `json:"acceptance"`
	Boundaries      []string              `json:"boundaries"`
	Slices          []Slice               `json:"slices"`
	OpenQuestions   []Question            `json:"open_questions"`
}