package maintenance

import (
	"os"
	"path/filepath"
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

const (
	v2Env     = "APP_PORT=3000\nJWT_AUTH_SECRET=abcdef0123456789\nSWAGGER_PATH=/docs\n"
	v2Compose = "services:\n  remnawave:\n    image: remnawave/backend:2.0.1\n"
	v2Nginx   = "upstream remnawave { server remnawave:3000; }\n"
)

// migrationHost builds a panel directory in 2.x state and puts a docker
// script in PATH that logs its arguments and fails when "failOn" is the
// compose subcommand. It returns the directory and the log of the calls.
func migrationHost(t *testing.T, failOn string) (dir, log string) {
	t.Helper()
	dir = t.TempDir()
	for name, content := range map[string]string{".env": v2Env, "docker-compose.yml": v2Compose, "nginx.conf": v2Nginx} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	log = filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n" +
		"if [ \"$1 $2\" = \"compose " + failOn + "\" ]; then echo boom >&2; exit 1; fi\n" +
		"echo 'select 1;'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	old := panelDir
	panelDir = dir
	t.Cleanup(func() { panelDir = old })
	return dir, log
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrationPutsTheFilesBackWhenThePullFails(t *testing.T) {
	dir, log := migrationHost(t, "pull")
	migrateV2ToV3(func(string) string { return "y" })
	for name, want := range map[string]string{".env": v2Env, "docker-compose.yml": v2Compose, "nginx.conf": v2Nginx} {
		if got := fileText(t, filepath.Join(dir, name)); got != want {
			t.Errorf("%s was not restored:\n%s", name, got)
		}
	}
	if calls := fileText(t, log); strings.Contains(calls, "compose up") {
		t.Errorf("the stack was recreated after a failed pull:\n%s", calls)
	}
}

func TestMigrationRestartsNginxWhenItsConfigChanged(t *testing.T) {
	dir, log := migrationHost(t, "none")
	migrateV2ToV3(func(string) string { return "y" })
	if got := fileText(t, filepath.Join(dir, "docker-compose.yml")); !strings.Contains(got, "remnawave/backend:3") {
		t.Errorf("the compose file was not migrated:\n%s", got)
	}
	if got := fileText(t, filepath.Join(dir, "nginx.conf")); !strings.HasPrefix(got, "gzip on;") {
		t.Errorf("nginx.conf has no gzip block:\n%s", got)
	}
	calls := fileText(t, log)
	up, restart := strings.Index(calls, "compose up"), strings.Index(calls, "compose restart remnawave-nginx")
	if up < 0 || restart < up {
		t.Errorf("nginx was not restarted after up:\n%s", calls)
	}
}

func TestMigrationKeepsTheNewFilesWhenUpFails(t *testing.T) {
	dir, _ := migrationHost(t, "up")
	migrateV2ToV3(func(string) string { return "y" })
	if got := fileText(t, filepath.Join(dir, "docker-compose.yml")); !strings.Contains(got, "remnawave/backend:3") {
		t.Errorf("the migrated compose file was rolled back after the images were pulled:\n%s", got)
	}
}

func TestEnsureGzipIgnoresACommentedLine(t *testing.T) {
	if _, changed := ensureGzip("# gzip on;\nupstream x {}\n"); !changed {
		t.Error("a commented-out gzip line counted as enabled")
	}
	if _, changed := ensureGzip("  gzip on;\n"); changed {
		t.Error("an indented gzip line was not recognised")
	}
}
