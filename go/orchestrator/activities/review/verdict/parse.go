package verdict

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ParseError separates a JSON parse failure from a schema-validation
// failure so the dispatch activity can route to FailureMalformedJSON
// vs FailureMalformedShape per spec 094 FR-014 / outcome.go FailureKinds.
// Callers should errors.Is(err, ErrMalformedJSON) to detect parse-time
// failures; everything else is a Validate (shape) failure.
type ParseError struct {
	// Kind names which stage failed: "malformed_json" or "malformed_shape".
	// Mirrors the FailureKind string values so the activity can map
	// directly without re-interpreting.
	Kind string
	// Detail is the underlying error message.
	Detail string
}

// Error renders "<kind>: <detail>" so the kind tag is always present.
func (e *ParseError) Error() string { return e.Kind + ": " + e.Detail }

// ErrMalformedJSON is the sentinel a ParseError wraps when JSON decoding
// fails. Use errors.Is(err, ErrMalformedJSON) to detect.
var ErrMalformedJSON = errors.New("verdict: malformed JSON")

// ErrMalformedShape is the sentinel a ParseError wraps when JSON decoded
// successfully but Validate rejected the schema. Use errors.Is.
var ErrMalformedShape = errors.New("verdict: malformed shape")

// Unwrap lets errors.Is reach the sentinel. Returned by ParseError.
func (e *ParseError) Unwrap() error {
	switch e.Kind {
	case "malformed_json":
		return ErrMalformedJSON
	case "malformed_shape":
		return ErrMalformedShape
	default:
		return nil
	}
}

// ParseStructured decodes raw bytes as a StructuredVerdict and validates
// against the FR-014 invariants. It is the single entry point the
// review-dispatch activity uses to translate a driver's
// Result.Explanation into either a valid Outcome.Verdict or a closed
// Failure with the right FailureKind.
//
// Returns a typed *ParseError on failure so the caller can read .Kind to
// branch on FailureMalformedJSON vs FailureMalformedShape, OR use
// errors.Is(err, ErrMalformedJSON / ErrMalformedShape).
//
// Empty input is treated as malformed_json (canonical "the driver
// returned nothing").
func ParseStructured(raw []byte) (StructuredVerdict, error) {
	if len(raw) == 0 {
		return StructuredVerdict{}, &ParseError{
			Kind:   "malformed_json",
			Detail: "empty input",
		}
	}
	var v StructuredVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return StructuredVerdict{}, &ParseError{
			Kind:   "malformed_json",
			Detail: fmt.Sprintf("json.Unmarshal: %v", err),
		}
	}
	if err := Validate(v); err != nil {
		return StructuredVerdict{}, &ParseError{
			Kind:   "malformed_shape",
			Detail: err.Error(),
		}
	}
	return v, nil
}
