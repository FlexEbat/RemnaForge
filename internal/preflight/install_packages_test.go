package preflight

import (
	"os"
	"strings"
	"testing"
)

// The install flow must not change the host beyond its own dependencies:
// BBR, automatic updates and the firewall are opt-in (internal/systune).
func TestInstallPackagesLeavesHostSettingsAlone(t *testing.T) {
	for _, p := range aptPackages {
		if p == "unattended-upgrades" {
			t.Errorf("aptPackages installs %s, which only the System tuning menu may do", p)
		}
	}
	src, err := os.ReadFile("install_packages.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{
		`"ufw", "--force"`, `"ufw", "allow"`, `"ufw", "enable"`,
		"sysctl", "dpkg-reconfigure", "unattended", "enableBBR",
	} {
		if strings.Contains(string(src), banned) {
			t.Errorf("install_packages.go contains %q", banned)
		}
	}
}

func TestAptPackagesHaveNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range aptPackages {
		if seen[p] {
			t.Errorf("%s listed twice", p)
		}
		seen[p] = true
	}
}
