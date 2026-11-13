package domain

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	ok := []string{"example.com", "sub.example.com", "a-b.example.co.uk", "xn--80ak6aa92e.com", "EXAMPLE.com", "example.com.", "1.example.com", "a1.b2.c3.io"}
	for _, n := range ok {
		if err := ValidateName(n); err != nil {
			t.Errorf("%q rejected: %v", n, err)
		}
	}
	long := strings.Repeat("a", 64) + ".com"
	cases := map[string]error{
		"":                                ErrNameEmpty,
		"localhost":                       ErrNameLabels,
		"*.example.com":                   ErrNameWildcard,
		"192.168.1.1":                     ErrNameIP,
		"2001:db8::1":                     ErrNameIP,
		"-a.example.com":                  ErrNameHyphen,
		"a-.example.com":                  ErrNameHyphen,
		"exa mple.com":                    ErrNameChars,
		"exa_mple.com":                    ErrNameChars,
		"пример.рф":                       ErrNameChars,
		"https://example.com":             ErrNameChars,
		"example.com/path":                ErrNameChars,
		"example.com:443":                 ErrNameChars,
		"a..example.com":                  ErrNameLabelLen,
		".example.com":                    ErrNameLabelLen,
		long:                              ErrNameLabelLen,
		"example.123":                     ErrNameTLD,
		strings.Repeat("a.", 130) + "com": ErrNameTooLong,
	}
	for n, want := range cases {
		if got := ValidateName(n); !errors.Is(got, want) {
			t.Errorf("%q: got %v, want %v", n, got, want)
		}
	}
}

func TestEveryNameErrorHasAMessage(t *testing.T) {
	for _, e := range []error{ErrNameEmpty, ErrNameTooLong, ErrNameLabels, ErrNameChars, ErrNameHyphen, ErrNameLabelLen, ErrNameIP, ErrNameWildcard, ErrNameTLD} {
		if k := nameErrorKey(e); k == "DOMAIN_ERR_CHARS" && e != ErrNameChars {
			t.Errorf("%v falls back to the generic message", e)
		}
	}
}

