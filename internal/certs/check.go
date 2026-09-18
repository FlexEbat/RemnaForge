package certs

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const letsencryptLive = "/etc/letsencrypt/live"
const letsencryptArchive = "/etc/letsencrypt/archive"
const letsencryptRenewal = "/etc/letsencrypt/renewal"

// caddyCertDir is the default file-storage layout Caddy's built-in ACME
// client uses inside its own data volume, one subdirectory per issuer.
// This is Caddy's documented default (unchanged since Caddy 2 beta 17,
// 2020) for its Let's Encrypt production issuer specifically; a
// deployment that overrides Caddy's storage backend or issuer (this
// project's Caddyfiles don't) would need a different path.
const caddyCertDir = "/data/caddy/certificates/acme-v02.api.letsencrypt.org-directory"

// NginxCertPaths returns the fullchain/privkey path pair certbot uses
// for domain under /etc/letsencrypt/live, the layout every Nginx-based
// install flow in this project mounts into both the webserver container
// (as /etc/nginx/ssl/<domain>/...) and, for a config profile's
// Hysteria2 inbound, directly into the node container at this same
// path.
func NginxCertPaths(domain string) (fullchain, privkey string) {
	return letsencryptLive + "/" + domain + "/fullchain.pem",
		letsencryptLive + "/" + domain + "/privkey.pem"
}

// CaddyCertPaths returns the certificate/key path pair Caddy's built-in
// ACME client will have written for domain, once issued, inside the
// caddy_data volume every Caddy-based install flow in this project
// already shares with its webserver container. A config profile's
// Hysteria2 inbound reads these directly, same as NginxCertPaths for
// the Nginx flows, via a read-only mount of that same named volume into
// the node container.
func CaddyCertPaths(domain string) (cert, key string) {
	return caddyCertDir + "/" + domain + "/" + domain + ".crt",
		caddyCertDir + "/" + domain + "/" + domain + ".key"
}

// AskCertDomain resolves the on-disk certificate directory name for
// domainName, for a caller that didn't issue that certificate itself
// (internal/nodeprofile) and so has no way to know whether it's a
// per-domain certificate (the directory is domainName itself) or a
// wildcard certificate covering domainName's base domain (the
// directory is the base domain instead, via domain.ExtractDomain).
// Asks the operator directly rather than guessing, since guessing
// wrong means Xray's Hysteria2 inbound fails to find its certificate
// files on the actual node.
func AskCertDomain(domainName string) string {
	fmt.Println()
	answer := ui.Reading(fmt.Sprintf(i18n.T("ASK_WILDCARD_CERT"), domainName))
	if answer == "y" || answer == "Y" {
		return domain.ExtractDomain(domainName)
	}
	return domainName
}

// renewHookCommand is the certbot renew_hook line this project writes into
// every domain's renewal.conf, shared by FixLetsencryptStructure (this
// file) and fixRenewHook (handle.go).
//
// FIXED: these two call sites used to hardcode two *different*
// renew_hook strings for this exact same purpose - one included a
// trailing "docker compose exec remnawave-nginx nginx -s reload", the
// other didn't. Depending on which of the two ran last, a server could
// end up with either hook, silently. Both now share this single
// constant, using the more complete variant (the extra reload is a
// harmless no-op immediately after `docker compose up -d`, and a real
// safety net if that ever changes to not force a full container
// restart).
const renewHookCommand = `renew_hook = sh -c 'cd /opt/remnawave && docker compose down remnawave-nginx && docker compose up -d remnawave-nginx && docker compose exec remnawave-nginx nginx -s reload'`

// latestMatchingDir returns the highest natural-sort-ordered directory
// name under base matching prefix*, or "" if none exist.
func latestMatchingDir(base, prefix string) string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sortNatural(matches)
	return filepath.Join(base, matches[len(matches)-1])
}

func CheckCertificates(domainName string) bool {
	if _, err := os.Stat(letsencryptLive); err != nil {
		fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), domainName, ui.ColorReset)
		return false
	}

	liveDir := latestMatchingDir(letsencryptLive, domainName)
	if liveDir != "" {
		files := []string{"cert.pem", "chain.pem", "fullchain.pem", "privkey.pem"}
		ok := true
		for _, f := range files {
			filePath := filepath.Join(liveDir, f)
			info, err := os.Lstat(filePath)
			if err != nil {
				fmt.Printf("%s%s %s (missing %s)%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), domainName, f, ui.ColorReset)
				ok = false
				break
			}
			if info.Mode()&os.ModeSymlink == 0 {
				if err := FixLetsencryptStructure(filepath.Base(liveDir)); err != nil {
					fmt.Printf("%s%s %s (failed to fix structure)%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), domainName, ui.ColorReset)
					ok = false
					break
				}
			}
		}
		if ok {
			fmt.Printf("%s%s%s%s\n", ui.ColorGreen, i18n.T("CERT_FOUND"), filepath.Base(liveDir), ui.ColorReset)
			return true
		}
	}

	baseDomain := domain.ExtractDomain(domainName)
	if baseDomain != domainName {
		wildcardDir := latestMatchingDir(letsencryptLive, baseDomain)
		if wildcardDir != "" && domain.IsWildcardCert(baseDomain) {
			fmt.Printf("%s%s%s %s %s%s\n", ui.ColorGreen, i18n.T("WILDCARD_CERT_FOUND"), baseDomain, i18n.T("FOR_DOMAIN"), domainName, ui.ColorReset)
			return true
		}
	}

	fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), domainName, ui.ColorReset)
	return false
}

