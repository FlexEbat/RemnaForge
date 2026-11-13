package domain

import (
	"net"
	"testing"
)

func TestExtractDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":          "example.com",
		"sub.example.com":      "example.com",
		"a.b.c.example.com":    "example.com",
		"sub.example.co.uk":    "example.co.uk",
		"example.co.uk":        "example.co.uk",
		"panel.example.com.au": "example.com.au",
		"localhost":            "localhost",
		"co.uk":                "co.uk",
	}
	for in, want := range cases {
		if got := ExtractDomain(in); got != want {
			t.Errorf("ExtractDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIPInRanges(t *testing.T) {
	ranges := []*net.IPNet{mustCIDR(t, "173.245.48.0/20"), mustCIDR(t, "104.16.0.0/13")}
	cases := []struct {
		ip   string
		want bool
	}{
		{"173.245.48.1", true},
		{"173.245.63.255", true},
		{"173.245.64.0", false},
		{"104.16.0.1", true},
		{"104.24.0.1", false},
		{"8.8.8.8", false},
		{"", false},
		{"not an ip", false},
		{"2606:4700::1", false},
	}
	for _, c := range cases {
		if got := ipInRanges(c.ip, ranges); got != c.want {
			t.Errorf("%q: got %v, want %v", c.ip, got, c.want)
		}
	}
	if ipInRanges("1.2.3.4", nil) {
		t.Error("an empty range list matched an address")
	}
}
