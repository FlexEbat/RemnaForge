package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
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
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// replaceInFile replaces every occurrence of old with new in the file
// at path and keeps the file's existing permissions.
func replaceInFile(path, old, new string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return writeKeepMode(path, []byte(strings.ReplaceAll(string(data), old, new)))
}

// setComposeEnv sets KEY=value in an environment list entry of a compose
// file ("      - KEY=..."), preserving the indentation and list marker.
func setComposeEnv(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re := regexp.MustCompile(`(?m)^(\s*-\s*` + regexp.QuoteMeta(key) + `=).*$`)
	if !re.Match(data) {
		return fmt.Errorf("%s not found in %s", key, path)
	}
	out := re.ReplaceAllString(string(data), "${1}"+strings.ReplaceAll(value, "$", "$$"))
	return writeKeepMode(path, []byte(out))
}

func writeKeepMode(path string, data []byte) error {
	mode := os.FileMode(0600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}
