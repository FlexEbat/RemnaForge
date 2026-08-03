package certs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// ensureCertbot is the Go equivalent of the `command -v certbot ||
// install_packages` guard repeated in manage_certificates()
// (install_remnawave.sh:1637-1643, 1648-1654). install_packages() itself
// (the apt bootstrap routine) isn't ported yet, so if certbot is missing
// we report the same error the original would show if that install failed,
// rather than silently doing nothing. Delegates to internal/preflight so
// install flows can run the identical check before they even get here.
func ensureCertbot() error {
	return preflight.CheckCertbot()
}

// Original bash (install_remnawave.sh:1621-1630): show_manage_certificates().
func showManageCertificates() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_9"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("CERT_UPDATE"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("CERT_GENERATE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

// ManageCertificates is the menu-facing entry point, wired into
// internal/menu for "Manage certificates domain".
// Original bash (install_remnawave.sh:1632-1667): manage_certificates().
func ManageCertificates() {
	showManageCertificates()
	option := ui.Reading(i18n.T("CERT_PROMPT1"))

	switch option {
	case "1":
		if err := ensureCertbot(); err != nil {
			return
		}
		updateCurrentCertificates()
	case "2":
		if err := ensureCertbot(); err != nil {
			return
		}
		generateNewCertificates()
	case "0":
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	default:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_INVALID_CHOICE"), ui.ColorReset)
	}
}

