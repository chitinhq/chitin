package gov

import (
	"crypto/rand"
	"fmt"
	"time"
)

// crockfordAlphabet is Crockford base32: 0-9, A-Z minus I, L, O, U.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newULID returns a ULID-shaped 26-char Crockford-base32 string:
//
//	48-bit ms timestamp (big-endian) + 80-bit cryptographic random.
//
// The first 10 chars encode the timestamp, so IDs sort lexicographically
// by creation time — useful for `envelope list` and audit-log queries
// that want creation order without parsing JSON.
//
// Returns an error only on rand.Read failure.
func newULID() (string, error) {
	now := uint64(time.Now().UnixMilli())
	var bytes [16]byte
	bytes[0] = byte(now >> 40)
	bytes[1] = byte(now >> 32)
	bytes[2] = byte(now >> 24)
	bytes[3] = byte(now >> 16)
	bytes[4] = byte(now >> 8)
	bytes[5] = byte(now)
	if _, err := rand.Read(bytes[6:]); err != nil {
		return "", fmt.Errorf("ulid random: %w", err)
	}

	// Crockford base32 encode 16 bytes (128 bits) into 26 chars.
	// First char gets only 3 bits (top of timestamp); the rest take 5 each.
	var out [26]byte
	out[0] = crockfordAlphabet[(bytes[0]&224)>>5]
	out[1] = crockfordAlphabet[bytes[0]&31]
	out[2] = crockfordAlphabet[(bytes[1]&248)>>3]
	out[3] = crockfordAlphabet[((bytes[1]&7)<<2)|((bytes[2]&192)>>6)]
	out[4] = crockfordAlphabet[(bytes[2]&62)>>1]
	out[5] = crockfordAlphabet[((bytes[2]&1)<<4)|((bytes[3]&240)>>4)]
	out[6] = crockfordAlphabet[((bytes[3]&15)<<1)|((bytes[4]&128)>>7)]
	out[7] = crockfordAlphabet[(bytes[4]&124)>>2]
	out[8] = crockfordAlphabet[((bytes[4]&3)<<3)|((bytes[5]&224)>>5)]
	out[9] = crockfordAlphabet[bytes[5]&31]
	out[10] = crockfordAlphabet[(bytes[6]&248)>>3]
	out[11] = crockfordAlphabet[((bytes[6]&7)<<2)|((bytes[7]&192)>>6)]
	out[12] = crockfordAlphabet[(bytes[7]&62)>>1]
	out[13] = crockfordAlphabet[((bytes[7]&1)<<4)|((bytes[8]&240)>>4)]
	out[14] = crockfordAlphabet[((bytes[8]&15)<<1)|((bytes[9]&128)>>7)]
	out[15] = crockfordAlphabet[(bytes[9]&124)>>2]
	out[16] = crockfordAlphabet[((bytes[9]&3)<<3)|((bytes[10]&224)>>5)]
	out[17] = crockfordAlphabet[bytes[10]&31]
	out[18] = crockfordAlphabet[(bytes[11]&248)>>3]
	out[19] = crockfordAlphabet[((bytes[11]&7)<<2)|((bytes[12]&192)>>6)]
	out[20] = crockfordAlphabet[(bytes[12]&62)>>1]
	out[21] = crockfordAlphabet[((bytes[12]&1)<<4)|((bytes[13]&240)>>4)]
	out[22] = crockfordAlphabet[((bytes[13]&15)<<1)|((bytes[14]&128)>>7)]
	out[23] = crockfordAlphabet[(bytes[14]&124)>>2]
	out[24] = crockfordAlphabet[((bytes[14]&3)<<3)|((bytes[15]&224)>>5)]
	out[25] = crockfordAlphabet[bytes[15]&31]
	return string(out[:]), nil
}
