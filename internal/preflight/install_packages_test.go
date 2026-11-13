package preflight

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/sysinfo"
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

func TestReportEnvironmentRefusesTinyHost(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	env := sysinfo.Env{
		OSReleasePath: write("os-release", "ID=debian\nVERSION_ID=\"12\"\n"),
		MeminfoPath:   write("meminfo", "MemTotal: 524288 kB\n"),
		KernelPath:    write("osrelease", "6.1.0\n"),
		SystemdDir:    dir,
		Arch:          "amd64",
		LookPath:      func(string) (string, error) { return "", errors.New("none") },
		FreeBytes:     func(string) (uint64, error) { return 100 << 30, nil },
		Run:           func(string, ...string) ([]byte, error) { t.Fatal("repos checked although not asked"); return nil, nil },
	}
	if err := reportEnvironment(env, false); err == nil {
		t.Fatal("a host with 512 MiB must be refused")
	}
	if err := os.WriteFile(env.MeminfoPath, []byte("MemTotal: 4194304 kB\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := reportEnvironment(env, false); err != nil {
		t.Fatalf("a host with 4 GiB was refused: %v", err)
	}
}
