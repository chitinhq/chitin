// Package speclint hosts deterministic spec-PR consistency rules (spec 115 FR-003).
package speclint

// Violation is the shared output contract for every L0N rule (spec 115 FR-003).
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
