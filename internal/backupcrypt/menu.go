package backupcrypt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// SearchGlobs are the places panel backups are kept: a backup is a directory
// /root/<stack>-backup-<time>.
var SearchGlobs = []string{"/root/*-backup-*"}

// MenuEnv is everything the menu reaches outside itself.
type MenuEnv struct {
	Ask    func(prompt string) string
	Secret func(prompt string) string // reads without echo
	Glob   func(pattern string) ([]string, error)
}

// DefaultMenuEnv is the real terminal and file system.
func DefaultMenuEnv() MenuEnv {
	return MenuEnv{Ask: ui.Reading, Secret: ui.ReadSecret, Glob: filepath.Glob}
}

// EncryptedFiles lists the .gpg files of a directory, or the file itself when
// path is one.
func EncryptedFiles(path string) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		if !strings.HasSuffix(path, Suffix) {
			return nil, fmt.Errorf("%s does not end in %s", path, Suffix)
		}
		return []string{path}, nil
	}
	files, err := filepath.Glob(filepath.Join(path, "*"+Suffix))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no %s files in %s", Suffix, path)
	}
	sort.Strings(files)
	return files, nil
}

// FindBackups returns the directories that hold encrypted files.
func FindBackups(env MenuEnv) []string {
	var out []string
	for _, g := range SearchGlobs {
		dirs, _ := env.Glob(g)
		for _, d := range dirs {
			if files, err := EncryptedFiles(d); err == nil && len(files) > 0 {
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DecryptAll decrypts every file. It refuses to overwrite a plaintext file
// that already exists, and when one file fails it removes the files this call
// wrote, so a wrong passphrase leaves nothing behind.
func DecryptAll(files []string, passphrase string) ([]string, error) {
	for _, f := range files {
		out := strings.TrimSuffix(f, Suffix)
		if _, err := os.Stat(out); err == nil {
			return nil, fmt.Errorf("%s already exists, move it away first", out)
		}
	}
	var done []string
	for _, f := range files {
		out, err := Decrypt(f, passphrase)
		if err != nil {
			for _, d := range done {
				_ = os.Remove(d)
			}
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		done = append(done, out)
	}
	return done, nil
}

func say(color, msg string) { fmt.Printf("%s%s%s\n", color, msg, ui.ColorReset) }

// Menu lets the operator pick a backup, asks for the passphrase and decrypts
// it. The decrypted files stay next to the encrypted ones, mode 0600.
func Menu(env MenuEnv) {
	say(ui.ColorGreen, i18n.T("BACKUP_DECRYPT_TITLE"))
	found := FindBackups(env)
	for i, d := range found {
		say(ui.ColorYellow, fmt.Sprintf("%d. %s", i+1, d))
	}
	if len(found) == 0 {
		say(ui.ColorYellow, i18n.T("BACKUP_DECRYPT_NONE"))
	}
	say(ui.ColorYellow, "0. "+i18n.T("EXIT"))
	choice := strings.TrimSpace(env.Ask(i18n.T("BACKUP_DECRYPT_PROMPT")))
	if choice == "" || choice == "0" {
		return
	}
	path := choice
	if n, err := strconv.Atoi(choice); err == nil {
		if n < 1 || n > len(found) {
			say(ui.ColorRed, i18n.T("INVALID_CHOICE"))
			return
		}
		path = found[n-1]
	}
	files, err := EncryptedFiles(path)
	if err != nil {
		say(ui.ColorRed, fmt.Sprintf(i18n.T("BACKUP_DECRYPT_FAILED"), err))
		return
	}
	pass := env.Secret(i18n.T("BACKUP_DECRYPT_PASSPHRASE"))
	if pass == "" {
		return
	}
	ui.RegisterSecret(pass)
	out, err := DecryptAll(files, pass)
	if err != nil {
		say(ui.ColorRed, fmt.Sprintf(i18n.T("BACKUP_DECRYPT_FAILED"), err))
		return
	}
	for _, o := range out {
		say(ui.ColorGreen, fmt.Sprintf(i18n.T("BACKUP_DECRYPT_DONE"), o))
	}
	say(ui.ColorYellow, i18n.T("BACKUP_DECRYPT_WARNING"))
}
