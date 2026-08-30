// Package domain holds shared domain/certificate-related helpers used
// by every install_node/install_panel variant: extracting a domain's
// registrable base, checking that a domain resolves to this server (or
// flagging a Cloudflare proxy IP instead), and detecting a wildcard
// certificate's base domain.
//
// A few things are done with native Go rather than shelling out to a
// CLI tool, since Go's standard library covers them directly:
//   - DNS lookups use net.LookupIP instead of `dig +short A`.
//   - Public-IP detection uses net/http instead of `curl -4 ifconfig.me`.
//   - Cloudflare IP-range matching uses net.ParseCIDR instead of manual
//     bit-shift math.
//   - Certificate parsing uses crypto/x509 + encoding/pem instead of
//     `openssl x509 -noout -text | grep`.
package domain

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
	"golang.org/x/net/publicsuffix"
)

// ExtractDomain returns a subdomain's registrable base domain (its
// "effective TLD + 1"), used to detect whether a wildcard certificate
// covers the given host. This uses the real Public Suffix List
// algorithm (golang.org/x/net/publicsuffix) rather than naively taking
// the last two dot-separated labels, which is wrong for any domain
// under a multi-label public suffix: "sub.example.co.uk" would
// naively reduce to "co.uk" instead of "example.co.uk", breaking
// wildcard-certificate base-domain detection for such domains.
func ExtractDomain(subdomain string) string {
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(subdomain); err == nil {
		return etld1
	}
	// Fall back to the naive last-two-labels behavior only if the PSL
	// lookup itself fails, e.g. for a single-label or otherwise
	// malformed input. EffectiveTLDPlusOne errors on those rather than
	// silently guessing.
	parts := strings.Split(subdomain, ".")
	if len(parts) > 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1]
	}
	return subdomain
}

// CheckResult mirrors the three bash return codes from check_domain():
// 0 = domain resolves straight to this server, 1 = mismatch/unresolvable
// but proceeding anyway, 2 = user aborted.
type CheckResult int

const (
	CheckOK CheckResult = iota
	CheckMismatch
	CheckAbort
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// resolveA is the Go equivalent of: dig +short A "$domain" | grep -E ... | head -n 1
func resolveA(domain string) string {
	ips, err := net.LookupIP(domain)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

// publicServerIP tries a small list of public IP-echo services in
// order, returning the first one that answers.
func publicServerIP() string {
	for _, url := range []string{"https://ifconfig.me", "https://api.ipify.org", "https://ipinfo.io/ip"} {
		resp, err := httpClient.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// cloudflareRanges is the Go equivalent of: curl -s https://www.cloudflare.com/ips-v4
func cloudflareRanges() []*net.IPNet {
	resp, err := httpClient.Get("https://www.cloudflare.com/ips-v4")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var nets []*net.IPNet
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(line)
		if err == nil {
			nets = append(nets, ipnet)
		}
	}
	return nets
}

func ipInRanges(ipStr string, ranges []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range ranges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// showWarning/allowCFProxy default to true, matching bash's ${2:-true}/${3:-true}.
func CheckDomain(domainName string, showWarning, allowCFProxy bool) CheckResult {
	domainIP := resolveA(domainName)
	serverIP := publicServerIP()

	if domainIP == "" || serverIP == "" {
		if showWarning {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_LABEL"), ui.ColorReset)
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CHECK_DOMAIN_IP_FAIL"), ui.ColorReset)
			fmt.Printf(ui.ColorYellow+i18n.T("CHECK_DOMAIN_IP_FAIL_INSTRUCTION")+ui.ColorReset+"\n", domainName, serverIP)
			confirm := ui.Reading(i18n.T("CONFIRM_PROMPT"))
			if confirm != "y" && confirm != "Y" {
				return CheckAbort
			}
		}
		return CheckMismatch
	}

	cfRanges := cloudflareRanges()
	ipInCF := ipInRanges(domainIP, cfRanges)

	switch {
	case domainIP == serverIP:
		return CheckOK

	case ipInCF:
		if allowCFProxy {
			return CheckOK
		}
		if showWarning {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_LABEL"), ui.ColorReset)
			fmt.Printf(ui.ColorRed+i18n.T("CHECK_DOMAIN_CLOUDFLARE")+ui.ColorReset+"\n", domainName, domainIP)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_DOMAIN_CLOUDFLARE_INSTRUCTION"), ui.ColorReset)
			confirm := ui.Reading(i18n.T("CONFIRM_PROMPT"))
			if confirm == "y" || confirm == "Y" {
				return CheckMismatch
			}
			return CheckAbort
		}
		return CheckMismatch

	default:
		if showWarning {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_LABEL"), ui.ColorReset)
			fmt.Printf(ui.ColorRed+i18n.T("CHECK_DOMAIN_MISMATCH")+ui.ColorReset+"\n", domainName, domainIP, serverIP)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_DOMAIN_MISMATCH_INSTRUCTION"), ui.ColorReset)
			confirm := ui.Reading(i18n.T("CONFIRM_PROMPT"))
			if confirm == "y" || confirm == "Y" {
				return CheckMismatch
			}
			return CheckAbort
		}
		return CheckMismatch
	}
}

// IsWildcardCert reports whether domainName's live certificate covers
// "*.domainName" as one of its names.
func IsWildcardCert(domainName string) bool {
	certPath := "/etc/letsencrypt/live/" + domainName + "/fullchain.pem"

	data, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}

	wildcard := "*." + domainName
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return false
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			if cert.Subject.CommonName == wildcard {
				return true
			}
			for _, name := range cert.DNSNames {
				if name == wildcard {
					return true
				}
			}
		}
		if len(data) == 0 {
			break
		}
	}
	return false
}
