package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactKeyPatterns(t *testing.T) {
	ForgetSecrets()
	cases := map[string]string{
		"POSTGRES_PASSWORD=hunter2hunter2":                      "POSTGRES_PASSWORD=***",
		"JWT_AUTH_SECRET=abcDEF123456":                          "JWT_AUTH_SECRET=***",
		"WEBHOOK_SECRET_HEADER=vsmu67Kmg6R8":                    "WEBHOOK_SECRET_HEADER=***",
		"REMNAWAVE_API_TOKEN=eyJhbGciOi.abc.def":                "REMNAWAVE_API_TOKEN=***",
		"      - REMNAWAVE_API_TOKEN=eyJ.abc.def":               "      - REMNAWAVE_API_TOKEN=***",
		"dns_cloudflare_api_token = cf-token-123456":            "dns_cloudflare_api_token = ***",
		"X-Auth-Key: 0123456789abcdef":                          "X-Auth-Key: 0123456789abcdef", // not a secret-looking name
		"api_key: 'my key with spaces'":                         "api_key: '***'",
		`{"password":"p@ss w0rd","user":"bob"}`:                 `{"password":"***","user":"bob"}`,
		`{"accessToken": "abc.def.ghi"}`:                        `{"accessToken": "***"}`,
		"API-KEY=zzzz":                                          "API-KEY=***",
		"METRICS_PASS=secretpass1":                              "METRICS_PASS=secretpass1", // PASS alone is not a pattern
		"nothing to hide here":                                  "nothing to hide here",
		"DATABASE_URL=postgresql://postgres:s3cr3tpw@db:5432/x": "DATABASE_URL=postgresql://postgres:***@db:5432/x",
		"Authorization: Bearer eyJhbGciOi.payload.sig":          "Authorization: Bearer ***",
		"authorization: basic dXNlcjpwYXNz":                     "authorization: basic ***",
		"mysql://root:pw123456@host/db and more":                "mysql://root:***@host/db and more",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestRedactRegisteredValuesAnywhere(t *testing.T) {
	ForgetSecrets()
	RegisterSecret("generated-superadmin-pass")
	RegisterSecret("short") // too short to mask: it would shred the log
	RegisterSecret("  padded-secret-value  ")
	line := "Login: admin / generated-superadmin-pass, and padded-secret-value, say short"
	got := Redact(line)
	if strings.Contains(got, "generated-superadmin-pass") || strings.Contains(got, "padded-secret-value") {
		t.Errorf("a registered secret survived: %q", got)
	}
	if !strings.Contains(got, "say short") {
		t.Errorf("a short value was masked: %q", got)
	}
}

func TestRedactLongestSecretFirst(t *testing.T) {
	ForgetSecrets()
	RegisterSecret("abcdef")
	RegisterSecret("abcdefghijkl")
	if got := Redact("x abcdefghijkl y"); got != "x *** y" {
		t.Errorf("got %q", got)
	}
}

func TestRegisterSecretsFromText(t *testing.T) {
	ForgetSecrets()
	RegisterSecretsFromText("APP_PORT=3000\nJWT_AUTH_SECRET=\"quoted-secret-1\"\nPOSTGRES_PASSWORD=plain-password-2\nHOST=example.com\n")
	got := Redact("the values quoted-secret-1 and plain-password-2 and example.com")
	if strings.Contains(got, "quoted-secret-1") || strings.Contains(got, "plain-password-2") || !strings.Contains(got, "example.com") {
		t.Errorf("got %q", got)
	}
}

func TestReadingSecretRegistersWhatWasTyped(t *testing.T) {
	ForgetSecrets()
	RegisterSecret("typed-by-operator")
	if strings.Contains(Redact("token typed-by-operator"), "typed-by-operator") {
		t.Error("not masked")
	}
}

// The log file never holds a secret, even one split across two writes of
// the pipe, and still holds everything else.
func TestLogWriterMasksSecretsAcrossWrites(t *testing.T) {
	ForgetSecrets()
	RegisterSecret("split-secret-value")
	path := filepath.Join(t.TempDir(), "log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := &logWriter{f: f}
	for _, chunk := range []string{"\x1b[32mpassword is split-sec", "ret-value and POSTGRES_PASS", "WORD=abc123456\x1b[0m\nnext line\nlast without newline"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	w.flush()
	f.Close()
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.Contains(got, "split-secret-value") || strings.Contains(got, "abc123456") || strings.Contains(got, "\x1b") {
		t.Errorf("log content:\n%q", got)
	}
	for _, want := range []string{"password is ***", "POSTGRES_PASSWORD=***", "next line", "last without newline"} {
		if !strings.Contains(got, want) {
			t.Errorf("log misses %q:\n%s", want, got)
		}
	}
}

// The audit of slice 9 found no secret in the argument list of any command.
// This keeps it that way: the flags that put a secret on a command line
// do not appear in the code.
func TestNoSecretFlagsOnCommandLines(t *testing.T) {
	banned := []string{`"--passphrase"`, `"--password"`, `"--token"`, `"--api-key"`, `"--secret"`, `PGPASSWORD=`, `MYSQL_PWD=`, `"-p" + `}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, _ := os.ReadFile(path)
		for i, line := range strings.Split(string(src), "\n") {
			for _, b := range banned {
				if strings.Contains(line, b) {
					t.Errorf("%s:%d puts a secret on a command line (%s): %s", path, i+1, b, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
