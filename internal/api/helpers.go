package api

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

// randHex generates n random bytes and returns them hex-encoded.
func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// replaceInFile replaces every occurrence of old with new in the file
// at path.
func replaceInFile(path, old, new string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	updated := strings.ReplaceAll(string(data), old, new)
	_ = os.WriteFile(path, []byte(updated), 0644)
}

// replaceLineInFile replaces every line starting with prefix with
// newLine.
func replaceLineInFile(path, prefix, newLine string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = newLine
		}
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
