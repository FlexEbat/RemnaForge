package maintenance

import (
	"strings"
	"testing"
)

func TestMigrateEnv(t *testing.T) {
	in := "APP_PORT=3000\nJWT_AUTH_SECRET=abc\nJWT_API_TOKENS_SECRET=def\nSWAGGER_PATH=/docs\nIS_DOCS_ENABLED=true\nOTHER=1"
	out, changed := migrateEnv(in)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, "APP_SECRET=abc") || strings.Contains(out, "JWT_") || strings.Contains(out, "SWAGGER") || strings.Contains(out, "DOCS") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if again, changed := migrateEnv(out); changed || again != out {
		t.Fatal("migration must be idempotent")
	}
}

func TestMigrateEnvKeepsExistingAppSecret(t *testing.T) {
	out, _ := migrateEnv("APP_SECRET=new\nJWT_AUTH_SECRET=old")
	if !strings.Contains(out, "APP_SECRET=new") || strings.Contains(out, "old") {
		t.Fatalf("got %q", out)
	}
}

func TestMigrateCompose(t *testing.T) {
	out, changed := migrateCompose("    image: remnawave/backend:2\n    image: remnawave/node:latest")
	if !changed || !strings.Contains(out, "remnawave/backend:3") || !strings.Contains(out, "remnawave/node:latest") {
		t.Fatalf("got %q", out)
	}
	if _, changed := migrateCompose(out); changed {
		t.Fatal("must be idempotent")
	}
}

func TestEnsureGzip(t *testing.T) {
	out, changed := ensureGzip("upstream x {}\n")
	if !changed || !strings.HasPrefix(out, "gzip on;") {
		t.Fatalf("got %q", out)
	}
	if _, changed := ensureGzip(out); changed {
		t.Fatal("must be idempotent")
	}
}
