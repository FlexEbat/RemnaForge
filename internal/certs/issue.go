package certs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

var hasUpperRE = regexp.MustCompile(`[A-Z]`)

// secretsDir is the Go equivalent of `~/.secrets/certbot`.
func secretsDir() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".secrets", "certbot")
	}
	return "/root/.secrets/certbot"
}

// Original bash (install_remnawave.sh:1481-1505): check_api().
// Validates Cloudflare credentials against the Cloudflare API, prompting
// up to 3 times like the original. Returns the (possibly re-entered)
// apiKey/email, since Go can't mutate the caller's variables the way
// bash's globals do.
func CheckAPI(apiKey, email string) (validAPIKey, validEmail string, err error) {
	const attempts = 3
	client := &http.Client{Timeout: 15 * time.Second}

	for attempt := 1; attempt <= attempts; attempt++ {
		req, _ := http.NewRequest("GET", "https://api.cloudflare.com/client/v4/zones", nil)
		if hasUpperRE.MatchString(apiKey) {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set("X-Auth-Key", apiKey)
			req.Header.Set("X-Auth-Email", email)
		}
		req.Header.Set("Content-Type", "application/json")

		ok := false
		if resp, reqErr := client.Do(req); reqErr == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var parsed struct {
				Success bool `json:"success"`
			}
			if json.Unmarshal(body, &parsed) == nil && parsed.Success {
				ok = true
			}
		}

		if ok {
			fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CF_VALIDATING"), ui.ColorReset)
			return apiKey, email, nil
		}

		fmt.Printf(ui.ColorRed+i18n.T("CF_INVALID_ATTEMPT")+ui.ColorReset+"\n", attempt, attempts)
		if attempt < attempts {
			apiKey = ui.Reading(i18n.T("ENTER_CF_TOKEN"))
			email = ui.Reading(i18n.T("ENTER_CF_EMAIL"))
		}
	}

	return apiKey, email, fmt.Errorf(i18n.T("CF_INVALID"), attempts)
}

// runCertbot is a small os/exec wrapper: certbot's own stdout/stderr are
// left attached to the process (matching bash's un-redirected certbot
// calls, which the user sees directly).
func runCertbot(args ...string) error {
	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Original bash (install_remnawave.sh:1507-1618): get_certificates().
func GetCertificates(domainName, certMethod, letsencryptEmail string) error {
	baseDomain := domain.ExtractDomain(domainName)
	wildcardDomain := "*." + baseDomain

	fmt.Printf(ui.ColorYellow+i18n.T("GENERATING_CERTS")+ui.ColorReset+"\n", domainName)

	switch certMethod {
	case "1": // Cloudflare DNS-01 (wildcard)
		apiKey := ui.Reading(i18n.T("ENTER_CF_TOKEN"))
		email := ui.Reading(i18n.T("ENTER_CF_EMAIL"))

		validKey, validEmail, err := CheckAPI(apiKey, email)
		if err != nil {
			return err
		}

		dir := secretsDir()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		iniPath := filepath.Join(dir, "cloudflare.ini")
		var ini string
		if hasUpperRE.MatchString(validKey) {
			ini = fmt.Sprintf("dns_cloudflare_api_token = %s\n", validKey)
		} else {
			ini = fmt.Sprintf("dns_cloudflare_email = %s\ndns_cloudflare_api_key = %s\n", validEmail, validKey)
		}
		if err := os.WriteFile(iniPath, []byte(ini), 0600); err != nil {
			return err
		}

		if err := runCertbot(
			"certonly",
			"--dns-cloudflare",
			"--dns-cloudflare-credentials", iniPath,
			"--dns-cloudflare-propagation-seconds", "60",
			"-d", baseDomain,
			"-d", wildcardDomain,
			"--email", validEmail,
			"--agree-tos",
			"--non-interactive",
			"--key-type", "ecdsa",
			"--elliptic-curve", "secp384r1",
		); err != nil {
			return err
		}

	case "2": // ACME HTTP-01 (no wildcard)
		_ = exec.Command("ufw", "allow", "80/tcp", "comment", "HTTP for ACME challenge").Run()

		certErr := runCertbot(
			"certonly",
			"--standalone",
			"-d", domainName,
			"--email", letsencryptEmail,
			"--agree-tos",
			"--non-interactive",
			"--http-01-port", "80",
			"--key-type", "ecdsa",
			"--elliptic-curve", "secp384r1",
		)

		_ = exec.Command("ufw", "delete", "allow", "80/tcp").Run()
		_ = exec.Command("ufw", "reload").Run()

		if certErr != nil {
			return certErr
		}

	case "3": // Gcore DNS-01 (wildcard)
		if !certbotHasPlugin("dns-gcore") {
			fmt.Printf("%sInstalling certbot-dns-gcore plugin...%s\n", ui.ColorYellow, ui.ColorReset)
			if err := installGcorePlugin(); err != nil {
				fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_GCORE_PLUGIN"), ui.ColorReset)
				return err
			}
			fmt.Printf("%sPlugin installed successfully.%s\n", ui.ColorGreen, ui.ColorReset)
		} else {
			fmt.Printf("%sGcore plugin already available.%s\n", ui.ColorGreen, ui.ColorReset)
		}

		gcoreAPIKey := ui.Reading(i18n.T("ENTER_GCORE_TOKEN"))

		dir := secretsDir()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		iniPath := filepath.Join(dir, "gcore.ini")
		if err := os.WriteFile(iniPath, []byte(fmt.Sprintf("dns_gcore_apitoken = %s\n", gcoreAPIKey)), 0600); err != nil {
			return err
		}

		if err := runCertbot(
			"certonly",
			"--authenticator", "dns-gcore",
			"--dns-gcore-credentials", iniPath,
			"--dns-gcore-propagation-seconds", "80",
			"-d", baseDomain,
			"-d", wildcardDomain,
			"--email", letsencryptEmail,
			"--agree-tos",
			"--non-interactive",
			"--key-type", "ecdsa",
			"--elliptic-curve", "secp384r1",
		); err != nil {
			return err
		}

	default:
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("INVALID_CERT_METHOD"), ui.ColorReset)
		return fmt.Errorf("invalid cert method")
	}

	if _, err := os.Stat(filepath.Join(letsencryptLive, domainName)); err != nil {
		fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("CERT_GENERATION_FAILED"), domainName, ui.ColorReset)
		return fmt.Errorf("certificate not found after issuance")
	}
	return nil
}

func certbotHasPlugin(name string) bool {
	out, err := exec.Command("certbot", "plugins").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

func installGcorePlugin() error {
	helpOut, _ := exec.Command("python3", "-m", "pip", "install", "--help").CombinedOutput()
	args := []string{"-m", "pip", "install"}
	if strings.Contains(string(helpOut), "break-system-packages") {
		args = append(args, "--break-system-packages")
	}
	args = append(args, "certbot-dns-gcore")

	if err := exec.Command("python3", args...).Run(); err != nil {
		return err
	}
	if !certbotHasPlugin("dns-gcore") {
		return fmt.Errorf("plugin not available after install")
	}
	return nil
}
