// Package stack holds helpers for the docker compose stacks this tool
// installs under /opt/remnawave and /opt/remnanode: safe teardown,
// volume checks and database backups.
package stack

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
		cmd := exec.Command("docker", "compose", "down", "-v", "--rmi", "all", "--remove-orphans")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker compose down in %s: %v: %s", dir, err, out)
		}
	}
	return os.RemoveAll(dir)
}

// VolumeExists reports whether a docker volume whose name contains the
// given text exists (compose prefixes volume names with the project).
func VolumeExists(name string) bool {
	out, err := exec.Command("docker", "volume", "ls", "-q", "--filter", "name="+name).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// BackupPanel writes a gzip-compressed pg_dump of the panel database
// and a copy of .env into dir/backups/<timestamp>/, returning that
// directory. The panel's db container must be running.
func BackupPanel(dir string) (string, error) {
	dest := filepath.Join(dir, "backups", time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dest, 0700); err != nil {
		return "", err
	}

	if data, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
		if err := os.WriteFile(filepath.Join(dest, ".env"), data, 0600); err != nil {
			return "", err
		}
	}

	out, err := os.OpenFile(filepath.Join(dest, "db.sql.gz"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	cmd := exec.Command("docker", "exec", "remnawave-db", "sh", "-c",
		`pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB"`)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	if _, err := io.Copy(gz, pipe); err != nil {
		_ = cmd.Wait()
		return "", err
	}
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("pg_dump failed: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return dest, nil
}

// PromptBackup offers a database backup of the panel stack in dir,
// when one is installed and its database container is running.
func PromptBackup(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		return
	}
	if exec.Command("docker", "inspect", "-f", "{{.State.Running}}", "remnawave-db").Run() != nil {
		return
	}
	answer := ui.Reading(i18n.T("BACKUP_BEFORE_REMOVE"))
	if answer != "y" && answer != "Y" {
		return
	}
	dest, err := BackupPanel(dir)
	if err != nil {
		fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_FAILED"), err, ui.ColorReset)
		return
	}
	// Keep the backup outside the directory that is about to be deleted.
	keep := filepath.Join("/root", filepath.Base(dir)+"-backup-"+filepath.Base(dest))
	if err := os.Rename(dest, keep); err != nil {
		keep = dest
	}
	fmt.Printf("%s"+i18n.T("BACKUP_DONE")+"%s\n", ui.ColorGreen, keep, ui.ColorReset)
}

// ConfirmDelete asks the operator to type DELETE.
func ConfirmDelete() bool {
	if ui.Reading(i18n.T("CONFIRM_TYPE_DELETE")) != "DELETE" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONFIRM_ABORTED"), ui.ColorReset)
		return false
	}
	return true
}
