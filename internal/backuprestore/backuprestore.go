// Package backuprestore is a port of the "Backup and Restore" menu item
// (install_remnawave.sh:2272-2277).
//
// The original doesn't implement backup/restore itself. It downloads and
// runs a separate, independently maintained tool:
// https://github.com/distillium/remnawave-backup-restore (MIT license,
// ~3600 lines of bash: Google Drive/S3 upload, Telegram notifications,
// its own cron scheduling, its own translations). Reimplementing that
// tool in Go would mean forking and maintaining a second, unrelated
// project inside this one. Instead, this package does exactly what the
// original menu item does: fetch the script if it isn't cached yet, then
// hand off to it interactively. That is a complete, honestly-scoped port
// of what "Backup and Restore" actually is here, not a stub.
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

// scriptURL matches install_remnawave.sh:2276 exactly.
const scriptURL = "https://raw.githubusercontent.com/distillium/remnawave-backup-restore/main/backup-restore.sh"

// Run is the Go equivalent of:
//
//	if [ -f ~/backup-restore.sh ]; then
//	    rw-backup
//	else
//	    curl -o ~/backup-restore.sh https://raw.githubusercontent.com/distillium/remnawave-backup-restore/main/backup-restore.sh && chmod +x ~/backup-restore.sh && ~/backup-restore.sh
//	fi
//
// (install_remnawave.sh:2272-2277). If the script was already downloaded
// on a previous run, this uses the `rw-backup` command it installs on its
// own first run, exactly like the original. Otherwise it downloads a
// fresh copy to $HOME and runs that directly.
func Run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root" // this tool already requires root; matches the original's ~ under sudo
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
