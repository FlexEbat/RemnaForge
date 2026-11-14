// Package stack holds helpers for the docker compose stacks this tool
// installs under /opt/remnawave and /opt/remnanode: safe teardown,
// volume checks and database backups.
package stack

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/backupcrypt"
	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Teardown stops a stack and deletes its containers, volumes and images
// (only those belonging to this compose project). The directory itself
// is removed only when the stack went down cleanly, so a failed
// teardown never costs the operator the .env that the surviving
// volumes depend on.
func Teardown(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		cmd := execx.Command("docker", "compose", "down", "-v", "--rmi", "all", "--remove-orphans")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker compose down in %s: %v: %s", dir, err, out)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// A removed stack starts from fresh values the next time.
	_ = cfgfile.Forget(dir, filepath.Base(dir)+".")
	return nil
}

// BackupPanel writes a gzip-compressed pg_dump of the panel database
// and a copy of .env into dir/backups/<timestamp>/, returning that
// directory. The panel's db container must be running.
func BackupPanel(dir string) (string, error) {
	dest := filepath.Join(dir, "backups", time.Now().UTC().Format("20060102-150405"))
	if err := backupPanelTo(dir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// backupPanelTo is BackupPanel with an explicit destination. A failed
// run removes dest again: a half-written db.sql.gz next to a valid .env
// looks like a usable backup and is not one.
func backupPanelTo(dir, dest string) (err error) {
	if err = os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dest)
		}
	}()

	if data, readErr := os.ReadFile(filepath.Join(dir, ".env")); readErr == nil {
		if err = cfgfile.Replace(filepath.Join(dest, ".env"), string(data), cfgfile.Opts{}); err != nil {
			return err
		}
	}

	out, err := os.OpenFile(filepath.Join(dest, "db.sql.gz"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	cmd := execx.CommandTimeout(execx.DockerLongTimeout, "docker", "exec", "remnawave-db", "sh", "-c",
		`pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	if _, err = io.Copy(gz, pipe); err != nil {
		_ = cmd.Wait()
		return err
	}
	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("pg_dump failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err = gz.Close(); err != nil {
		return err
	}
	return out.Sync()
}

// PromptBackup offers a database backup of the panel stack in dir,
// when one is installed and its database container is running. The
// backup goes straight to /root, outside dir, because the caller
// deletes dir next. It returns false when the operator asked for a
// backup and none was written; the caller must then stop before
// removing anything, since the volumes hold the only copy of the data.
func PromptBackup(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		return true
	}
	if execx.Command("docker", "inspect", "-f", "{{.State.Running}}", "remnawave-db").Run() != nil {
		return true
	}
	answer := ui.Reading(i18n.T("BACKUP_BEFORE_REMOVE"))
	if answer != "y" && answer != "Y" {
		return true
	}
	keep := filepath.Join("/root", filepath.Base(dir)+"-backup-"+time.Now().UTC().Format("20060102-150405"))
	if err := backupPanelTo(dir, keep); err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_FAILED"), err, ui.ColorReset)
		return false
	}
	if answer := ui.Reading(i18n.T("BACKUP_ENCRYPT")); answer == "y" || answer == "Y" {
		if err := encryptBackup(keep, backupcrypt.Prompt); err != nil {
			fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_ENCRYPT_FAILED"), err, ui.ColorReset)
			// A backup the operator asked to be encrypted must not stay behind
			// in plain text, and the caller must not go on without one.
			_ = os.RemoveAll(keep)
			return false
		}
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("BACKUP_ENCRYPTED"), ui.ColorReset)
	}
	fmt.Printf("%s"+i18n.T("BACKUP_DONE")+"%s\n", ui.ColorGreen, keep, ui.ColorReset)
	return true
}

// encryptBackup encrypts the files of a finished backup. prompt returns the
// passphrase, or false when the operator cancels.
func encryptBackup(dest string, prompt func() (string, bool)) error {
	pass, ok := prompt()
	if !ok {
		return fmt.Errorf("cancelled")
	}
	return backupcrypt.EncryptFiles(dest, pass, ".env", "db.sql.gz")
}

// ConfirmDelete asks the operator to type DELETE.
func ConfirmDelete() bool {
	if ui.Reading(i18n.T("CONFIRM_TYPE_DELETE")) != "DELETE" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONFIRM_ABORTED"), ui.ColorReset)
		return false
	}
	return true
}
