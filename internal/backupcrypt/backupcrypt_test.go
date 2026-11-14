package backupcrypt

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const secretPlain = "POSTGRES_PASSWORD=very-secret-db-password\nJWT_AUTH_SECRET=very-secret-jwt\n"

func needGPG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	needGPG(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, ".env")
	write(t, plain, secretPlain)

	enc, err := Encrypt(plain, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if enc != plain+".gpg" {
		t.Errorf("encrypted path %q", enc)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Error("the plaintext was left behind")
	}
	fi, _ := os.Stat(enc)
	if fi.Mode().Perm() != 0600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	data, _ := os.ReadFile(enc)
	if bytes.Contains(data, []byte("very-secret")) || bytes.Contains(data, []byte("correct horse")) {
		t.Error("the encrypted file contains the plaintext or the passphrase")
	}

	out, err := Decrypt(enc, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != secretPlain || out != plain {
		t.Errorf("round trip: %q at %s", got, out)
	}
	if _, err := os.Stat(enc); err != nil {
		t.Error("decrypting removed the encrypted file")
	}
}

func TestDecryptWithTheWrongPassphraseFailsAndLeavesNoOutput(t *testing.T) {
	needGPG(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, "db.sql.gz")
	write(t, plain, "dump")
	enc, err := Encrypt(plain, "right-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(enc, "wrong-passphrase"); err == nil {
		t.Fatal("decrypted with the wrong passphrase")
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Error("a failed decrypt left output behind")
	}
}

// The passphrase reaches gpg on descriptor 3 and never as an argument.
func TestPassphraseIsNeverAnArgument(t *testing.T) {
	var calls [][]string
	var passes []string
	old := run
	defer func() { run = old }()
	run = func(args []string, pass string) error {
		calls = append(calls, args)
		passes = append(passes, pass)
		for i, a := range args {
			if a == "--output" {
				_ = os.WriteFile(args[i+1], []byte("ciphertext"), 0644)
			}
		}
		return nil
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	write(t, p, "x")
	if _, err := Encrypt(p, "my-long-passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(p+".gpg", "my-long-passphrase"); err != nil {
		t.Fatal(err)
	}
	for i, args := range calls {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "my-long-passphrase") {
			t.Errorf("the passphrase is in the arguments: %s", joined)
		}
		for _, want := range []string{"--passphrase-fd 3", "--pinentry-mode loopback", "--batch"} {
			if !strings.Contains(joined, want) {
				t.Errorf("call %d misses %q: %s", i, want, joined)
			}
		}
		if passes[i] != "my-long-passphrase" {
			t.Errorf("call %d got passphrase %q", i, passes[i])
		}
	}
	if !strings.Contains(strings.Join(calls[0], " "), "--symmetric --cipher-algo AES256") {
		t.Errorf("encrypt arguments: %v", calls[0])
	}
	if !strings.Contains(strings.Join(calls[1], " "), "--decrypt") {
		t.Errorf("decrypt arguments: %v", calls[1])
	}
}

func TestEncryptFailureKeepsThePlaintextAndLeavesNoPartialFile(t *testing.T) {
	old := run
	defer func() { run = old }()
	run = func(args []string, _ string) error {
		for i, a := range args {
			if a == "--output" {
				_ = os.WriteFile(args[i+1], []byte("half"), 0600)
			}
		}
		return errors.New("gpg: out of disk")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "db.sql.gz")
	write(t, p, "dump")
	if _, err := Encrypt(p, "long-enough-pass"); err == nil {
		t.Fatal("expected an error")
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "dump" {
		t.Errorf("the plaintext was damaged or removed: %q %v", b, err)
	}
	if _, err := os.Stat(p + ".gpg"); !os.IsNotExist(err) {
		t.Error("a partial encrypted file was left")
	}
}

func TestShortPassphraseIsRefusedBeforeGPGRuns(t *testing.T) {
	old := run
	defer func() { run = old }()
	run = func([]string, string) error { t.Fatal("gpg ran"); return nil }
	if _, err := Encrypt("/nonexistent", "short"); err == nil {
		t.Error("a short passphrase was accepted")
	}
}

func TestDecryptNeedsTheGPGSuffix(t *testing.T) {
	if _, err := Decrypt("/tmp/file", "long-enough-pass"); err == nil {
		t.Error("a file without .gpg was accepted")
	}
}

func TestMissingGPGIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	write(t, p, "x")
	_, err := Encrypt(p, "long-enough-pass")
	if !errors.Is(err, ErrNoGPG) {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Error("the plaintext changed")
	}
}

func TestEncryptFilesSkipsMissingAndNamesTheFailure(t *testing.T) {
	needGPG(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".env"), secretPlain)
	if err := EncryptFiles(dir, "long-enough-pass", ".env", "db.sql.gz"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.gpg")); err != nil {
		t.Error(".env.gpg missing")
	}
	old := run
	defer func() { run = old }()
	run = func([]string, string) error { return errors.New("boom") }
	write(t, filepath.Join(dir, "other"), "x")
	err := EncryptFiles(dir, "long-enough-pass", "other")
	if err == nil || !strings.Contains(err.Error(), "other") {
		t.Errorf("err = %v", err)
	}
}
