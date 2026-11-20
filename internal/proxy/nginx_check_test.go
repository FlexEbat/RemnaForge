package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// upstreamRE finds the "server host:port;" lines of the upstream blocks.
// nginx -t resolves their host names, and the container names of the stack do
// not exist outside Docker.
var upstreamRE = regexp.MustCompile(`(?m)^(\s*server\s+)[A-Za-z][A-Za-z0-9.-]*(:\d+\s*;)`)

// listenPortRE finds the port of a "listen" directive written as "listen 443"
// or "listen [::]:443".
var listenPortRE = regexp.MustCompile(`(?m)^(\s*listen\s+(?:\[::\]:)?)(\d{2,3}\b)`)

// checkWithNginx wraps the rendered http-level configuration in a minimal
// main file and runs `nginx -t` on it, with the certificate store moved into
// a temporary directory. The test is skipped where nginx is not installed;
// CI installs it.
func checkWithNginx(t *testing.T, name, conf string, certs ...string) {
	t.Helper()
	bin, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx is not installed")
	}
	dir := t.TempDir()
	key, crt := filepath.Join(dir, "k.pem"), filepath.Join(dir, "c.pem")
	gen := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", crt,
		"-subj", "/CN=example.org", "-days", "2", "-addext", "subjectAltName=DNS:example.org,DNS:*.example.org")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("openssl cannot make a test certificate: %v: %s", err, out)
	}
	ssl := filepath.Join(dir, "ssl")
	for _, c := range certs {
		d := filepath.Join(ssl, c)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for src, dst := range map[string]string{crt: "fullchain.pem", key: "privkey.pem"} {
			b, _ := os.ReadFile(src)
			if err := os.WriteFile(filepath.Join(d, dst), b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The stack runs nginx 1.28, where "http2 on;" is a directive. Before
	// 1.25.1 it does not exist (HTTP/2 was a parameter of "listen"), so an
	// older nginx, such as the one of an Ubuntu 24.04 runner, cannot check
	// that line; everything else is still checked.
	if nginxOlderThan(t, bin, 1, 25, 1) {
		conf = regexp.MustCompile(`(?m)^\s*http2 on;\n`).ReplaceAllString(conf, "")
	}
	// nginx -t opens the listening sockets, so the test runs on high ports and
	// with the unix sockets inside its temporary directory.
	conf = listenPortRE.ReplaceAllString(conf, "${1}2${2}")
	conf = strings.ReplaceAll(conf, "/dev/shm/", dir+"/")
	conf = strings.ReplaceAll(conf, "/etc/nginx/ssl/", ssl+"/")
	conf = upstreamRE.ReplaceAllString(conf, "${1}127.0.0.1${2}")
	confPath := filepath.Join(dir, "site.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	// The temporary paths of nginx default to directories only root may create.
	main := fmt.Sprintf("pid %[1]s/nginx.pid;\nerror_log %[1]s/error.log;\nevents {}\nhttp {\n    access_log off;\n"+
		"    client_body_temp_path %[1]s/body;\n    proxy_temp_path %[1]s/proxy;\n    fastcgi_temp_path %[1]s/fastcgi;\n"+
		"    uwsgi_temp_path %[1]s/uwsgi;\n    scgi_temp_path %[1]s/scgi;\n    include %[2]s;\n}\n", dir, confPath)
	mainPath := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-t", "-c", mainPath, "-p", dir).CombinedOutput()
	if err != nil {
		t.Errorf("%s: nginx -t failed: %v\n%s\n--- config\n%s", name, err, out, conf)
	}
}

// nginxOlderThan reports whether the nginx binary is older than major.minor.patch.
func nginxOlderThan(t *testing.T, bin string, major, minor, patch int) bool {
	t.Helper()
	out, err := exec.Command(bin, "-v").CombinedOutput()
	if err != nil {
		t.Fatalf("nginx -v: %v: %s", err, out)
	}
	var a, b, c int
	if _, err := fmt.Sscanf(string(out[strings.Index(string(out), "nginx/"):]), "nginx/%d.%d.%d", &a, &b, &c); err != nil {
		t.Fatalf("cannot read the version from %q", out)
	}
	return a*1_000_000+b*1_000+c < major*1_000_000+minor*1_000+patch
}

func TestNginxConfigsPassNginxCheck(t *testing.T) {
	n := Must(Nginx)
	direct, socket := sample, sample
	direct.Layout, socket.Layout = Direct, Socket
	for name, p := range map[string]Params{"direct": direct, "socket": socket} {
		conf, err := n.Panel(p)
		if err != nil {
			t.Fatal(err)
		}
		checkWithNginx(t, "panel "+name, conf, p.PanelCert, p.SubCert, p.SelfstealCert)
	}
	node := Params{SelfstealDomain: "node.example.org", SelfstealCert: "example.org"}
	conf, err := n.Node(node)
	if err != nil {
		t.Fatal(err)
	}
	checkWithNginx(t, "node", conf, node.SelfstealCert)
}
