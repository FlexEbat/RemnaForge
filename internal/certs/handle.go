// Package certs is the certificate-management subsystem: checking
// existing certificates, issuing new ones, and the "Manage
// certificates" menu (update existing / generate new). All actual
// issuance goes through the `certbot` binary via os/exec, rather than
// reimplementing ACME/ECDSA cert issuance, a bigger and riskier
// undertaking than shelling out to a well-tested tool that a Remnawave
// server already needs installed.
//
// HandleCertificates takes targetDir as an explicit parameter (rather
// than a single hardcoded path), so each caller's SSL docker-compose
// volume-mount lines get appended to its own compose file: this matters
// because internal/panelfull/internal/panelonly point at
// /opt/remnawave, while internal/nginxnode points at /opt/remnanode.
package certs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// Result is returned by HandleCertificates so callers like nginxnode can
// know which domain's certificate to reference, instead of having every
// caller re-derive it.
type Result struct {
	Method string // "1" Cloudflare, "2" ACME HTTP-01, "3" Gcore
}

func HandleCertificates(domainsToCheck []string, certMethod, letsencryptEmail, targetDir string) (Result, error) {
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

	composePath := filepath.Join(targetDir, "docker-compose.yml")
	certDomainsAdded := map[string]bool{}

	addComposeCertLines := func(d string) error {
		if certDomainsAdded[d] {
			return nil
		}
		line := fmt.Sprintf("      - /etc/letsencrypt/live/%s/fullchain.pem:/etc/nginx/ssl/%s/fullchain.pem:ro\n      - /etc/letsencrypt/live/%s/privkey.pem:/etc/nginx/ssl/%s/privkey.pem:ro\n", d, d, d, d)
		f, err := os.OpenFile(composePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(line); err != nil {
			return err
		}
		certDomainsAdded[d] = true
		return nil
	}

	uniqueDomains := map[string]bool{}

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
				continue
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

	cronCommand := "/usr/bin/certbot renew --quiet"
	if certMethod == "2" {
		cronCommand = "ufw allow 80 && /usr/bin/certbot renew --quiet && ufw delete allow 80 && ufw reload && cd /opt/remnawave && docker compose down && docker compose up"
	}
	syncCertRenewalCron(cronCommand, minDaysLeft)

	for d := range uniqueDomains {
		fixRenewHook(d)
	}

	return Result{Method: certMethod}, nil
}

// syncCertRenewalCron ensures a weekly root crontab entry exists for
// certbot renewal, replacing it if the certificate is close to expiry
// and the entry doesn't already match.
func syncCertRenewalCron(cronCommand string, minDaysLeft int) {
	existing := currentCrontab()

	hasRenewLine := strings.Contains(existing, "/usr/bin/certbot renew")
	hasExactRule := strings.Contains(existing, "0 5 * * 0")

	switch {
	case !hasRenewLine:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADDING_CRON_FOR_EXISTING_CERTS"), ui.ColorReset)
		addCronRule(existing, cronCommand)

	case minDaysLeft <= 30 && !hasExactRule:
		fmt.Printf("%s%s %d %s%s\n", ui.ColorYellow, i18n.T("CERT_EXPIRY_SOON"), minDaysLeft, i18n.T("DAYS"), ui.ColorReset)
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATING_CRON"), ui.ColorReset)
		var kept []string
		for _, line := range strings.Split(existing, "\n") {
			if !strings.Contains(line, "/usr/bin/certbot renew") {
				kept = append(kept, line)
			}
		}
		addCronRule(strings.Join(kept, "\n"), cronCommand)

	default:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CRON_ALREADY_EXISTS"), ui.ColorReset)
	}
}

func currentCrontab() string {
	out, _ := exec.Command("crontab", "-u", "root", "-l").Output()
	return string(out)
}

// addCronRule is the Go equivalent of: (crontab -l; echo "0 5 * * 0 $cmd") | crontab -
func addCronRule(existing, cronCommand string) {
	rule := "0 5 * * 0 " + cronCommand
	newCrontab := strings.TrimRight(existing, "\n")
	if newCrontab != "" {
		newCrontab += "\n"
	}
	newCrontab += rule + "\n"

	cmd := exec.Command("crontab", "-u", "root", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	_ = cmd.Run()
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
	desiredHook := renewHookCommand

	if !strings.Contains(conf, "renew_hook") {
		conf = strings.TrimRight(conf, "\n") + "\n" + desiredHook + "\n"
		_ = os.WriteFile(renewalConf, []byte(conf), 0644)
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
		_ = os.WriteFile(renewalConf, []byte(strings.Join(lines, "\n")), 0644)
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATED_RENEW_AUTH"), ui.ColorReset)
	}
}
