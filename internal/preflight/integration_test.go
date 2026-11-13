//go:build integration

// This file only builds with `go test -tags=integration`. It exercises
// real system state (installs packages, starts services, edits
// sysctl.conf and ufw rules), so it must not run as part of a normal
// `go test ./...` on a developer's machine. The CI workflow
// (.github/workflows/ci.yml) runs it on a disposable GitHub-hosted
// Ubuntu runner, which has docker, apt, systemd and ufw available.
package preflight

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
	"github.com/FlexEbat/RemnaForge/internal/execx"
)

func TestDockerWorkingOnRealRunner(t *testing.T) {
	if !dockercheck.Working(dockercheck.DefaultEnv()) {
		t.Fatal("docker is not working on a runner that ships docker preinstalled")
	}
}

func TestInstallPackagesOnRealRunner(t *testing.T) {
	if err := InstallPackages(); err != nil {
		t.Fatalf("InstallPackages failed: %v", err)
	}

	if _, err := exec.LookPath("certbot"); err != nil {
		t.Error("certbot not on PATH after InstallPackages")
	}
	if _, err := exec.LookPath("ufw"); err != nil {
		t.Error("ufw not on PATH after InstallPackages")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Error("docker not on PATH after InstallPackages")
	}

	if !dockercheck.Working(dockercheck.DefaultEnv()) {
		t.Error("docker is not working after InstallPackages")
	}
}

// The install flow must leave BBR and the firewall to the operator: the
// System tuning menu is the only place that turns them on.
func TestInstallPackagesLeavesHostSettingsAloneOnRealRunner(t *testing.T) {
	if _, err := os.Stat("/etc/sysctl.d/99-remnaforge-bbr.conf"); err == nil {
		t.Error("the BBR drop-in exists after InstallPackages")
	}
	out, err := execx.Command("ufw", "status").Output()
	if err == nil && !strings.Contains(string(out), "inactive") {
		t.Errorf("ufw is active after InstallPackages:\n%s", out)
	}
}

func TestEnsureInstalledIsIdempotent(t *testing.T) {
	// Running EnsureInstalled a second time (the marker file and every
	// dependency already satisfied from the previous test) must not
	// error and must not re-run the full bootstrap.
	if err := EnsureInstalled(); err != nil {
		t.Fatalf("EnsureInstalled failed on second run: %v", err)
	}
}
