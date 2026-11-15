package domain

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
)

func TestCheckPolicyRejects(t *testing.T) {
	cases := map[string]string{
		"vpn.example.com":      "vpn",
		"node.example.com":     "node",
		"panel.example.com":    "panel",
		"proxy.example.com":    "proxy",
		"admin.example.com":    "admin",
		"xray.example.com":     "xray",
		"vless.example.com":    "vless",
		"remna.example.com":    "remna",
		"VPN.Example.com":      "vpn",
		"nl.example.com":       "nl",
		"de.example.com":       "de",
		"us.example.com":       "us",
		"uk.example.com":       "uk",
		"eu.example.com":       "eu",
		"nl1.example.com":      "nl",
		"de-node.example.com":  "de",
		"node2.example.com":    "node",
		"vpn01.example.com":    "vpn",
		"my-stats.example.com": "my",
		"vpn.example.com.":     "vpn",
	}
	for name, part := range cases {
		err := CheckPolicy(name)
		var pe *PolicyError
		if !errors.As(err, &pe) || !errors.Is(err, ErrPolicy) {
			t.Errorf("%q: got %v, want a policy error", name, err)
			continue
		}
		if pe.Part != part {
			t.Errorf("%q: part %q, want %q", name, pe.Part, part)
		}
	}
}

func TestCheckPolicyAccepts(t *testing.T) {
	for _, name := range []string{
		"stats.example.com", "metrics.example.com", "feed.example.com",
		"cdn.example.com", "nodes.example.com", "usa.example.com",
		"nlx.example.com", "a1b.example.com", "stats.vpn.example.com",
		"example.com",
	} {
		if err := CheckPolicy(name); err != nil {
			t.Errorf("%q: unexpected %v", name, err)
		}
	}
}

// Only the first label counts: the base domain belongs to the operator.
func TestCheckPolicyIgnoresLaterLabels(t *testing.T) {
	for _, name := range []string{"stats.vpn.example.com", "stats.de.example.com", "cdn.node.net"} {
		if err := CheckPolicy(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

// Invalid syntax is ValidateName's business, so CheckDomain still reports it.
func TestCheckPolicySkipsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "vpn", "vpn_x.example.com", "*.example.com", "10.0.0.1"} {
		if err := CheckPolicy(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestCountryCodeCount(t *testing.T) {
	if got := len(countryCodes); got != 249+2 {
		t.Errorf("country codes: %d, want 251 (249 ISO + uk, eu)", got)
	}
}

func TestPolicyMessageBothLocales(t *testing.T) {
	prev := i18n.Current
	defer i18n.SetLanguage(prev)
	for _, lang := range []string{"en", "ru"} {
		i18n.SetLanguage(lang)
		for _, pe := range []*PolicyError{{Part: "vpn"}, {Part: "nl", Country: true}} {
			msg := policyMessage("x.example.com", pe)
			if strings.Contains(msg, "%!") || strings.Contains(msg, "MISSING") {
				t.Errorf("%s: bad format in %q", lang, msg)
			}
			if !strings.Contains(msg, pe.Part) || !strings.Contains(msg, "stats") {
				t.Errorf("%s: %q lacks the part or the suggestion", lang, msg)
			}
		}
	}
}

func TestReadAsksAgainUntilAccepted(t *testing.T) {
	answers := []string{"vpn.example.com", "nl1.example.com", "de-node.example.com", "stats.example.com", "feed.example.com"}
	asked := 0
	ask := func(string) string {
		a := answers[asked]
		asked++
		return a
	}
	if got := readWith(ask, "ENTER_PANEL_DOMAIN"); got != "stats.example.com" {
		t.Errorf("got %q", got)
	}
	if asked != 4 {
		t.Errorf("asked %d times, want 4", asked)
	}
}

func TestReadPassesInvalidNamesThrough(t *testing.T) {
	ask := func(string) string { return "localhost" }
	if got := readWith(ask, "ENTER_NODE_DOMAIN"); got != "localhost" {
		t.Errorf("got %q", got)
	}
}

// Every prompt for a new panel, subscription or node domain must go through
// Read. A direct ui.Reading would skip the policy.
func TestNoDomainPromptSkipsPolicy(t *testing.T) {
	re := regexp.MustCompile(`ui\.Reading\(i18n\.T\("(ENTER_(NODE|PANEL|SUB)_DOMAIN|SELFSTEAL)"\)\)`)
	root := filepath.Join("..", "..", "internal")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if re.Match(b) {
			t.Errorf("%s reads a domain with ui.Reading, use domain.Read", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
