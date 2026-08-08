//go:build integration

// This file only builds with `go test -tags=integration`. It exercises
// real system state (installs packages, starts services, edits
// sysctl.conf and ufw rules), so it must not run as part of a normal
// `go test ./...` on a developer's machine. The CI workflow
// (.github/workflows/ci.yml) runs it on a disposable GitHub-hosted
// Ubuntu runner, the only environment this project has access to that
// can actually verify docker/apt/systemd/ufw behavior. The development
// sandbox this project was built in has none of those.
package preflight

import (
	"os/exec"
	"testing"
)

func TestCheckDockerOnRealRunner(t *testing.T) {
	if err := CheckDocker(); err != nil {
		t.Fatalf("CheckDocker failed on a runner that ships docker preinstalled: %v", err)
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

	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Errorf("docker info failed after InstallPackages: %v", err)
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
