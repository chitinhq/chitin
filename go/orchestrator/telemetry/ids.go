package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
)

// TraceIDForRun derives a deterministic 32-hex-char (16-byte) OTLP trace id
// from a scheduler run id. Every tick span of one run shares this trace id,
// so a collector groups a run's ticks into a single trace.
//
// The run id may be any string (a UUID, a Temporal workflow id, a test
// fixture). Hashing it — rather than stripping hyphens from a UUID — yields a
// valid trace id for every input shape, which is the property the OTLP wire
// format requires (exactly 32 lowercase hex chars).
func TraceIDForRun(runID string) string {
	sum := sha256.Sum256([]byte("chitin-orchestrator/run/" + runID))
	return hex.EncodeToString(sum[:16]) // first 16 bytes → 32 hex chars
}

// SpanIDForTick derives a deterministic 16-hex-char (8-byte) OTLP span id for
// one tick of a run. The run id is folded in so two runs' tick-N spans never
// collide within a shared trace namespace.
func SpanIDForTick(runID string, tick int) string {
	h := sha256.New()
	// A length-prefixed-style separator keeps ("ab",1) distinct from ("a",
	// "b1") — the colon cannot appear ambiguously across the two fields.
	h.Write([]byte("chitin-orchestrator/tick/"))
	h.Write([]byte(runID))
	h.Write([]byte{0})
	h.Write(itoaBytes(tick))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8]) // first 8 bytes → 16 hex chars
}

// itoaBytes renders a non-negative int as its decimal-digit bytes. It avoids
// a strconv import for this one internal use; tick counters are non-negative
// by construction (monotonic from zero), and a negative input is rendered
// with a leading '-' so it still hashes to a stable, distinct value.
func itoaBytes(n int) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return buf[i:]
}
