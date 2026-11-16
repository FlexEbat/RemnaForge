package managepanel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

func panelConfig(t *testing.T, layout proxy.Layout, selfsteal string) string {
	t.Helper()
	cfg, err := proxy.Must(proxy.HAProxy).Panel(proxy.Params{
		Layout: layout, CookieName: "ck", CookieValue: "cv",
		PanelDomain: "panel.example.org", SubDomain: "sub.example.org", SelfstealDomain: selfsteal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestHAProxyPanelIsFoundInTheGeneratedConfig(t *testing.T) {
	for _, layout := range []proxy.Layout{proxy.Direct, proxy.Socket} {
		d, n, v := haproxyPanel(panelConfig(t, layout, "node.example.org"))
		if d != "panel.example.org" || n != "ck" || v != "cv" {
			t.Errorf("layout %d: got %q %q %q", layout, d, n, v)
		}
	}
}

func TestOpenAndCloseGiveBackTheSameFile(t *testing.T) {
	for _, layout := range []proxy.Layout{proxy.Direct, proxy.Socket} {
		orig := panelConfig(t, layout, "")
		opened, err := addHAProxy8443(orig)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(opened, "frontend panel_8443") != 1 || !strings.Contains(opened, "bind :8443 ssl crt "+proxy.HAProxyCertDir+"/") {
			t.Errorf("layout %d: the block is missing or doubled:\n%s", layout, opened)
		}
		again, _ := addHAProxy8443(opened)
		if again != opened {
			t.Errorf("layout %d: opening twice must change nothing", layout)
		}
		closed, found := removeHAProxy8443(opened)
		if !found || closed != orig {
			t.Errorf("layout %d: closing must restore the file exactly (found %v)\n--- got\n%q\n--- want\n%q", layout, found, closed, orig)
		}
		if _, found := removeHAProxy8443(orig); found {
			t.Errorf("layout %d: nothing to remove in an untouched file", layout)
		}
	}
}

func TestAddRefusesAConfigWithoutThePanel(t *testing.T) {
	if _, err := addHAProxy8443("global\n    maxconn 1\n"); err == nil {
		t.Error("a file without the panel name and cookie must be refused")
	}
}

// The cookie gate of the 8443 frontend matches the main one.
func TestTheBlockKeepsTheCookieGate(t *testing.T) {
	opened, _ := addHAProxy8443(panelConfig(t, proxy.Direct, ""))
	block := opened[strings.Index(opened, haproxy8443Begin):]
	for _, want := range []string{
		"acl auth_cookie req.cook(ck) -m str cv", "acl auth_query url_param(ck) -m str cv",
		"use_backend panel if host_panel auth_cookie", "use_backend panel if host_panel auth_query",
		"http-request silent-drop",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the 8443 frontend lacks %q", want)
		}
	}
	if strings.Contains(block, "selfsteal") {
		t.Error("the 8443 frontend must not show the selfsteal page; an unauthorized request gets no answer")
	}
}

// With the block added, haproxy still accepts the whole file.
func TestOpenedConfigPassesHAProxyCheck(t *testing.T) {
	bin, err := exec.LookPath("haproxy")
	if err != nil {
		t.Skip("haproxy is not installed")
	}
	for _, layout := range []proxy.Layout{proxy.Direct, proxy.Socket} {
		selfsteal := ""
		if layout == proxy.Socket {
			selfsteal = "node.example.org"
		}
		opened, err := addHAProxy8443(panelConfig(t, layout, selfsteal))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		certs := filepath.Join(dir, "certs")
		if err := os.MkdirAll(certs, 0o755); err != nil {
			t.Fatal(err)
		}
		key, crt := filepath.Join(dir, "k.pem"), filepath.Join(dir, "c.pem")
		if out, err := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", crt,
			"-subj", "/CN=example.org", "-days", "2").CombinedOutput(); err != nil {
			t.Skipf("openssl: %v: %s", err, out)
		}
		k, _ := os.ReadFile(key)
		c, _ := os.ReadFile(crt)
		if err := os.WriteFile(filepath.Join(certs, "example.org.pem"), append(c, k...), 0o600); err != nil {
			t.Fatal(err)
		}
		opened = strings.NewReplacer(proxy.HAProxyCertDir, certs, proxy.HAProxySocket, filepath.Join(dir, "h.sock")).Replace(opened)
		path := filepath.Join(dir, "haproxy.cfg")
		if err := os.WriteFile(path, []byte(opened), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bin, "-c", "-f", path).CombinedOutput(); err != nil {
			t.Errorf("layout %d: haproxy -c failed: %v\n%s", layout, err, out)
		}
	}
}
