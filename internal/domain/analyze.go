package domain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Name validation errors. Each one maps to a message in the i18n tables.
var (
	ErrNameEmpty    = errors.New("domain name is empty")
	ErrNameTooLong  = errors.New("domain name is longer than 253 characters")
	ErrNameLabels   = errors.New("domain name needs at least two labels")
	ErrNameChars    = errors.New("domain name has characters outside a-z, 0-9 and hyphen")
	ErrNameHyphen   = errors.New("a label starts or ends with a hyphen")
	ErrNameLabelLen = errors.New("a label is empty or longer than 63 characters")
	ErrNameIP       = errors.New("an IP address is not a domain name")
	ErrNameWildcard = errors.New("a wildcard is not a domain name")
	ErrNameTLD      = errors.New("the last label cannot be only digits")
)

// ValidateName checks that name is a host name a certificate can be issued
// for: ASCII letters, digits and hyphens in dot-separated labels of 1 to 63
// characters, at most 253 characters in total, at least two labels. A
// trailing dot is accepted. Internationalized names must be given in their
// punycode form.
func ValidateName(name string) error {
	name = strings.TrimSuffix(name, ".")
	switch {
	case name == "":
		return ErrNameEmpty
	case strings.Contains(name, "*"):
		return ErrNameWildcard
	case net.ParseIP(name) != nil:
		return ErrNameIP
	case len(name) > 253:
		return ErrNameTooLong
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return ErrNameLabels
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return ErrNameLabelLen
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return ErrNameHyphen
		}
		for _, r := range l {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-'
			if !ok {
				return ErrNameChars
			}
		}
	}
	allDigits := true
	for _, r := range labels[len(labels)-1] {
		if r < '0' || r > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return ErrNameTLD
	}
	return nil
}

func nameErrorKey(err error) string {
	switch err {
	case ErrNameEmpty:
		return "DOMAIN_ERR_EMPTY"
	case ErrNameTooLong:
		return "DOMAIN_ERR_TOO_LONG"
	case ErrNameLabels:
		return "DOMAIN_ERR_LABELS"
	case ErrNameChars:
		return "DOMAIN_ERR_CHARS"
	case ErrNameHyphen:
		return "DOMAIN_ERR_HYPHEN"
	case ErrNameLabelLen:
		return "DOMAIN_ERR_LABEL_LEN"
	case ErrNameIP:
		return "DOMAIN_ERR_IP"
	case ErrNameWildcard:
		return "DOMAIN_ERR_WILDCARD"
	case ErrNameTLD:
		return "DOMAIN_ERR_TLD"
	}
	return "DOMAIN_ERR_CHARS"
}

// Issue is one problem the analysis of a domain found.
type Issue int

const (
	// IssueInvalidName: the name cannot be a certificate subject.
	IssueInvalidName Issue = iota
	// IssueNoRecords: the name resolves to nothing, or the server's own
	// public address could not be found, so nothing can be compared.
	IssueNoRecords
	// IssueNoA: the server has a public IPv4 address and the name has no
	// A record.
	IssueNoA
	// IssueAMismatch: no A record points at this server.
	IssueAMismatch
	// IssueAAAAMismatch: an AAAA record exists and none points at this
	// server's IPv6 address.
	IssueAAAAMismatch
	// IssueAAAANoServerV6: an AAAA record exists but the server has no
	// public IPv6 address. Let's Encrypt validates over IPv6 first, so the
	// challenge would fail.
	IssueAAAANoServerV6
	// IssueCloudflare: a record points into Cloudflare's proxy ranges.
	IssueCloudflare
)

// Report is the analysis of one domain against this server.
type Report struct {
	Name     string
	NameErr  error
	A, AAAA  []string
	ServerV4 string
	ServerV6 string
	Issues   []Issue
}

// Has reports whether the report carries the issue.
func (r Report) Has(i Issue) bool {
	for _, x := range r.Issues {
		if x == i {
			return true
		}
	}
	return false
}

// Env holds every outside source Analyze needs. DefaultEnv reads the real
// DNS and the public internet.
type Env struct {
	LookupIP         func(host string) ([]net.IP, error)
	ServerIPv4       func() string
	ServerIPv6       func() string
	CloudflareRanges func() []*net.IPNet // IPv4 and IPv6 ranges together
}

// DefaultEnv returns the Env of the running host.
func DefaultEnv() Env {
	return Env{
		LookupIP: net.LookupIP,
		ServerIPv4: func() string {
			return publicIP("tcp4", false, "https://api.ipify.org", "https://ifconfig.me", "https://ipinfo.io/ip")
		},
		ServerIPv6:       func() string { return publicIP("tcp6", true, "https://api6.ipify.org", "https://ifconfig.me") },
		CloudflareRanges: cloudflareRanges,
	}
}

// Analyze compares the DNS records of name with the public addresses of this
// server. Nothing is printed and nothing is asked.
func Analyze(name string, env Env) Report {
	r := Report{Name: name}
	if err := ValidateName(name); err != nil {
		r.NameErr = err
		r.Issues = []Issue{IssueInvalidName}
		return r
	}
	ips, _ := env.LookupIP(strings.TrimSuffix(name, "."))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			r.A = append(r.A, v4.String())
		} else {
			r.AAAA = append(r.AAAA, ip.String())
		}
	}
	r.ServerV4 = env.ServerIPv4()
	r.ServerV6 = env.ServerIPv6()

	if len(ips) == 0 || (r.ServerV4 == "" && r.ServerV6 == "") {
		r.Issues = []Issue{IssueNoRecords}
		return r
	}

	cf := env.CloudflareRanges()
	proxied := func(addrs []string) bool {
		for _, a := range addrs {
			if ipInRanges(a, cf) {
				return true
			}
		}
		return false
	}
	proxiedA, proxiedAAAA := proxied(r.A), proxied(r.AAAA)
	if proxiedA || proxiedAAAA {
		r.Issues = append(r.Issues, IssueCloudflare)
	}

	if !proxiedA {
		switch {
		case len(r.A) == 0 && r.ServerV4 != "":
			r.Issues = append(r.Issues, IssueNoA)
		case len(r.A) > 0 && r.ServerV4 != "" && !containsIP(r.A, r.ServerV4):
			r.Issues = append(r.Issues, IssueAMismatch)
		}
	}
	if !proxiedAAAA && len(r.AAAA) > 0 {
		switch {
		case r.ServerV6 == "":
			r.Issues = append(r.Issues, IssueAAAANoServerV6)
		case !containsIP(r.AAAA, r.ServerV6):
			r.Issues = append(r.Issues, IssueAAAAMismatch)
		}
	}
	return r
}

