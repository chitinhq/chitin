package copilot

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// flexInt64 is an int64 that unmarshals from a JSON number, a numeric JSON
// string, or an RFC3339 JSON string. It never fails the decode: an
// unrecognized value leaves the timestamp at zero rather than aborting the
// whole response.
//
// Chitin fork patch (spec 083 US3 / research Decision 1). Upstream
// copilot-sdk v0.2.2 typed `timestamp` as a bare int64; the Copilot CLI
// v1.x emits it as a string, which made every `drive copilot` session fail
// at startup with:
//
//	json: cannot unmarshal string into Go struct field PingResponse.timestamp of type int64
//
// The timestamp value is informational only — no SDK code computes on it —
// so a tolerant decoder is the correct, minimal fix.
type flexInt64 int64

// UnmarshalJSON accepts a JSON number, a numeric string, or an RFC3339
// string. It never returns an error; an unparseable value yields zero.
func (f *flexInt64) UnmarshalJSON(data []byte) error {
	*f = 0
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		return nil
	}
	// Bare JSON number.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt64(n)
		return nil
	}
	// Quoted JSON string — a numeric epoch or an RFC3339 instant.
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		// Not a string and not a number — leave zero, do not fail decode.
		return nil
	}
	str = strings.TrimSpace(str)
	if n, err := strconv.ParseInt(str, 10, 64); err == nil {
		*f = flexInt64(n)
		return nil
	}
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		*f = flexInt64(t.Unix())
	}
	return nil
}
