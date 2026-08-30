package selfsteal

import (
	"crypto/rand"
	"encoding/hex"
)

// randHex generates n random bytes and returns them hex-encoded, used
// to build randomized ids/classes when obfuscating a selfsteal
// template.
func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
