package backupcrypt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// encryptedBackup makes a backup directory with two encrypted files.
func encryptedBackup(t *testing.T, root, name, pass string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".env"), secretPlain)
	write(t, filepath.Join(dir, "db.sql.gz"), "dump-bytes")
	if err := EncryptFiles(dir, pass, ".env", "db.sql.gz"); err != nil {
		t.Fatal(err)
	}
	return dir
}

type scripted struct {
	answers []string
	secrets []string
	prompts []string
}

func (s *scripted) env(root string) MenuEnv {
	return MenuEnv{
		Ask: func(p string) string {
			s.prompts = append(s.prompts, p)
			a := s.answers[0]
			s.answers = s.answers[1:]
			return a
		},
		Secret: func(p string) string {
			a := s.secrets[0]
			s.secrets = s.secrets[1:]
			return a
		},
		Glob: func(string) ([]string, error) { return filepath.Glob(filepath.Join(root, "*-backup-*")) },
	}
}

func TestMenuDecryptsAChosenBackup(t *testing.T) {
	needGPG(t)
	ui.ForgetSecrets()
	root := t.TempDir()
	older := encryptedBackup(t, root, "remnawave-backup-20260101-000000", "first-passphrase")
	dir := encryptedBackup(t, root, "remnawave-backup-20260202-000000", "second-passphrase")
	_ = os.MkdirAll(filepath.Join(root, "remnawave-backup-plain"), 0700) // no .gpg files: not listed

	s := &scripted{answers: []string{"2"}, secrets: []string{"second-passphrase"}}
	Menu(s.env(root))

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(got) != secretPlain {
		t.Fatalf(".env: %q %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "db.sql.gz")); string(b) != "dump-bytes" {
		t.Errorf("db.sql.gz: %q", b)
	}
	for _, n := range []string{".env", "db.sql.gz"} {
		if fi, _ := os.Stat(filepath.Join(dir, n)); fi.Mode().Perm() != 0600 {
			t.Errorf("%s mode %v", n, fi.Mode().Perm())
		}
		if _, err := os.Stat(filepath.Join(dir, n+".gpg")); err != nil {
			t.Errorf("%s.gpg was removed", n)
		}
		if _, err := os.Stat(filepath.Join(older, n)); err == nil {
			t.Errorf("the other backup was decrypted too")
		}
	}
	if strings.Contains(ui.Redact("second-passphrase"), "second-passphrase") {
		t.Error("the passphrase is not masked in the log")
	}
}

func TestMenuNumbersTheBackupsAndHidesPlainOnes(t *testing.T) {
	needGPG(t)
	root := t.TempDir()
	encryptedBackup(t, root, "remnawave-backup-a", "some-passphrase")
	_ = os.MkdirAll(filepath.Join(root, "remnawave-backup-b"), 0700)
	got := FindBackups((&scripted{}).env(root))
	if len(got) != 1 || !strings.HasSuffix(got[0], "remnawave-backup-a") {
		t.Errorf("found %v", got)
	}
}

func TestWrongPassphraseLeavesNothingBehind(t *testing.T) {
	needGPG(t)
	root := t.TempDir()
	dir := encryptedBackup(t, root, "remnawave-backup-x", "right-passphrase")
	s := &scripted{answers: []string{"1"}, secrets: []string{"wrong-passphrase"}}
	Menu(s.env(root))
	for _, n := range []string{".env", "db.sql.gz"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s was written with a wrong passphrase", n)
		}
	}
}

// When the second file fails, the first one's output is removed too.
func TestPartialFailureRemovesWhatWasWritten(t *testing.T) {
	needGPG(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "alpha")
	write(t, filepath.Join(dir, "b"), "bravo")
	if _, err := Encrypt(filepath.Join(dir, "a"), "same-passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := Encrypt(filepath.Join(dir, "b"), "other-passphrase"); err != nil {
		t.Fatal(err)
	}
	files, _ := EncryptedFiles(dir)
	if _, err := DecryptAll(files, "same-passphrase"); err == nil {
		t.Fatal("expected a failure on the second file")
	}
	for _, n := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s survived a failed decryption", n)
		}
	}
}

