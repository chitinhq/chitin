// Package hash implements canonical JSON and SHA-256 used for event hash chains.
//
// Canonical JSON: keys sorted lexicographically at every level; no whitespace; UTF-8.
// Must produce byte-identical output to the TypeScript implementation in
// libs/contracts/src/hash.ts for cross-language parity.
package hash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

func CanonicalJSON(value any) (string, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, value); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeCanonical(buf *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	case float64:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	case int, int64, int32:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	case []any:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonical(buf, v[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported type %T in canonical JSON", value)
	}
	return nil
}

func Sha256Hex(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func HashEvent(event map[string]any) (string, error) {
	rest := make(map[string]any, len(event))
	for k, v := range event {
		if k == "this_hash" {
			continue
		}
		rest[k] = v
	}
	c, err := CanonicalJSON(rest)
	if err != nil {
		return "", err
	}
	return Sha256Hex(c), nil
}