// Original bash (install_remnawave.sh:1669-1818): update_current_certificates().
func updateCurrentCertificates() {
	if _, err := os.Stat(letsencryptLive); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), ui.ColorReset)
		return
	}

	const renewThreshold = 30
	logDir := "/var/log/letsencrypt"
	if _, err := os.Stat(logDir); err != nil {
		_ = os.MkdirAll(logDir, 0755)
	}

	entries, err := os.ReadDir(letsencryptLive)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), ui.ColorReset)
		return
	}

	// unique_domains: strip a trailing "-NNNN" suffix so cert renewal
	// conflicts (example.com, example.com-0001, ...) collapse to one entry,
	// keeping whichever directory sorts last.
	uniqueDomains := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		certDomain := stripDashSuffix(e.Name())
		if existing, ok := uniqueDomains[certDomain]; !ok || natLess(filepath.Base(existing), e.Name()) {
			uniqueDomains[certDomain] = filepath.Join(letsencryptLive, e.Name())
		}
	}

	certStatus := map[string]string{}

	// BUG FIX (not in the original): Go map iteration order is randomized
	// per-run by design, unlike bash's associative arrays which iterate in
	// insertion order. Sorting the keys here makes both the processing
	// order and (more importantly) the final results summary below
	// reproducible between runs, instead of shuffling every time.
	domainsInOrder := make([]string, 0, len(uniqueDomains))
	for certDomain := range uniqueDomains {
		domainsInOrder = append(domainsInOrder, certDomain)
	}
	sort.Strings(domainsInOrder)

	for _, certDomain := range domainsInOrder {
		domainDir := uniqueDomains[certDomain]
		domainName := filepath.Base(domainDir)

		certMethod := "2" // default: ACME HTTP-01
		renewalConf := filepath.Join(letsencryptRenewal, domainName+".conf")
		if data, err := os.ReadFile(renewalConf); err == nil {
			conf := string(data)
			switch {
			case strings.Contains(conf, "dns_cloudflare"):
				certMethod = "1"
			case strings.Contains(conf, "dns-gcore"):
				certMethod = "3"
			}
		}

		certFile := filepath.Join(domainDir, "fullchain.pem")
		mtimeBefore := fileModTime(certFile)

		_ = FixLetsencryptStructure(certDomain)

		daysLeft, expErr := CheckCertExpiry(domainName)
		if expErr != nil {
			certStatus[certDomain] = i18n.T("ERROR_PARSING_CERT")
			continue
		}

		if certMethod == "1" || certMethod == "3" {
			ensureDNSCredentials(certMethod, renewalConf)
		}

		if daysLeft <= renewThreshold {
			if certMethod == "2" {
				_ = exec.Command("ufw", "allow", "80/tcp").Run()
				_ = exec.Command("ufw", "reload").Run()
			}

			fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
			renewCmd := exec.Command("certbot", "renew", "--cert-name", domainName, "--no-random-sleep-on-renew")
			logFile, _ := os.OpenFile(filepath.Join(logDir, "letsencrypt.log"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
			if logFile != nil {
				renewCmd.Stdout = logFile
				renewCmd.Stderr = logFile
			}
			renewErr := renewCmd.Run()
			if logFile != nil {
				logFile.Close()
			}

			if certMethod == "2" {
				_ = exec.Command("ufw", "delete", "allow", "80/tcp").Run()
				_ = exec.Command("ufw", "reload").Run()
			}

			if renewErr != nil {
				certStatus[certDomain] = i18n.T("ERROR_UPDATE") + ": " + i18n.T("RATE_LIMIT_EXCEEDED")
				continue
			}

			newDir := latestMatchingDir(letsencryptLive, certDomain)
			newDomain := filepath.Base(newDir)
			mtimeAfter := fileModTime(filepath.Join(newDir, "fullchain.pem"))

			if CheckCertificates(newDomain) && mtimeBefore != mtimeAfter {
				if _, expErr := CheckCertExpiry(newDomain); expErr == nil {
					certStatus[certDomain] = i18n.T("UPDATED")
				} else {
					certStatus[certDomain] = i18n.T("ERROR_PARSING_CERT")
				}
			} else {
				certStatus[certDomain] = i18n.T("ERROR_UPDATE")
			}
		} else {
			certStatus[certDomain] = fmt.Sprintf("%s %d %s", i18n.T("REMAINING"), daysLeft, i18n.T("DAYS"))
		}
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RESULTS_CERTIFICATE_UPDATES"), ui.ColorReset)
	for _, certDomain := range domainsInOrder {
		status := certStatus[certDomain]
		switch {
		case status == i18n.T("UPDATED"):
			fmt.Printf("%s%s%s %s%s\n", ui.ColorGreen, i18n.T("CERTIFICATE_FOR"), certDomain, i18n.T("SUCCESSFULLY_UPDATED"), ui.ColorReset)
		case strings.Contains(status, i18n.T("ERROR_UPDATE")):
			fmt.Printf("%s%s%s: %s%s\n", ui.ColorRed, i18n.T("FAILED_TO_UPDATE_CERTIFICATE_FOR"), certDomain, status, ui.ColorReset)
		case status == i18n.T("ERROR_PARSING_CERT"):
			fmt.Printf("%s%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CHECKING_EXPIRY_FOR"), certDomain, ui.ColorReset)
		default:
			fmt.Printf("%s%s%s %s%s)%s\n", ui.ColorYellow, i18n.T("CERTIFICATE_FOR"), certDomain, i18n.T("DOES_NOT_REQUIRE_UPDATE"), status, ui.ColorReset)
		}
	}
}

// ensureDNSCredentials is the Go equivalent of install_remnawave.sh:
// 1724-1756: if the renewal.conf points at a credentials file that's gone
// missing, re-prompt for the API key/email and recreate it.
func ensureDNSCredentials(certMethod, renewalConf string) {
	data, err := os.ReadFile(renewalConf)
	if err != nil {
		return
	}
	conf := string(data)

	if certMethod == "1" {
		credFile := renewalConfValue(conf, "dns_cloudflare_credentials")
		if credFile == "" {
			return
		}
		if _, err := os.Stat(credFile); err == nil {
			return
		}
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_CLOUDFLARE_FILE_NOT_FOUND"), ui.ColorReset)
		email := ui.Reading(ui.ColorYellow + i18n.T("ENTER_CF_EMAIL") + ui.ColorReset)
		apiKey := ui.Reading(ui.ColorYellow + i18n.T("ENTER_CF_TOKEN") + ui.ColorReset)
		validKey, validEmail, err := CheckAPI(apiKey, email)
		if err != nil {
			return
		}
		_ = os.MkdirAll(filepath.Dir(credFile), 0755)
		ini := fmt.Sprintf("dns_cloudflare_email = %s\ndns_cloudflare_api_key = %s\n", validEmail, validKey)
		_ = os.WriteFile(credFile, []byte(ini), 0600)

	} else if certMethod == "3" {
		credFile := renewalConfValue(conf, "dns-gcore-credentials")
		if credFile == "" {
			return
		}
		if _, err := os.Stat(credFile); err == nil {
			return
		}
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_GCORE_FILE_NOT_FOUND"), ui.ColorReset)
		gcoreKey := ui.Reading(ui.ColorYellow + i18n.T("ENTER_GCORE_TOKEN") + ui.ColorReset)
		_ = os.MkdirAll(filepath.Dir(credFile), 0755)
		_ = os.WriteFile(credFile, []byte(fmt.Sprintf("dns_gcore_apitoken = %s\n", gcoreKey)), 0600)
	}
}

// Original bash (install_remnawave.sh:1820-1865): generate_new_certificates().
func generateNewCertificates() {
	newDomain := ui.Reading(i18n.T("CERT_GENERATE_PROMPT"))

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_PROMPT"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_CF"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_ACME"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("CERT_METHOD_GCORE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
	certMethod := ui.Reading(i18n.T("CERT_METHOD_CHOOSE"))

	if certMethod == "0" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return
	}

	var letsencryptEmail string
	if certMethod == "2" || certMethod == "3" {
		letsencryptEmail = ui.Reading(i18n.T("EMAIL_PROMPT"))
	}

	switch certMethod {
	case "1", "3":
		fmt.Printf("%s%s *.%s...%s\n", ui.ColorYellow, i18n.T("GENERATING_WILDCARD_CERT"), newDomain, ui.ColorReset)
		if err := GetCertificates(newDomain, certMethod, letsencryptEmail); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), ui.ColorReset)
			return
		}
	case "2":
		fmt.Printf("%s%s %s...%s\n", ui.ColorYellow, i18n.T("GENERATING_CERTS"), newDomain, ui.ColorReset)
		if err := GetCertificates(newDomain, "2", letsencryptEmail); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), ui.ColorReset)
			return
		}
	default:
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_INVALID_CHOICE"), ui.ColorReset)
		return
	}

	if CheckCertificates(newDomain) {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CERT_UPDATE_SUCCESS"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), ui.ColorReset)
	}
}

func stripDashSuffix(name string) string {
	if idx := strings.LastIndex(name, "-"); idx != -1 {
		suffix := name[idx+1:]
		if isAllDigits(suffix) {
			return name[:idx]
		}
	}
	return name
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func fileModTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}
