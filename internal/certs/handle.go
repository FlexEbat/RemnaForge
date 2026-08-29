// Package certs is a full port of the certificate-management subsystem in
// install_remnawave.sh: check_certificates(), check_api(),
// get_certificates(), check_cert_expiry(), fix_letsencrypt_structure(),
// handle_certificates(), and the "Manage certificates" menu
// (show_manage_certificates/manage_certificates/update_current_certificates/
// generate_new_certificates). All actual issuance goes through the
// `certbot` binary via os/exec, matching the original. Reimplementing
// ACME/ECDSA cert issuance would be a bigger and riskier undertaking than
// shelling out to a well-tested tool that a Remnawave server already
// needs installed.
//
// Deliberate improvement over a literal port: handle_certificates()
// hardcodes target_dir="/opt/remnawave" (install_remnawave.sh:1970), which
// means every caller's SSL docker-compose volume-mount lines get appended
// to the *panel's* compose file regardless of which flow invoked it.
// This is harmless for install_panel/install_panel_node (which do use
// /opt/remnawave), but wrong for a standalone install_node
// (/opt/remnanode). HandleCertificates here takes targetDir as an
// explicit parameter instead of hardcoding it, so each caller points at
// its own compose file.
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

// Result is returned by HandleCertificates. The original has no
// equivalent return value; CERT_METHOD stays a global there. Callers
// like nginxnode need to know which domain's certificate to reference,
// so this type surfaces it instead of making every caller re-derive it.
type Result struct {
	Method string // "1" Cloudflare, "2" ACME HTTP-01, "3" Gcore
}

// Original bash (install_remnawave.sh:1966-2124): handle_certificates().
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

// syncCertRenewalCron is the Go equivalent of install_remnawave.sh:2101-2111:
// ensure a weekly root crontab entry exists for certbot renewal, replacing
// it if the certificate is close to expiry and the entry doesn't already
// match.
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

// fixRenewHook is the Go equivalent of install_remnawave.sh:2113-2123: make
// sure each domain's renewal.conf has the panel-restart renew_hook.
//
// BUG FIX (not a 1:1 port): now shares renewHookCommand (check.go) with
// FixLetsencryptStructure instead of hardcoding its own, older text
// missing the trailing nginx reload step; see the comment on
// renewHookCommand for why.
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
