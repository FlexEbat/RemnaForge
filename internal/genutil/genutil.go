// Package genutil is a port of generate_user() and generate_password()
// (install_remnawave.sh:337-360). Both originally pull from /dev/urandom
// via tr/fold/shuf; here that's crypto/rand, which is the direct Go
// equivalent of reading from /dev/urandom (and what /dev/urandom itself is
// backed by on Linux).
package genutil

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"strings"
)

const (
	upperChars   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lowerChars   = "abcdefghijklmnopqrstuvwxyz"
	digitChars   = "0123456789"
	specialChars = "!@#%^&*()_+"
	allChars     = upperChars + lowerChars + digitChars + specialChars
)

// randChar picks one random character from charset using crypto/rand.
func randChar(charset string) byte {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
	return charset[n.Int64()]
}

// Original bash (install_remnawave.sh:337-340):
//
//	generate_user() {
//	    local length=8
//	    tr -dc 'a-zA-Z' < /dev/urandom | fold -w $length | head -n 1
//	}
func GenerateUser() string {
	const length = 8
	letters := upperChars + lowerChars
	out := make([]byte, length)
	for i := range out {
		out[i] = randChar(letters)
	}
	return string(out)
}

// Original bash (install_remnawave.sh:342-360): generate_password().
// Guarantees at least 1 upper, 1 lower, 1 digit, 3 special chars, fills
// the rest from the full charset, then shuffles - same recipe as the
// bash version's `password+=...; ...; fold -w1 | shuf | tr -d '\n'`.
func GeneratePassword() string {
	const length = 24

	chars := make([]byte, 0, length)
	chars = append(chars, randChar(upperChars))
	chars = append(chars, randChar(lowerChars))
	chars = append(chars, randChar(digitChars))
	for i := 0; i < 3; i++ {
		chars = append(chars, randChar(specialChars))
	}
	for len(chars) < length {
		chars = append(chars, randChar(allChars))
	}

	// Fisher-Yates shuffle using crypto/rand (equivalent to `shuf`).
	for i := len(chars) - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		chars[i], chars[j.Int64()] = chars[j.Int64()], chars[i]
	}

	return string(chars)
}

var alnumFilter = func(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// GenerateAlnumSecret is the Go equivalent of:
//
//	openssl rand -base64 48 | tr -dc 'a-zA-Z0-9' | head -c 64
//
// (src/nginx/install_panel_node.sh:45-46, used for JWT_AUTH_SECRET and
// JWT_API_TOKENS_SECRET). Filtering base64 output to alnum-only can come up
// short of the requested length in bash (no retry) - here we keep
// generating additional random bytes until we actually have enough
// characters, which is a small, deliberate improvement.
func GenerateAlnumSecret(length int) string {
	var sb strings.Builder
	for sb.Len() < length {
		buf := make([]byte, 48)
		_, _ = rand.Read(buf)
		encoded := base64.StdEncoding.EncodeToString(buf)
		for _, r := range encoded {
			if alnumFilter(r) {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()[:length]
}
