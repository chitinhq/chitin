package speclint

// Violation is the canonical shape every L0N rule returns and the
// spec-lint subcommand serialises to JSON (spec 115 FR-003:
// "{rule, file, line, severity, message}"). Lives in its own file so
// every rule in this package can reference the same declaration without
// redeclaring the type when L01-L07 land alongside L04.
type Violation struct {
	Rule     string `json:"rule"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Severity values are a closed set: error gates iteration, warning is
// informational (spec 115 edge case: "Only `error` violations gate the
// iteration").
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)