func cidrs(t *testing.T, list ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, c := range list {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

// fakeDNS builds an Env over fixed records and fixed server addresses.
func fakeDNS(records map[string][]string, v4, v6 string) Env {
	return Env{
		LookupIP: func(host string) ([]net.IP, error) {
			var ips []net.IP
			for _, a := range records[host] {
				ips = append(ips, net.ParseIP(a))
			}
			if len(ips) == 0 {
				return nil, errors.New("no such host")
			}
			return ips, nil
		},
		ServerIPv4: func() string { return v4 },
		ServerIPv6: func() string { return v6 },
		CloudflareRanges: func() []*net.IPNet {
			return []*net.IPNet{
				mustCIDRNet("104.16.0.0/13"), mustCIDRNet("172.64.0.0/13"), mustCIDRNet("2606:4700::/32"),
			}
		},
	}
}

func mustCIDRNet(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func issues(r Report) []Issue { return r.Issues }

func TestAnalyzeAligned(t *testing.T) {
	env := fakeDNS(map[string][]string{"example.com": {"203.0.113.10", "2001:db8::10"}}, "203.0.113.10", "2001:db8::10")
	r := Analyze("example.com", env)
	if len(r.Issues) != 0 {
		t.Fatalf("issues = %v", r.Issues)
	}
	if len(r.A) != 1 || len(r.AAAA) != 1 {
		t.Errorf("A=%v AAAA=%v", r.A, r.AAAA)
	}
}

func TestAnalyzeIPv6Notation(t *testing.T) {
	// The same address written two ways is not a mismatch.
	env := fakeDNS(map[string][]string{"example.com": {"203.0.113.10", "2001:0db8:0000:0000:0000:0000:0000:0010"}}, "203.0.113.10", "2001:db8::10")
	if r := Analyze("example.com", env); len(r.Issues) != 0 {
		t.Errorf("issues = %v", r.Issues)
	}
}

func TestAnalyzeAAAAMismatch(t *testing.T) {
	env := fakeDNS(map[string][]string{"example.com": {"203.0.113.10", "2001:db8::bad"}}, "203.0.113.10", "2001:db8::10")
	r := Analyze("example.com", env)
	if !r.Has(IssueAAAAMismatch) || r.Has(IssueAMismatch) || len(r.Issues) != 1 {
		t.Errorf("issues = %v", r.Issues)
	}
}

func TestAnalyzeAAAAWithoutServerIPv6(t *testing.T) {
	env := fakeDNS(map[string][]string{"example.com": {"203.0.113.10", "2001:db8::10"}}, "203.0.113.10", "")
	r := Analyze("example.com", env)
	if !r.Has(IssueAAAANoServerV6) || len(r.Issues) != 1 {
		t.Errorf("issues = %v", r.Issues)
	}
}

func TestAnalyzeAMismatchAndMissingA(t *testing.T) {
	env := fakeDNS(map[string][]string{"example.com": {"198.51.100.7"}, "v6only.example.com": {"2001:db8::10"}}, "203.0.113.10", "2001:db8::10")
	if r := Analyze("example.com", env); !r.Has(IssueAMismatch) || len(r.Issues) != 1 {
		t.Errorf("A mismatch: %v", r.Issues)
	}
	if r := Analyze("v6only.example.com", env); !r.Has(IssueNoA) || len(r.Issues) != 1 {
		t.Errorf("missing A: %v", r.Issues)
	}
	// An A record among several that matches is enough.
	env = fakeDNS(map[string][]string{"example.com": {"198.51.100.7", "203.0.113.10"}}, "203.0.113.10", "")
	if r := Analyze("example.com", env); len(r.Issues) != 0 {
		t.Errorf("round robin with this server: %v", r.Issues)
	}
}

func TestAnalyzeCloudflareProxied(t *testing.T) {
	env := fakeDNS(map[string][]string{"proxied.example.com": {"104.18.5.5", "2606:4700::6810:505"}}, "203.0.113.10", "2001:db8::10")
	r := Analyze("proxied.example.com", env)
	if !r.Has(IssueCloudflare) {
		t.Fatalf("issues = %v", r.Issues)
	}
	// A proxied record is not also reported as a mismatch.
	if r.Has(IssueAMismatch) || r.Has(IssueAAAAMismatch) || r.Has(IssueNoA) || r.Has(IssueAAAANoServerV6) {
		t.Errorf("proxy reported as mismatch too: %v", r.Issues)
	}
	// Only the A record proxied, AAAA pointing at a wrong host: both show.
	env = fakeDNS(map[string][]string{"half.example.com": {"104.18.5.5", "2001:db8::bad"}}, "203.0.113.10", "2001:db8::10")
	r = Analyze("half.example.com", env)
	if !r.Has(IssueCloudflare) || !r.Has(IssueAAAAMismatch) {
		t.Errorf("issues = %v", r.Issues)
	}
}

func TestAnalyzeNoRecordsAndUnknownServer(t *testing.T) {
	env := fakeDNS(nil, "203.0.113.10", "")
	if r := Analyze("nothing.example.com", env); !r.Has(IssueNoRecords) || len(r.Issues) != 1 {
		t.Errorf("issues = %v", r.Issues)
	}
	env = fakeDNS(map[string][]string{"example.com": {"203.0.113.10"}}, "", "")
	if r := Analyze("example.com", env); !r.Has(IssueNoRecords) {
		t.Errorf("an unknown server address must not look aligned: %v", r.Issues)
	}
}

func TestAnalyzeInvalidNameSkipsLookups(t *testing.T) {
	env := fakeDNS(nil, "1.2.3.4", "")
	env.LookupIP = func(string) ([]net.IP, error) { t.Fatal("DNS queried for an invalid name"); return nil, nil }
	r := Analyze("https://example.com", env)
	if !r.Has(IssueInvalidName) || r.NameErr == nil {
		t.Errorf("report = %+v", r)
	}
}

func TestCheckWithResults(t *testing.T) {
	never := func(string) string { t.Fatal("asked although nothing is wrong"); return "" }
	aligned := fakeDNS(map[string][]string{"example.com": {"203.0.113.10"}}, "203.0.113.10", "")
	if got := checkWith(aligned, "example.com", true, true, never); got != CheckOK {
		t.Errorf("aligned: %v", got)
	}
	bad := fakeDNS(map[string][]string{"example.com": {"198.51.100.7"}}, "203.0.113.10", "")
	if got := checkWith(bad, "example.com", true, true, func(string) string { return "y" }); got != CheckMismatch {
		t.Errorf("accepted mismatch: %v", got)
	}
	if got := checkWith(bad, "example.com", true, true, func(string) string { return "n" }); got != CheckAbort {
		t.Errorf("declined mismatch: %v", got)
	}
	if got := checkWith(bad, "example.com", false, true, never); got != CheckMismatch {
		t.Errorf("silent mismatch: %v", got)
	}
	if got := checkWith(bad, "bad name", true, true, never); got != CheckAbort {
		t.Errorf("invalid name: %v", got)
	}
}

func TestCheckWithCloudflarePolicy(t *testing.T) {
	proxied := fakeDNS(map[string][]string{"example.com": {"104.18.5.5"}}, "203.0.113.10", "")
	never := func(string) string { t.Fatal("asked although a proxy is allowed"); return "" }
	if got := checkWith(proxied, "example.com", true, true, never); got != CheckOK {
		t.Errorf("proxy allowed: %v", got)
	}
	if got := checkWith(proxied, "example.com", true, false, func(string) string { return "n" }); got != CheckAbort {
		t.Errorf("proxy forbidden, declined: %v", got)
	}
	if got := checkWith(proxied, "example.com", true, false, func(string) string { return "Y" }); got != CheckMismatch {
		t.Errorf("proxy forbidden, accepted: %v", got)
	}
}

func TestCheckWithAAAAMismatchAsks(t *testing.T) {
	env := fakeDNS(map[string][]string{"example.com": {"203.0.113.10", "2001:db8::bad"}}, "203.0.113.10", "2001:db8::10")
	asked := 0
	if got := checkWith(env, "example.com", true, true, func(string) string { asked++; return "n" }); got != CheckAbort || asked != 1 {
		t.Errorf("got %v after %d questions", got, asked)
	}
}

func TestParseCIDRList(t *testing.T) {
	got := ParseCIDRList("173.245.48.0/20\n\n  2400:cb00::/32 \nnot a cidr\n103.21.244.0/22")
	if len(got) != 3 {
		t.Fatalf("got %d ranges", len(got))
	}
	if !ipInRanges("2400:cb00::1", got) || !ipInRanges("173.245.50.1", got) || ipInRanges("8.8.8.8", got) {
		t.Error("range matching broke")
	}
}
