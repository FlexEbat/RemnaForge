// Package cfgfiletest keeps the tests of packages that write configuration
// files away from the real manifest and backup directories, which only root
// can create. A package whose code reaches cfgfile calls Main from TestMain.
package cfgfiletest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
)

// Main points cfgfile at a temporary directory for the whole test binary,
// runs the tests and exits.
func Main(m *testing.M) {
	dir, err := os.MkdirTemp("", "cfgfile-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfgfile.ManifestPath = filepath.Join(dir, "manifest.json")
	cfgfile.BackupRoot = filepath.Join(dir, "backups")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
