package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nothing outside this package may turn a firewall on or change its default
// policy: every such change goes through Apply, which needs a Consent.
func TestNoCodeOutsideThisPackageEnablesAFirewall(t *testing.T) {
	banned := []string{
		`"--force", "enable"`, `"enable", "--now", "firewalld"`, `remnaforge-firewall`,
		`"ufw", "default"`, `"-P", "INPUT"`, `"ufw", "enable"`,
	}
	root := filepath.Join("..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "firewall" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %s", path, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The one place that turns a firewall on takes a Consent.
func TestPlanCommandsAreOnlyRunByApply(t *testing.T) {
	src, err := os.ReadFile("flow.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "env.Exec(") {
		t.Error("flow.go runs commands itself instead of through Apply")
	}
}
