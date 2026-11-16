package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func haproxyParams(layout Layout, withNode bool) Params {
	p := Params{
		Layout: layout, CookieName: "ck", CookieValue: "cv",
		PanelDomain: "panel.example.org", SubDomain: "sub.example.org",
	}
	if withNode {
		p.SelfstealDomain = "node.example.org"
	}
	return p
}

func render(t *testing.T, name string, p Params, node bool) string {
	t.Helper()
	b := Must(HAProxy)
	var s string
	var err error
	if node {
		s, err = b.Node(p)
	} else {
		s, err = b.Panel(p)
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

// checkWithHAProxy runs `haproxy -c` on cfg with the certificate directory
// and the socket moved into a temporary directory. The test is skipped where
// haproxy is not installed; CI installs it.
func checkWithHAProxy(t *testing.T, name, cfg string) {
	t.Helper()
	bin, err := exec.LookPath("haproxy")
	if err != nil {
		t.Skip("haproxy is not installed")
	}
	dir := t.TempDir()
	certs := filepath.Join(dir, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	pem := filepath.Join(certs, "example.org.pem")
	key, crt := filepath.Join(dir, "k.pem"), filepath.Join(dir, "c.pem")
	gen := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", crt,
		"-subj", "/CN=example.org", "-days", "2", "-addext", "subjectAltName=DNS:example.org,DNS:*.example.org")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("openssl cannot make a test certificate: %v: %s", err, out)
	}
	k, _ := os.ReadFile(key)
	c, _ := os.ReadFile(crt)
	if err := os.WriteFile(pem, append(c, k...), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg = strings.NewReplacer(HAProxyCertDir, certs, HAProxySocket, filepath.Join(dir, "haproxy.sock")).Replace(cfg)
	path := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-c", "-f", path).CombinedOutput(); err != nil {
		t.Errorf("%s: haproxy -c failed: %v\n%s\n--- config\n%s", name, err, out, cfg)
	}
}

func TestHAProxyConfigsPassHAProxyCheck(t *testing.T) {
	checkWithHAProxy(t, "panel only", render(t, "panel only", haproxyParams(Direct, false), false))
	checkWithHAProxy(t, "panel and node", render(t, "panel and node", haproxyParams(Socket, true), false))
	checkWithHAProxy(t, "node", render(t, "node", haproxyParams(Socket, true), true))
}

func TestHAProxyLayouts(t *testing.T) {
	direct := render(t, "direct", haproxyParams(Direct, false), false)
	socket := render(t, "socket", haproxyParams(Socket, true), false)
	node := render(t, "node", Params{SelfstealDomain: "node.example.org"}, true)

	if !strings.Contains(direct, "bind :443 ssl") || strings.Contains(direct, "accept-proxy") {
		t.Error("the direct layout must listen on 443 without PROXY protocol")
	}
	if strings.Contains(direct, "selfsteal") {
		t.Error("a panel without a node has no selfsteal backend")
	}
	for name, cfg := range map[string]string{"socket": socket, "node": node} {
		if !strings.Contains(cfg, "bind unix@"+HAProxySocket+" mode 666 accept-proxy ssl") {
			t.Errorf("%s: the layout behind Xray must accept PROXY protocol on %s", name, HAProxySocket)
		}
		if !strings.Contains(cfg, "use_backend xhttp if host_selfsteal xhttp_path") {
			t.Errorf("%s: the XHTTP path must reach /dev/shm/xhttp.sock", name)
		}
		if !strings.Contains(cfg, "server site 127.0.0.1:8080") {
			t.Errorf("%s: selfsteal must point at the static site container", name)
		}
	}
	for name, cfg := range map[string]string{"socket": socket, "node": node} {
		for _, want := range []string{
			"frontend tcp_in", "mode tcp", "bind :443",
			"server tls unix@" + HAProxySocket + " send-proxy-v2", "default_backend refuse",
		} {
			if !strings.Contains(cfg, want) {
				t.Errorf("%s: the SNI front on 443 lacks %q", name, want)
			}
		}
	}
	if !strings.Contains(socket, "req.ssl_sni -i panel.example.org sub.example.org node.example.org") ||
		!strings.Contains(node, "req.ssl_sni -i node.example.org }") {
		t.Error("the TCP front must route exactly the names this proxy serves")
	}
	if strings.Contains(direct, "tcp_in") {
		t.Error("with HAProxy on 443 itself there is no TCP front")
	}
	if !strings.Contains(socket, "use_backend selfsteal if host_panel") {
		t.Error("the panel name without the cookie must show the selfsteal page")
	}
	if strings.Contains(node, "backend panel") || strings.Contains(node, "backend subscription") {
		t.Error("a node has no panel backends")
	}
	for name, cfg := range map[string]string{"direct": direct, "socket": socket, "node": node} {
		for _, want := range []string{"strict-sni", "ssl-min-ver TLSv1.2", "http-request silent-drop", "X-Real-IP %[src]"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if strings.Contains(strings.ToLower(cfg), "reality") {
			t.Errorf("%s: HAProxy flows are TLS only", name)
		}
	}
	if !strings.Contains(socket, `acl auth_cookie req.cook(ck) -m str cv`) ||
		!strings.Contains(socket, `Set-Cookie "ck=cv; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=31536000" if host_panel auth_query`) {
		t.Error("the cookie gate of the nginx layout is missing")
	}
}

func TestHAProxyRefusesValuesThatBreakTheConfig(t *testing.T) {
	b := Must(HAProxy)
	for _, mut := range []func(*Params){
		func(p *Params) { p.PanelDomain = "panel.example.org\nbackend evil" },
		func(p *Params) { p.CookieValue = `x"y` },
		func(p *Params) { p.SubDomain = "sub example.org" },
		func(p *Params) { p.CookieName = "a#b" },
		func(p *Params) { p.CookieName = "" },
		func(p *Params) { p.PanelDomain = "" },
	} {
		p := haproxyParams(Socket, true)
		mut(&p)
		if _, err := b.Panel(p); err == nil {
			t.Errorf("%+v must be refused", p)
		}
	}
	if _, err := b.Node(Params{}); err == nil {
		t.Error("a node without a selfsteal name must be refused")
	}
}

func TestHAProxyExtraAndServices(t *testing.T) {
	extra := Must(HAProxy).Extra(Params{SelfstealDomain: "node.example.org"})
	if Must(HAProxy).Extra(Params{}) != nil {
		t.Error("a panel without a node has no selfsteal site to configure")
	}
	if !strings.Contains(extra["selfsteal.conf"], "listen 127.0.0.1:8080;") {
		t.Errorf("selfsteal.conf: %v", extra)
	}
	svc := HAProxyServices("/etc/letsencrypt/haproxy", Socket)
	for _, want := range []string{
		"remnawave-haproxy:", "image: haproxy:3.0", "network_mode: host",
		"/etc/letsencrypt/haproxy:" + HAProxyCertDir + ":ro", "rm -f " + HAProxySocket,
		"remnawave-selfsteal:", "./selfsteal.conf:/etc/nginx/conf.d/default.conf:ro",
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("services lack %q", want)
		}
	}
	direct := HAProxyServices("/etc/letsencrypt/haproxy", Direct)
	for _, bad := range []string{"selfsteal", "/dev/shm", "rm -f"} {
		if strings.Contains(direct, bad) {
			t.Errorf("HAProxy on 443 itself needs no %q", bad)
		}
	}
	if !strings.Contains(direct, "exec haproxy -W -db") || !strings.Contains(direct, HAProxyCertDir+":ro") {
		t.Errorf("the direct service must still run HAProxy with the bundles:\n%s", direct)
	}
}
