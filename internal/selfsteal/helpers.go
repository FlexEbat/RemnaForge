package selfsteal

import (
	"crypto/rand"
	"encoding/hex"
)

// randHex is the Go equivalent of: openssl rand -hex N
// (used throughout randomhtml(), src/modules/selfsteal_templates.sh:87-93).
func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
