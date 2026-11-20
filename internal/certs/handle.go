// Package certs is the certificate-management subsystem: checking
// existing certificates, issuing new ones, and the "Manage
// certificates" menu (update existing / generate new). All actual
// issuance goes through the `certbot` binary via os/exec, rather than
// reimplementing ACME/ECDSA cert issuance, a bigger and riskier
// undertaking than shelling out to a well-tested tool that a Remnawave
// server already needs installed.
//
// HandleCertificatesFor takes targetDir as an explicit parameter (rather
// than a single hardcoded path), so each caller's SSL docker-compose
// volume-mount lines get appended to its own compose file: this matters
// because internal/panelfull/internal/panelonly point at
// /opt/remnawave, while internal/nginxnode points at /opt/remnanode.
package certs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/domain"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/preflight"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Result is returned by HandleCertificatesFor so callers like nginxnode can
// know which domain's certificate to reference, instead of having every
// caller re-derive it.
type Result struct {
	Method string // "1" Cloudflare, "2" ACME HTTP-01, "3" Gcore
	// ComposeMounts holds the volume lines that mount the certificates into
	// the nginx container, one pair per certificate name. The caller puts
	// them into the compose file right after the open volumes list of the
	// nginx service, so the file is written once and whole. It is empty for
	// HAProxy, whose container mounts the bundle directory.
	ComposeMounts string
	// Skipped is true when the certificates already existed and none were
	// issued; Method is then not the method of any certificate on disk.
	Skipped bool
}

// CertName returns the certificate name that serves domain d: the name of
// the directory under /etc/letsencrypt/live that the proxy configuration and
// the compose mounts must use. A wildcard certificate (Cloudflare or Gcore)
// lives under the base domain, an HTTP-01 certificate under the domain
// itself. When the run found its certificates already issued, the answer
// comes from what is on disk, as it does when the mounts are written. The
// flows used to compare the method with "1" alone, which sent a Gcore
// wildcard and an existing per-domain certificate to the wrong directory.
func (r Result) CertName(d string) string {
	base := domain.ExtractDomain(d)
	if !r.Skipped {
		switch r.Method {
		case "1", "3":
			return base
		case "2":
			return d
		}
	}
	if _, err := os.Stat(filepath.Join(letsencryptLive, base)); err == nil && domain.IsWildcardCert(base) {
		return base
	}
	return d
}

