package genutil

import (
	"strings"
	"testing"
	"unicode"
)

func TestGeneratePassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p := GeneratePassword()
		if len(p) != 24 {
			t.Fatalf("length = %d, want 24", len(p))
		}
		var upper, lower, digit, special int
		for _, r := range p {
			switch {
			case unicode.IsUpper(r):
				upper++
			case unicode.IsLower(r):
				lower++
			case unicode.IsDigit(r):
				digit++
			case strings.ContainsRune(specialChars, r):
				special++
			default:
				t.Fatalf("unexpected character %q in %q", r, p)
			}
		}
		if upper < 1 || lower < 1 || digit < 1 || special < 3 {
			t.Fatalf("password %q misses a class: upper=%d lower=%d digit=%d special=%d", p, upper, lower, digit, special)
		}
		seen[p] = true
	}
	if len(seen) != 200 {
		t.Errorf("only %d distinct passwords in 200 calls", len(seen))
	}
}

func TestGenerateUser(t *testing.T) {
	u := GenerateUser()
	if len(u) != 8 {
		t.Fatalf("length = %d, want 8", len(u))
	}
	for _, r := range u {
		if !unicode.IsLetter(r) {
			t.Errorf("non-letter %q in username %q", r, u)
		}
	}
}

func TestGenerateAlnumSecret(t *testing.T) {
	for _, n := range []int{1, 32, 64, 200} {
		s := GenerateAlnumSecret(n)
		if len(s) != n {
			t.Errorf("GenerateAlnumSecret(%d) has length %d", n, len(s))
		}
		for _, r := range s {
			if !alnumFilter(r) {
				t.Errorf("non-alphanumeric %q in secret", r)
			}
		}
	}
}

func TestHardenEnv(t *testing.T) {
	env := strings.Join([]string{
		"APP_PORT=3000",
		"DATABASE_URL=postgresql://postgres:postgres@remnawave-db:5432/postgres",
		"POSTGRES_PASSWORD=postgres",
		"WEBHOOK_SECRET_HEADER=change-me",
		"OTHER=keep",
		"",
	}, "\n")

	out := HardenEnv(env)
	if strings.Contains(out, "postgres:postgres@") || strings.Contains(out, "POSTGRES_PASSWORD=postgres\n") {
		t.Errorf("default database password survived:\n%s", out)
	}
	if strings.Contains(out, "change-me") {
		t.Errorf("default webhook secret survived:\n%s", out)
	}
	for _, keep := range []string{"APP_PORT=3000", "OTHER=keep"} {
		if !strings.Contains(out, keep) {
			t.Errorf("unrelated line %q lost", keep)
		}
	}

	// The URL and POSTGRES_PASSWORD must carry the same password.
	var urlPass, envPass string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "DATABASE_URL="):
			urlPass = strings.SplitN(strings.SplitN(line, "postgres:", 2)[1], "@", 2)[0]
		case strings.HasPrefix(line, "POSTGRES_PASSWORD="):
			envPass = strings.TrimPrefix(line, "POSTGRES_PASSWORD=")
		}
	}
	if urlPass == "" || urlPass != envPass {
		t.Errorf("DATABASE_URL password %q differs from POSTGRES_PASSWORD %q", urlPass, envPass)
	}

	if HardenEnv(env) == out {
		t.Error("two runs produced identical secrets")
	}
}
