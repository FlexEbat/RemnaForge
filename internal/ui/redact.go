package ui

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// mask replaces a secret in the log and in reports.
const mask = "***"

// minSecretLen keeps short strings ("1", "yes") out of the registry: masking
// them would shred the log and hide nothing.
const minSecretLen = 6

var (
	// keyValueRE finds KEY=value, KEY: value and "key": "value" where the key
	// names a secret. The value is a quoted string or a run without spaces.
	keyValueRE = regexp.MustCompile(`(?i)([A-Za-z0-9_.\-]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API[_\-]?KEY)[A-Za-z0-9_.\-]*)(["']?\s*[=:]\s*)("[^"\n]*"|'[^'\n]*'|[^\s"',;&]+)`)
	// authHeaderRE finds "Authorization: Bearer xxx" and the Basic form.
	authHeaderRE = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic|Token)\s+)\S+`)
	// urlCredRE finds user:password@ in a URL.
	urlCredRE = regexp.MustCompile(`(://[^/\s:@]+:)([^@\s/]+)(@)`)

	secretMu sync.Mutex
	secrets  = map[string]bool{}
)

// RegisterSecret makes Redact mask this exact value wherever it appears,
// whatever key it comes with. Install flows register what they generate or
// the operator types: tokens, passwords, API keys.
func RegisterSecret(value string) {
	value = strings.TrimSpace(value)
	if len(value) < minSecretLen {
		return
	}
	secretMu.Lock()
	secrets[value] = true
	secretMu.Unlock()
}

// RegisterSecretsFromText registers the value of every secret-looking
// KEY=value pair in text, such as the content of a .env file.
func RegisterSecretsFromText(text string) {
	for _, m := range keyValueRE.FindAllStringSubmatch(text, -1) {
		RegisterSecret(strings.Trim(m[3], `"'`))
	}
}

// ForgetSecrets clears the registry. Tests use it.
func ForgetSecrets() {
	secretMu.Lock()
	secrets = map[string]bool{}
	secretMu.Unlock()
}

// Redact masks secrets in s: registered values anywhere, the value of a key
// whose name contains PASSWORD, SECRET, TOKEN or API_KEY, bearer
// credentials, and the password of a URL.
func Redact(s string) string {
	secretMu.Lock()
	vals := make([]string, 0, len(secrets))
	for v := range secrets {
		vals = append(vals, v)
	}
	secretMu.Unlock()
	// Longest first, so a secret that contains another is masked whole.
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, mask)
	}
	s = keyValueRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := keyValueRE.FindStringSubmatch(m)
		val := sub[3]
		quote := ""
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			quote = string(val[0])
		}
		return sub[1] + sub[2] + quote + mask + quote
	})
	s = authHeaderRE.ReplaceAllString(s, "${1}"+mask)
	s = urlCredRE.ReplaceAllString(s, "${1}"+mask+"${3}")
	return s
}
