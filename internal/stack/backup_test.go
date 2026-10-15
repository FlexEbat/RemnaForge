package stack

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeDocker puts a `docker` stub first on PATH. The stub runs body as
// a shell script, so a test can simulate pg_dump succeeding or failing
// without a Docker daemon.
func fakeDocker(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestBackupPanelToRemovesPartialBackupOnFailure(t *testing.T) {
	fakeDocker(t, `echo "partial dump"; echo "pg_dump: connection lost" >&2; exit 1`)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup")

	err := backupPanelTo(dir, dest)
	if err == nil {
		t.Fatal("expected an error when pg_dump fails")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("failed backup left %s behind (stat err: %v)", dest, statErr)
	}
}

func TestBackupPanelToWritesDumpAndEnv(t *testing.T) {
	fakeDocker(t, `echo "CREATE TABLE t();"`)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup")

	if err := backupPanelTo(dir, dest); err != nil {
		t.Fatalf("backupPanelTo: %v", err)
	}
	for _, name := range []string{".env", "db.sql.gz"} {
		fi, err := os.Stat(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Errorf("%s mode = %o, want 0600", name, fi.Mode().Perm())
		}
	}
}
