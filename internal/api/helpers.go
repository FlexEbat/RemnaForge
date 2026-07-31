package api

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

// randHex is the Go equivalent of: openssl rand -hex 8
// (src/api/remnawave_api.sh:283, inside create_config_profile()).
func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// replaceInFile is the Go equivalent of:
//
//	sed -i "s|old|new|g" path
//
// used by get_public_key() (src/api/remnawave_api.sh:137).
func replaceInFile(path, old, new string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	updated := strings.ReplaceAll(string(data), old, new)
	_ = os.WriteFile(path, []byte(updated), 0644)
}

// replaceLineInFile is the Go equivalent of:
//
//	sed -i "s|REMNAWAVE_API_TOKEN=.*|REMNAWAVE_API_TOKEN=$api_token|" path
//
// used by create_api_token() (src/api/remnawave_api.sh:484): any line
// starting with `prefix` gets fully replaced by `newLine`.
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
