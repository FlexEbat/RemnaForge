package stack

import (
	"os"
	"os/exec"
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

func TestEncryptBackupEncryptsEnvAndDumpAndRemovesPlaintext(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	home, _ := os.MkdirTemp("", "g")
	defer os.RemoveAll(home)
	t.Setenv("GNUPGHOME", home)
	dest := t.TempDir()
	_ = os.WriteFile(filepath.Join(dest, ".env"), []byte("POSTGRES_PASSWORD=db-pass-123456\n"), 0600)
	_ = os.WriteFile(filepath.Join(dest, "db.sql.gz"), []byte("dump"), 0600)

	if err := encryptBackup(dest, func() (string, bool) { return "a-long-passphrase", true }); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env.gpg", "db.sql.gz.gpg"} {
		if fi, err := os.Stat(filepath.Join(dest, name)); err != nil || fi.Mode().Perm() != 0600 {
			t.Errorf("%s: %v %v", name, fi, err)
		}
	}
	for _, name := range []string{".env", "db.sql.gz"} {
		if _, err := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Errorf("plaintext %s survived", name)
		}
	}
}

func TestEncryptBackupCancelledEncryptsNothing(t *testing.T) {
	dest := t.TempDir()
	_ = os.WriteFile(filepath.Join(dest, ".env"), []byte("A=1\n"), 0600)
	if err := encryptBackup(dest, func() (string, bool) { return "", false }); err == nil {
		t.Error("a cancelled passphrase prompt succeeded")
	}
	if _, err := os.Stat(filepath.Join(dest, ".env")); err != nil {
		t.Error("the plaintext was touched")
	}
}
