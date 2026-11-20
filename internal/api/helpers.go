package api

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
)

// BaseURL turns a bare host:port into an http:// URL and leaves a value
// that already carries a scheme untouched, with any trailing slash removed.
func BaseURL(host string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "http://" + host
}

// randHex generates n random bytes and returns them hex-encoded.
func randHex(n int) string {
	buf := make([]byte, n)
	// A failed read leaves zeros, which would make a predictable Reality
	// short ID or Hysteria password. Stopping is the only safe answer.
	if _, err := rand.Read(buf); err != nil {
		panic("api: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// replaceInFile replaces every occurrence of old with new in the file
// at path and keeps the file's existing permissions.
func replaceInFile(path, old, new string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return cfgfile.Replace(path, strings.ReplaceAll(string(data), old, new), cfgfile.Opts{})
}
