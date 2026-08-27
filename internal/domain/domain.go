// Package domain is a port of three shared domain/certificate-related
// helper functions defined at the top level of install_remnawave.sh and
// used by every install_node/install_panel variant:
//
//   - extract_domain()   (install_remnawave.sh:1328-1331)
//   - check_domain()     (install_remnawave.sh:1333-1421)
//   - is_wildcard_cert() (install_remnawave.sh:1423-1436)
//
// Several bash tool calls are replaced with native Go equivalents rather
// than shelled out to, per project decision to improve where it's easy:
//   - `dig +short A` -> net.LookupIP
//   - `curl -s -4 ifconfig.me` (etc.) -> net/http
//   - manual bit-shift CIDR math for the Cloudflare ranges -> net.ParseCIDR
//   - `openssl x509 -noout -text | grep` -> crypto/x509 + encoding/pem
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

// Original bash (install_remnawave.sh:1328-1331):
//
//	extract_domain() {
//	    local SUBDOMAIN=$1
//	    echo "$SUBDOMAIN" | awk -F'.' '{if (NF > 2) {print $(NF-1)"."$NF} else {print $0}}'
//	}
//
// BUG FIX (not a 1:1 port): the original always takes the last two
// dot-separated labels, wrong for any domain under a multi-label public
// suffix. "sub.example.co.uk" reduces to "co.uk" instead of
// "example.co.uk". This breaks wildcard-certificate base-domain
// detection for such domains, in both the original bash and (until now)
// this port. Fixed here using the real Public Suffix List algorithm
// (golang.org/x/net/publicsuffix) instead of perpetuating the bug.
func ExtractDomain(subdomain string) string {
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(subdomain); err == nil {
		return etld1
	}
	// Fall back to the original's naive behavior only if the PSL lookup
	// itself fails, e.g. for a single-label or otherwise malformed input.
	// EffectiveTLDPlusOne errors on those rather than silently guessing.
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

// publicServerIP is the Go equivalent of:
//
//	curl -s -4 ifconfig.me || curl -s -4 api.ipify.org || curl -s -4 ipinfo.io/ip
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

// Original bash (install_remnawave.sh:1333-1421): check_domain().
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

// Original bash (install_remnawave.sh:1423-1436):
//
//	is_wildcard_cert() {
//	    local domain=$1
//	    local cert_path="/etc/letsencrypt/live/$domain/fullchain.pem"
//	    if [ ! -f "$cert_path" ]; then
//	        return 1
//	    fi
//	    if openssl x509 -noout -text -in "$cert_path" | grep -q "\*\.$domain"; then
//	        return 0
//	    else
//	        return 1
//	    fi
//	}
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
