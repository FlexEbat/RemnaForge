// Package preflight holds environment checks and, via
// EnsureInstalled/InstallPackages, the actual dependency bootstrap that
// install flows run before doing real work.
//
// install_packages.go auto-bootstraps docker/certbot/ufw/cron when
// missing, so an install flow's later `docker compose up -d` doesn't
// fail silently on a fresh server and only surface as a "containers not
// ready" timeout minutes later. Docker itself is handled by
// internal/dockercheck. CheckCertbot remains a standalone check for
// callers that only need to verify state (the "Manage Certificates"
// menu should not reinstall the world just to check a certificate).
package preflight

import (
	"fmt"
	"os/exec"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

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