// HandleCertificatesFor obtains the certificates and wires them to the
// proxy kind. Nginx gets read-only mounts of certbot's files appended to
// its compose file. HAProxy gets one PEM bundle per name, the renew script
// that rebuilds the bundles and reloads it, and no compose edit, because
// its container mounts the bundle directory.
func HandleCertificatesFor(kind proxy.Kind, domainsToCheck []string, targetDir string) (Result, error) {
	var certMethod, letsencryptEmail string
	needCertificates := false
	minDaysLeft := 9999

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CERTS"), ui.ColorReset)

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REQUIRED_DOMAINS"), ui.ColorReset)
	for _, d := range domainsToCheck {
		fmt.Printf("%s- %s%s\n", ui.ColorWhite, d, ui.ColorReset)
	}

	for _, d := range domainsToCheck {
		if !CheckCertificates(d) {
			needCertificates = true
		} else if days, err := CheckCertExpiry(d); err == nil && days < minDaysLeft {
			minDaysLeft = days
		}
	}

	if needCertificates {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_PROMPT"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_CF"), ui.ColorReset)
		fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_ACME"), ui.ColorReset)
		fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_GCORE"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		fmt.Println()
		certMethod = ui.Reading(i18n.T("CERT_METHOD_CHOOSE"))

		switch certMethod {
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return Result{}, fmt.Errorf("aborted")
		case "2", "3":
			letsencryptEmail = ui.Reading(i18n.T("EMAIL_PROMPT"))
		case "1":
			// no email needed
		default:
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_INVALID_CHOICE"), ui.ColorReset)
			return Result{}, fmt.Errorf("invalid cert method")
		}
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CERTS_SKIPPED"), ui.ColorReset)
		certMethod = "1"
	}

	certDomainsAdded := map[string]bool{}
	var certNames []string
	var mounts strings.Builder

	// addComposeCertLines records the nginx mounts of one certificate name,
	// once. Nothing is written to disk here: the caller owns the compose
	// file.
	addComposeCertLines := func(d string) error {
		if certDomainsAdded[d] {
			return nil
		}
		certDomainsAdded[d] = true
		if kind == proxy.HAProxy {
			certNames = append(certNames, d)
			return nil
		}
		mounts.WriteString(fmt.Sprintf("      - /etc/letsencrypt/live/%s/fullchain.pem:/etc/nginx/ssl/%s/fullchain.pem:ro\n      - /etc/letsencrypt/live/%s/privkey.pem:/etc/nginx/ssl/%s/privkey.pem:ro\n", d, d, d, d))
		return nil
	}

	uniqueDomains := map[string]bool{}

	// HTTP-01 issues from this server's port 80. A dry run against the
	// staging server proves DNS, the firewall and the port before the real
	// request, and keeps a failed run off the production rate limits.
	if needCertificates && certMethod == "2" {
		if !preflight.ConfirmDryRun(preflight.DefaultCheckEnv(), domainsToCheck, ui.Reading) {
			return Result{}, fmt.Errorf("aborted")
		}
	}

	switch {
	case needCertificates && (certMethod == "1" || certMethod == "3"):
		for _, d := range domainsToCheck {
			uniqueDomains[domain.ExtractDomain(d)] = true
		}
		for d := range uniqueDomains {
			if err := GetCertificates(d, certMethod, letsencryptEmail); err != nil {
				fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), d, ui.ColorReset)
				return Result{}, err
			}
			minDaysLeft = 90
			if err := addComposeCertLines(d); err != nil {
				return Result{}, err
			}
		}

	case needCertificates && certMethod == "2":
		for _, d := range domainsToCheck {
			if err := GetCertificates(d, "2", letsencryptEmail); err != nil {
				fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), d, ui.ColorReset)
				return Result{}, err
			}
			if err := addComposeCertLines(d); err != nil {
				return Result{}, err
			}
		}

	default:
		for _, d := range domainsToCheck {
			base := domain.ExtractDomain(d)
			certDomain := d
			if _, err := os.Stat(filepath.Join(letsencryptLive, base)); err == nil && domain.IsWildcardCert(base) {
				certDomain = base
			}
			if err := addComposeCertLines(certDomain); err != nil {
				return Result{}, err
			}
		}
	}

	cronCommand := renewCronCommand(certMethod)
	if needCertificates && certMethod == "2" {
		if err := InstallHTTP01Hook(); err != nil {
			return Result{}, err
		}
	}
	if kind == proxy.HAProxy {
		if err := InstallHAProxyHook(); err != nil {
			return Result{}, err
		}
		for _, name := range certNames {
			if err := validateBundleName(name); err != nil {
				return Result{}, err
			}
			if err := WriteHAProxyBundle(letsencryptLive, name); err != nil && !runctx.DryRun() {
				return Result{}, fmt.Errorf("certificate bundle for %s: %w", name, err)
			}
		}
	}
	// A failed crontab update is reported by the function; the certificates
	// are issued, so the run goes on.
	_ = syncCertRenewalCron(cronCommand, minDaysLeft, needCertificates)

	// Every certificate this run issued gets the renew_hook that restarts the
	// proxy, whatever the method: the HTTP-01 method had none, and the cron
	// job restarted the whole stack instead. An HAProxy bundle needs its hook
	// for existing certificates as well.
	if needCertificates || kind == proxy.HAProxy {
		for name := range certDomainsAdded {
			fixRenewHook(name)
		}
	}

	return Result{Method: certMethod, ComposeMounts: mounts.String(), Skipped: !needCertificates}, nil
}

// fixRenewHook makes sure each domain's renewal.conf has the
// panel-restart renew_hook. Shares renewHookCommand (check.go) with
// FixLetsencryptStructure so both write the same text; see the comment
// on renewHookCommand for why that matters.
func fixRenewHook(domainName string) {
	renewalConf := filepath.Join(letsencryptRenewal, domainName+".conf")
	data, err := os.ReadFile(renewalConf)
	if err != nil {
		return
	}
	conf := string(data)
	desiredHook := renewHookFor(domainName)

	if !strings.Contains(conf, "renew_hook") {
		conf = strings.TrimRight(conf, "\n") + "\n" + desiredHook + "\n"
		_ = cfgfile.Replace(renewalConf, conf, cfgfile.Opts{})
		return
	}

	hasExact := false
	lines := strings.Split(conf, "\n")
	for _, line := range lines {
		if line == desiredHook {
			hasExact = true
			break
		}
	}
	if !hasExact {
		for i, line := range lines {
			if strings.Contains(line, "renew_hook") {
				lines[i] = desiredHook
			}
		}
		_ = cfgfile.Replace(renewalConf, strings.Join(lines, "\n"), cfgfile.Opts{})
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATED_RENEW_AUTH"), ui.ColorReset)
	}
}