// Uses crypto/x509 directly instead of shelling to
// `openssl x509 -enddate` + `date -d`.
func CheckCertExpiry(domainName string) (int, error) {
	liveDir := latestMatchingDir(letsencryptLive, domainName)
	if liveDir == "" {
		return 0, fmt.Errorf("no cert dir for %s", domainName)
	}

	certFile := filepath.Join(liveDir, "fullchain.pem")
	data, err := os.ReadFile(certFile)
	if err != nil {
		return 0, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_PARSING_CERT"), ui.ColorReset)
		return 0, fmt.Errorf("no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_PARSING_CERT"), ui.ColorReset)
		return 0, err
	}

	daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
	return daysLeft, nil
}

var certVersionRE = regexp.MustCompile(`cert(\d+)\.pem$`)

func FixLetsencryptStructure(domainName string) error {
	liveDir := filepath.Join(letsencryptLive, domainName)
	archiveDir := filepath.Join(letsencryptArchive, domainName)
	renewalConf := filepath.Join(letsencryptRenewal, domainName+".conf")

	if _, err := os.Stat(liveDir); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_NOT_FOUND"), ui.ColorReset)
		return err
	}
	if _, err := os.Stat(archiveDir); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ARCHIVE_NOT_FOUND"), ui.ColorReset)
		return err
	}
	renewalData, err := os.ReadFile(renewalConf)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("RENEWAL_CONF_NOT_FOUND"), ui.ColorReset)
		return err
	}

	confArchiveDir := renewalConfValue(string(renewalData), "archive_dir")
	if confArchiveDir != archiveDir {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ARCHIVE_DIR_MISMATCH"), ui.ColorReset)
		return fmt.Errorf("archive_dir mismatch")
	}

	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return err
	}
	latestVersion := ""
	for _, e := range entries {
		if m := certVersionRE.FindStringSubmatch(e.Name()); m != nil {
			if latestVersion == "" || natLess(latestVersion, m[1]) {
				latestVersion = m[1]
			}
		}
	}
	if latestVersion == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CERT_VERSION_NOT_FOUND"), ui.ColorReset)
		return fmt.Errorf("no cert version found")
	}

	files := []string{"cert", "chain", "fullchain", "privkey"}
	for _, f := range files {
		archiveFile := filepath.Join(archiveDir, fmt.Sprintf("%s%s.pem", f, latestVersion))
		liveFile := filepath.Join(liveDir, f+".pem")

		if _, err := os.Stat(archiveFile); err != nil {
			fmt.Printf("%s%s %s%s\n", ui.ColorRed, i18n.T("FILE_NOT_FOUND"), archiveFile, ui.ColorReset)
			return err
		}
		if info, err := os.Lstat(liveFile); err == nil && info.Mode()&os.ModeSymlink == 0 {
			_ = os.Remove(liveFile)
		}
		_ = os.Remove(liveFile)
		if err := os.Symlink(archiveFile, liveFile); err != nil {
			return err
		}
	}

	certPath := filepath.Join(liveDir, "cert.pem")
	chainPath := filepath.Join(liveDir, "chain.pem")
	fullchainPath := filepath.Join(liveDir, "fullchain.pem")
	privkeyPath := filepath.Join(liveDir, "privkey.pem")

	conf := string(renewalData)
	conf = setRenewalConfLine(conf, "cert", certPath)
	conf = setRenewalConfLine(conf, "chain", chainPath)
	conf = setRenewalConfLine(conf, "fullchain", fullchainPath)
	conf = setRenewalConfLine(conf, "privkey", privkeyPath)

	expectedHook := renewHookCommand
	conf = removeLinesWithPrefix(conf, "renew_hook")
	conf = strings.TrimRight(conf, "\n") + "\n" + expectedHook + "\n"

	if err := os.WriteFile(renewalConf, []byte(conf), 0644); err != nil {
		return err
	}

	_ = os.Chmod(certPath, 0644)
	_ = os.Chmod(chainPath, 0644)
	_ = os.Chmod(fullchainPath, 0644)
	_ = os.Chmod(privkeyPath, 0600)

	return nil
}

// renewalConfValue reads the value of a "key = value" line from a
// renewal.conf-style config.
func renewalConfValue(conf, key string) string {
	for _, line := range strings.Split(conf, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// setRenewalConfLine sets a "key = value" line in conf, adding it if
// not already present with that exact value.
func setRenewalConfLine(conf, key, value string) string {
	target := fmt.Sprintf("%s = %s", key, value)
	lines := strings.Split(conf, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+" =") || strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			if strings.TrimSpace(line) == target {
				found = true
			} else {
				lines[i] = target
				found = true
			}
		}
	}
	if !found {
		lines = append(lines, target)
	}
	return strings.Join(lines, "\n")
}

func removeLinesWithPrefix(conf, prefix string) string {
	lines := strings.Split(conf, "\n")
	var kept []string
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