func containsIP(addrs []string, want string) bool {
	w := net.ParseIP(want)
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && w != nil && ip.Equal(w) {
			return true
		}
	}
	return false
}

// publicIP asks echo services for the address this server shows to the
// internet over the given network ("tcp4" or "tcp6"), so the answer has the
// wanted family. A dual-stack host would otherwise get whichever one the
// service sees first.
func publicIP(network string, wantV6 bool, urls ...string) string {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(string(body)))
		if ip == nil {
			continue
		}
		if (ip.To4() == nil) == wantV6 {
			return ip.String()
		}
	}
	return ""
}

var cloudflareClient = &http.Client{Timeout: 10 * time.Second}

// cloudflareRanges fetches Cloudflare's published IPv4 and IPv6 ranges.
func cloudflareRanges() []*net.IPNet {
	var nets []*net.IPNet
	for _, url := range []string{"https://www.cloudflare.com/ips-v4", "https://www.cloudflare.com/ips-v6"} {
		resp, err := cloudflareClient.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		nets = append(nets, ParseCIDRList(string(body))...)
	}
	return nets
}

// ParseCIDRList reads one CIDR per line and skips everything else.
func ParseCIDRList(text string) []*net.IPNet {
	var nets []*net.IPNet
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(line); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// CheckDomain analyzes domainName and, when showWarning is set, explains each
// problem and asks whether to go on. allowCFProxy says whether a Cloudflare
// proxy address in front of the domain is acceptable. It returns CheckOK for
// a clean domain, CheckMismatch when the operator goes on despite a problem
// (or when showWarning is off), and CheckAbort when the operator stops.
func CheckDomain(domainName string, showWarning, allowCFProxy bool) CheckResult {
	return checkWith(DefaultEnv(), domainName, showWarning, allowCFProxy, ui.Reading)
}

func checkWith(env Env, domainName string, showWarning, allowCFProxy bool, ask func(string) string) CheckResult {
	rep := Analyze(domainName, env)
	if rep.Has(IssueInvalidName) {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("DOMAIN_INVALID"), domainName, i18n.T(nameErrorKey(rep.NameErr))), ui.ColorReset)
		return CheckAbort
	}
	var blocks [][2]string // message, instruction
	for _, issue := range rep.Issues {
		switch issue {
		case IssueNoRecords:
			blocks = append(blocks, [2]string{i18n.T("CHECK_DOMAIN_IP_FAIL"),
				fmt.Sprintf(i18n.T("CHECK_DOMAIN_IP_FAIL_INSTRUCTION"), domainName, firstNonEmpty(rep.ServerV4, rep.ServerV6))})
		case IssueNoA:
			blocks = append(blocks, [2]string{fmt.Sprintf(i18n.T("CHECK_DOMAIN_NO_A"), domainName, rep.ServerV4), i18n.T("CHECK_DOMAIN_MISMATCH_INSTRUCTION")})
		case IssueAMismatch:
			blocks = append(blocks, [2]string{fmt.Sprintf(i18n.T("CHECK_DOMAIN_MISMATCH"), domainName, rep.A[0], rep.ServerV4), i18n.T("CHECK_DOMAIN_MISMATCH_INSTRUCTION")})
		case IssueAAAAMismatch:
			blocks = append(blocks, [2]string{fmt.Sprintf(i18n.T("CHECK_DOMAIN_AAAA_MISMATCH"), domainName, rep.AAAA[0], rep.ServerV6), i18n.T("CHECK_DOMAIN_AAAA_INSTRUCTION")})
		case IssueAAAANoServerV6:
			blocks = append(blocks, [2]string{fmt.Sprintf(i18n.T("CHECK_DOMAIN_AAAA_NO_SERVER"), domainName, rep.AAAA[0]), i18n.T("CHECK_DOMAIN_AAAA_INSTRUCTION")})
		case IssueCloudflare:
			if !allowCFProxy {
				cfAddr := firstNonEmpty(first(rep.A), first(rep.AAAA))
				blocks = append(blocks, [2]string{fmt.Sprintf(i18n.T("CHECK_DOMAIN_CLOUDFLARE"), domainName, cfAddr), i18n.T("CHECK_DOMAIN_CLOUDFLARE_INSTRUCTION")})
			}
		}
	}
	if len(blocks) == 0 {
		return CheckOK
	}
	if !showWarning {
		return CheckMismatch
	}
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("WARNING_LABEL"), ui.ColorReset)
	for _, b := range blocks {
		fmt.Printf("%s%s%s\n", ui.ColorRed, b[0], ui.ColorReset)
		fmt.Printf("%s%s%s\n", ui.ColorYellow, b[1], ui.ColorReset)
	}
	if answer := ask(i18n.T("CONFIRM_PROMPT")); answer == "y" || answer == "Y" {
		return CheckMismatch
	}
	return CheckAbort
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
