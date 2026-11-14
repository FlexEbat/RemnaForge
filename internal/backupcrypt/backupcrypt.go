// Package backupcrypt encrypts and decrypts backup files with
// `gpg --symmetric`. The passphrase is read from the terminal, and reaches
// gpg through a pipe on file descriptor 3: it is never an argument of a
// command and never written to disk.
package backupcrypt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Suffix is appended to the name of an encrypted file.
const Suffix = ".gpg"

// MinPassphrase is the shortest passphrase accepted.
const MinPassphrase = 8

// ErrNoGPG means gpg is not installed.
var ErrNoGPG = errors.New("gpg is not installed (install the package gnupg)")

// run executes gpg with the given arguments and passes the passphrase on file
// descriptor 3. Tests replace it.
var run = func(args []string, passphrase string) error {
	if _, err := exec.LookPath("gpg"); err != nil {
		return ErrNoGPG
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	cmd := execx.Command("gpg", args...)
	cmd.ExtraFiles = []*os.File{r} // becomes file descriptor 3
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		w.Close()
		return err
	}
	_, _ = io.WriteString(w, passphrase+"\n")
	w.Close()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("gpg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func baseArgs() []string {
	return []string{"--batch", "--yes", "--quiet", "--pinentry-mode", "loopback", "--passphrase-fd", "3"}
}

// Encrypt writes path+".gpg" (mode 0600) and removes the plaintext once the
// encrypted file is complete. On failure the plaintext stays and no partial
// encrypted file is left.
func Encrypt(path, passphrase string) (string, error) {
	if len(passphrase) < MinPassphrase {
		return "", fmt.Errorf("the passphrase must have at least %d characters", MinPassphrase)
	}
	out := path + Suffix
	args := append(baseArgs(), "--symmetric", "--cipher-algo", "AES256", "--output", out, path)
	if err := run(args, passphrase); err != nil {
		_ = os.Remove(out)
		return "", err
	}
	if err := os.Chmod(out, 0600); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return out, nil
}

// Decrypt writes the file without its ".gpg" suffix (mode 0600) and keeps the
// encrypted one.
func Decrypt(path, passphrase string) (string, error) {
	if !strings.HasSuffix(path, Suffix) {
		return "", fmt.Errorf("%s does not end in %s", path, Suffix)
	}
	out := strings.TrimSuffix(path, Suffix)
	args := append(baseArgs(), "--decrypt", "--output", out, path)
	if err := run(args, passphrase); err != nil {
		_ = os.Remove(out)
		return "", err
	}
	if err := os.Chmod(out, 0600); err != nil {
		return "", err
	}
	return out, nil
}

// EncryptFiles encrypts the named files of dir. If one fails, files already
// encrypted stay encrypted and the error names the file.
func EncryptFiles(dir, passphrase string, names ...string) error {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			continue
		}
		if _, err := Encrypt(filepath.Join(dir, n), passphrase); err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
	}
	return nil
}

// Prompt reads a passphrase twice from the terminal, without echo, until both
// entries match and are long enough. An empty first entry cancels.
func Prompt() (string, bool) {
	for {
		first := ui.ReadSecret(i18n.T("BACKUP_PASSPHRASE"))
		if first == "" {
			return "", false
		}
		if len(first) < MinPassphrase {
			fmt.Printf("%s"+i18n.T("BACKUP_PASSPHRASE_SHORT")+"%s\n", ui.ColorRed, MinPassphrase, ui.ColorReset)
			continue
		}
		if second := ui.ReadSecret(i18n.T("BACKUP_PASSPHRASE_AGAIN")); second != first {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("BACKUP_PASSPHRASE_MISMATCH"), ui.ColorReset)
			continue
		}
		ui.RegisterSecret(first)
		return first, true
	}
}
