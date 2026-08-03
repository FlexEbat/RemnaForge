// Package preflight holds environment checks - and, via
// EnsureInstalled/InstallPackages, the actual dependency bootstrap - that
// install flows run before doing real work.
//
// CheckDocker/CheckCertbot below started out as preflight-only checks (see
// git history) for a gap the original bash script had: its install flows
// didn't check docker at all (they just ran `docker compose up -d` and
// let it silently fail ~5 minutes later as a confusing "containers not
// ready" timeout), and only checked for certbot reactively inside the
// "Manage Certificates" menu, not before the install flows that also need
// it. install_packages.go (a full port of install_packages(),
// install_remnawave.sh:1229-1326) closes that gap properly: install flows
// now call EnsureInstalled(), which auto-bootstraps docker/certbot/ufw/
// cron/unattended-upgrades/BBR when missing - matching the original's
// actual behavior (it always auto-installs on a missing dependency,
// rather than just erroring) instead of merely reporting the problem.
// CheckDocker/CheckCertbot remain as standalone checks for callers that
// only want to *verify* (e.g. the "Manage Certificates" menu, which
// shouldn't reinstall the world just to check a certificate).
package preflight

import (
	"fmt"
	"os/exec"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// CheckDocker verifies both that the `docker` binary exists AND that the
// daemon is actually reachable and the `compose` plugin is present -
// `docker` being on $PATH doesn't guarantee either (daemon not running,
// current user lacking permission, or an old docker without the compose
// plugin are all common on a freshly-provisioned VPS).
func CheckDocker() error {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_INSTALLED"), ui.ColorReset)
		return fmt.Errorf("docker not installed")
	}

	if err := exec.Command("docker", "info").Run(); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_WORKING"), ui.ColorReset)
		return fmt.Errorf("docker daemon not reachable")
	}

	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_WORKING"), ui.ColorReset)
		return fmt.Errorf("docker compose plugin not available")
	}

	return nil
}

// CheckCertbot verifies the `certbot` binary is on $PATH. Shared by
// internal/certs (the "Manage Certificates" menu) and every install flow
// that ends up calling certs.HandleCertificates/GetCertificates.
func CheckCertbot() error {
	if _, err := exec.LookPath("certbot"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_CERTBOT"), ui.ColorReset)
		return fmt.Errorf("certbot not installed")
	}
	return nil
}
