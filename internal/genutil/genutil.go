// Package genutil generates random usernames, passwords, and alnum
// secrets using crypto/rand.
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

// GenerateUser returns an 8-character random username made of letters
// only.
func GenerateUser() string {
	const length = 8
	letters := upperChars + lowerChars
	out := make([]byte, length)
	for i := range out {
		out[i] = randChar(letters)
	}
	return string(out)
}

// GeneratePassword returns a 24-character random password. Guarantees
// at least 1 upper, 1 lower, 1 digit, 3 special chars, fills the rest
// from the full charset, then shuffles.
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

	// Fisher-Yates shuffle using crypto/rand.
	for i := len(chars) - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		chars[i], chars[j.Int64()] = chars[j.Int64()], chars[i]
	}

	return string(chars)
}

var alnumFilter = func(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// GenerateAlnumSecret returns a random alphanumeric-only secret of the
// given length, used for APP_SECRET. Generates base64 output and
// filters it down to alnum characters, retrying with additional random
// bytes until it has enough, so it can't come up short the way naively
// filtering a single fixed-size chunk could.
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