func TestDecryptAllNeverOverwritesAPlaintextFile(t *testing.T) {
	needGPG(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), "alpha")
	if _, err := Encrypt(filepath.Join(dir, "a"), "long-passphrase"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "a"), "precious newer plaintext")
	files, _ := EncryptedFiles(dir)
	if _, err := DecryptAll(files, "long-passphrase"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a")); string(b) != "precious newer plaintext" {
		t.Errorf("the existing file was overwritten: %q", b)
	}
}

func TestMenuAcceptsAFilePathAndADirectoryPath(t *testing.T) {
	needGPG(t)
	root := t.TempDir()
	dir := encryptedBackup(t, root, "elsewhere", "long-passphrase")
	s := &scripted{answers: []string{filepath.Join(dir, ".env.gpg")}, secrets: []string{"long-passphrase"}}
	Menu(s.env(t.TempDir()))
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Error(".env was not decrypted")
	}
	if _, err := os.Stat(filepath.Join(dir, "db.sql.gz")); err == nil {
		t.Error("a single file path decrypted the whole directory")
	}
	s = &scripted{answers: []string{dir}, secrets: []string{"long-passphrase"}}
	_ = os.Remove(filepath.Join(dir, ".env"))
	Menu(s.env(t.TempDir()))
	if _, err := os.Stat(filepath.Join(dir, "db.sql.gz")); err != nil {
		t.Error("a directory path did not decrypt its files")
	}
}

func TestMenuCancelsAndRejects(t *testing.T) {
	old := run
	defer func() { run = old }()
	run = func([]string, string) error { t.Fatal("gpg ran"); return nil }
	root := t.TempDir()
	encryptedFake := filepath.Join(root, "remnawave-backup-q")
	_ = os.MkdirAll(encryptedFake, 0700)
	write(t, filepath.Join(encryptedFake, ".env.gpg"), "x")

	for name, s := range map[string]*scripted{
		"zero":             {answers: []string{"0"}},
		"empty":            {answers: []string{""}},
		"out of range":     {answers: []string{"7"}},
		"not a file":       {answers: []string{"/nonexistent/path"}},
		"plain file":       {answers: []string{filepath.Join(encryptedFake, "..", "remnawave-backup-q", "..")}},
		"empty passphrase": {answers: []string{"1"}, secrets: []string{""}},
	} {
		Menu(s.env(root))
		_ = name
	}
	if _, err := EncryptedFiles(filepath.Join(root, "missing")); err == nil {
		t.Error("a missing path was accepted")
	}
	plain := filepath.Join(root, "notes.txt")
	write(t, plain, "x")
	if _, err := EncryptedFiles(plain); err == nil {
		t.Error("a file without .gpg was accepted")
	}
}

func TestMenuPassesThePassphraseOnlyThroughTheSecretReader(t *testing.T) {
	var gotPass string
	var gotArgs []string
	old := run
	defer func() { run = old }()
	run = func(args []string, pass string) error {
		gotArgs, gotPass = args, pass
		for i, a := range args {
			if a == "--output" {
				_ = os.WriteFile(args[i+1], []byte("plain"), 0644)
			}
		}
		return nil
	}
	root := t.TempDir()
	dir := filepath.Join(root, "remnawave-backup-z")
	_ = os.MkdirAll(dir, 0700)
	write(t, filepath.Join(dir, ".env.gpg"), "cipher")
	s := &scripted{answers: []string{"1"}, secrets: []string{"typed-passphrase"}}
	Menu(s.env(root))
	if gotPass != "typed-passphrase" || strings.Contains(strings.Join(gotArgs, " "), "typed-passphrase") {
		t.Errorf("pass %q args %v", gotPass, gotArgs)
	}
}
