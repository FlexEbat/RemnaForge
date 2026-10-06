// Package backuprestore implements the "Backup and Restore" menu item.
//
// Backup and restore are handled by a separate, independently
// maintained tool, https://github.com/distillium/remnawave-backup-restore
// (MIT license; Google Drive/S3 upload, Telegram notifications, cron
// scheduling). This package fetches its script if it is not present yet
// and hands off to it interactively.
package backuprestore

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// scriptURL is the community backup/restore tool this package fetches.
const scriptURL = "https://raw.githubusercontent.com/distillium/remnawave-backup-restore/main/backup-restore.sh"

// Run downloads the community backup/restore script to $HOME if it
// isn't already there, then hands off to it interactively. If the
// script was already downloaded on a previous run, this uses the
// `rw-backup` command it installs on its own first run instead of
// downloading it again.
func Run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root" // this tool already requires root, so this is a safe fallback
	}
	scriptPath := filepath.Join(home, "backup-restore.sh")

	if _, lookErr := exec.LookPath("rw-backup"); lookErr == nil {
		return runInteractive("rw-backup")
	}
	if _, statErr := os.Stat(scriptPath); statErr == nil {
		return runInteractive(scriptPath)
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.DownloadingBackupRestore(), ui.ColorReset)
	if err := downloadScript(scriptPath); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DOWNLOAD_FAIL"), ui.ColorReset)
		return err
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return err
	}

	return runInteractive(scriptPath)
}

// downloadScript writes the script to a temporary file next to dest and
// renames it into place only once the download has completed, so an
// interrupted download never leaves a truncated script behind.
func downloadScript(dest string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(scriptURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, scriptURL)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".backup-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 5<<20)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// runInteractive runs name (a full path or something on $PATH) with its
// stdio wired directly to ours, the same pattern
// internal/managepanel.runRemnawaveCLI uses for `docker exec -it`: the
// downloaded tool's own menu, prompts, and progress output all pass
// through untouched.
func runInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
