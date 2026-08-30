// Package backuprestore implements the "Backup and Restore" menu item.
//
// This project doesn't implement backup/restore itself. It downloads
// and runs a separate, independently maintained tool:
// https://github.com/distillium/remnawave-backup-restore (MIT license,
// ~3600 lines of bash: Google Drive/S3 upload, Telegram notifications,
// its own cron scheduling, its own translations). Reimplementing that
// tool in Go would mean forking and maintaining a second, unrelated
// project inside this one. Instead, this package fetches the script if
// it isn't cached yet, then hands off to it interactively.
package backuprestore

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
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

	if _, statErr := os.Stat(scriptPath); statErr == nil {
		return runInteractive("rw-backup")
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

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
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
