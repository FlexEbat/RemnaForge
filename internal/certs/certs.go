// Package certs is a partial port of the certificate-management
// subsystem in install_remnawave.sh: handle_certificates()
// (lines 1966-2100+), check_certificates(), get_certificates()
// (~360 lines: acme.sh install, Cloudflare DNS-01, Gcore DNS-01, certbot
// HTTP-01, cron-based renewal), and check_cert_expiry().
//
// SCOPE NOTE: only the menu/UI shell of handle_certificates() is ported
// here (listing required domains, prompting for a method). The actual
// issuance backends (get_certificates, check_certificates,
// check_cert_expiry) are a large, separate subsystem - comparable in size
// to an entire install module on its own - and are deliberately left as a
// stub for a dedicated future pass, rather than rushed. This mirrors menu
// item "Manage certificates domain", which is stubbed the same way.
//
// Callers (nginxnode.InstallationNode, etc.) can still run end-to-end: if
// no real certificate exists yet, HandleCertificates reports that and lets
// the caller fall back to whatever docker-compose/nginx.conf paths make
// sense (matching how the original code behaves when cert_method ends up
// empty - see comments in internal/nginxnode).
package certs

import (
	"fmt"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// Result is returned by HandleCertificates.
type Result struct {
	Method string // "1" Cloudflare/existing, "2" ACME HTTP-01, "3" Gcore
}

// Original bash (install_remnawave.sh:1966-2100+): handle_certificates().
// domainsToCheck mirrors the nameref'd associative array of domain names.
func HandleCertificates(domainsToCheck []string, certMethod, letsencryptEmail string) (Result, error) {
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CERTS"), ui.ColorReset)

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REQUIRED_DOMAINS"), ui.ColorReset)
	for _, d := range domainsToCheck {
		fmt.Printf("%s- %s%s\n", ui.ColorWhite, d, ui.ColorReset)
	}

	// TODO(certs): this is where check_certificates()/check_cert_expiry()
	// would inspect /etc/letsencrypt/live/<domain> for each entry and decide
	// need_certificates. Until that's ported, we conservatively assume
	// certificates are needed so the method prompt always runs (safe
	// default: never silently skip issuance).
	needCertificates := true

	if !needCertificates {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CERTS_SKIPPED"), ui.ColorReset)
		return Result{Method: "1"}, nil
	}

	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_PROMPT"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_CF"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_ACME"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_GCORE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
	method := ui.Reading(i18n.T("CERT_METHOD_CHOOSE"))

	switch method {
	case "0":
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return Result{}, fmt.Errorf("aborted")
	case "2", "3":
		if letsencryptEmail == "" {
			letsencryptEmail = ui.Reading(i18n.T("EMAIL_PROMPT"))
		}
	case "1":
		// no email needed
	default:
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_INVALID_CHOICE"), ui.ColorReset)
		return Result{}, fmt.Errorf("invalid cert method")
	}

	fmt.Printf("%s[certs] %s%s\n", ui.ColorGray, i18n.InDevelopment(), ui.ColorReset)
	// TODO(certs): get_certificates(domain, method, email) - acme.sh
	// install + Cloudflare/Gcore DNS-01 token flow, or certbot HTTP-01
	// standalone, plus writing the *.pem docker-compose volume mounts and
	// setting up the renewal cron job. Not ported yet.

	return Result{Method: method}, nil
}
