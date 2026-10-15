// Package backuprestore implements the "Backup and Restore" menu item.
//
// Backup and restore are handled by a separate, independently
// maintained tool, https://github.com/distillium/remnawave-backup-restore
// (MIT license; Google Drive/S3 upload, Telegram notifications, cron
// scheduling). This package fetches its script if it is not present yet
// and hands off to it interactively.
package backuprestore

import (
	"crypto/sha256"
	"encoding/hex"
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

// scriptRef is the commit of the community backup/restore tool this
// package fetches, and scriptSHA256 the SHA-256 of backup-restore.sh at
// that commit. The script runs as root, so the download is pinned to a
// commit and verified instead of following the moving main branch. To
// update: pick a commit, review the script, then set both constants to
// the new commit and `sha256sum backup-restore.sh`.
const (
	scriptRef    = "a24e020eac10ad1bc0576d55a87a1c01f0ef9048"
	scriptSHA256 = "3b73286792ad71538dba156ebc3ae42e58699cf2fceb4126ec2bcd8dc8e2cc82"
)

const scriptURL = "https://raw.githubusercontent.com/distillium/remnawave-backup-restore/" + scriptRef + "/backup-restore.sh"

// maxScriptSize caps the download; the pinned script is far smaller.
const maxScriptSize = 5 << 20

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
// renames it into place only once the download is complete and matches
// scriptSHA256, so neither an interrupted download nor a tampered one
// ever ends up as an executable script.
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxScriptSize+1))
	if err != nil {
		return err
	}
	if len(data) > maxScriptSize {
		return fmt.Errorf("script from %s exceeds %d bytes", scriptURL, maxScriptSize)
	}
	if err := verifyScript(data, scriptSHA256); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".backup-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// verifyScript compares the SHA-256 of data with the expected hex digest.
func verifyScript(data []byte, wantHex string) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != wantHex {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, wantHex)
	}
	return nil
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
