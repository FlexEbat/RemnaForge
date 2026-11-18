package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

func panelDirWithCompose(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  remnawave-db:\n    image: postgres\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubDocker(t *testing.T, running, volume bool) {
	t.Helper()
	oldRun, oldVol := dbRunning, dbVolumeExists
	dbRunning = func() bool { return running }
	dbVolumeExists = func() bool { return volume }
	t.Cleanup(func() { dbRunning, dbVolumeExists = oldRun, oldVol })
}

// A stopped database cannot be dumped, and the caller deletes its volume
// next. The prompt used to return true without a word.
func TestPromptBackupStopsWhenTheDatabaseIsStoppedAndTheOperatorDoesNotConfirm(t *testing.T) {
	stubDocker(t, false, true)
	dir := panelDirWithCompose(t)
	for _, answer := range []string{"", "y", "yes", "data", "DELETE"} {
		if promptBackup(dir, func(string) string { return answer }) {
			t.Errorf("answer %q let the caller delete a database that has no backup", answer)
		}
	}
}

func TestPromptBackupGoesOnWhenTheOperatorTypesDATA(t *testing.T) {
	stubDocker(t, false, true)
	if !promptBackup(panelDirWithCompose(t), func(string) string { return "DATA" }) {
		t.Error("an explicit confirmation was refused")
	}
}

func TestPromptBackupAsksNothingWhenThereIsNoDatabaseVolume(t *testing.T) {
	stubDocker(t, false, false)
	asked := false
	if !promptBackup(panelDirWithCompose(t), func(string) string { asked = true; return "" }) || asked {
		t.Error("the prompt asked although no data exists")
	}
}

func TestPromptBackupAsksNothingForAStackWithoutADatabase(t *testing.T) {
	stubDocker(t, false, true)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  remnanode:\n    image: node\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	asked := false
	if !promptBackup(dir, func(string) string { asked = true; return "" }) || asked {
		t.Error("a node stack was asked about a database")
	}
}

func TestPromptBackupWithoutAnInstallationPasses(t *testing.T) {
	stubDocker(t, false, true)
	if !promptBackup(t.TempDir(), func(string) string { return "" }) {
		t.Error("an empty directory blocked the caller")
	}
}

// loggingDocker puts a docker script first in PATH and returns the file that
// collects the arguments of its calls.
func loggingDocker(t *testing.T, exit int) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\necho 'volume is in use' >&2\nexit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return log
}

func TestResolveExistingDBVolumePassesWhenThereIsNoVolume(t *testing.T) {
	stubDocker(t, false, false)
	if err := ResolveExistingDBVolume(func(string) string { t.Fatal("asked"); return "" }); err != nil {
		t.Error(err)
	}
}

func TestResolveExistingDBVolumeStopsWithoutTheConfirmation(t *testing.T) {
	stubDocker(t, false, true)
	log := loggingDocker(t, 0)
	if err := ResolveExistingDBVolume(func(string) string { return "yes" }); err == nil {
		t.Error("the install went on over an existing volume")
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("docker was called without the confirmation")
	}
}

func TestResolveExistingDBVolumeRemovesTheVolumeOnDATA(t *testing.T) {
	stubDocker(t, false, true)
	log := loggingDocker(t, 0)
	if err := ResolveExistingDBVolume(func(string) string { return "DATA" }); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(log)
	if strings.TrimSpace(string(got)) != "volume rm remnawave-db-data" {
		t.Errorf("docker was called with %q", got)
	}
}

func TestResolveExistingDBVolumeKeepsTheErrorWhenDockerRefuses(t *testing.T) {
	stubDocker(t, false, true)
	loggingDocker(t, 1)
	if err := ResolveExistingDBVolume(func(string) string { return "DATA" }); err == nil {
		t.Error("a volume that docker would not remove counted as removed")
	}
}
