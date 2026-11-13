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
	"net"
	"os"
	"strings"

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

// CheckResult is the outcome of a domain check:
// 0 = domain resolves straight to this server, 1 = mismatch/unresolvable
// but proceeding anyway, 2 = user aborted.
type CheckResult int

const (
	CheckOK CheckResult = iota
	CheckMismatch
	CheckAbort
)

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
