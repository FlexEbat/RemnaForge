// Package preflight holds environment checks that install flows should
// run *before* doing real work, so a missing dependency fails fast with a
// clear, localized message instead of surfacing much later as a confusing
// symptom (e.g. "containers not ready" after a multi-minute retry loop,
// or a raw Go `exec: "certbot": executable file not found in $PATH`
// error).
//
// This isn't a port of any single original bash function - the original
// script's install flows didn't preflight-check docker at all (they just
// ran `docker compose up -d` and let it silently fail), and only checked
// for certbot reactively inside the "Manage Certificates" menu
// (manage_certificates(), install_remnawave.sh:1637-1654), not before the
// install flows that also end up needing it (get_certificates() is called
// from install_node/install_panel/install_panel_node too). Both gaps are
// fixed here, using LANG keys the original already defines
// (ERROR_DOCKER_NOT_INSTALLED/ERROR_DOCKER_NOT_WORKING - originally meant
// for install_packages(), which isn't ported - and
// ERROR_INSTALL_CERTBOT) rather than inventing new ones.
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
