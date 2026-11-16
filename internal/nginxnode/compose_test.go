package nginxnode

import (
	"os"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

const (
	fullchain = "/etc/letsencrypt/live/example.org/fullchain.pem"
	privkey   = "/etc/letsencrypt/live/example.org/privkey.pem"
)

// The golden file is what the flow wrote before the compose text moved into
// buildCompose, built from the previous revision of the source.
func TestNginxComposeIsUnchanged(t *testing.T) {
	want, err := os.ReadFile("testdata/compose-nginx.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got := buildCompose(proxy.Nginx, "", "SECRET-KEY", fullchain, privkey); got != string(want) {
		t.Error("the nginx compose file differs from the one the flow wrote before")
	}
}

func TestNginxComposeCarriesTheCertificateMounts(t *testing.T) {
	mounts := "      - /etc/letsencrypt/live/a.org/fullchain.pem:/etc/nginx/ssl/a.org/fullchain.pem:ro\n"
	got := buildCompose(proxy.Nginx, mounts, "K", fullchain, privkey)
	i := strings.Index(got, "./nginx.conf:/etc/nginx/conf.d/default.conf:ro\n")
	j := strings.Index(got, mounts)
	k := strings.Index(got, "      - /dev/shm:/dev/shm:rw")
	if i < 0 || j < 0 || k < 0 || !(i < j && j < k) {
		t.Errorf("order wrong: config mount %d, cert mounts %d, shared dirs %d", i, j, k)
	}
}

func TestHAProxyNodeCompose(t *testing.T) {
	got := buildCompose(proxy.HAProxy, "", "SECRET-KEY", fullchain, privkey)
	for _, bad := range []string{"remnawave-nginx", "nginx.conf", "nginx.sock", "/etc/nginx/ssl"} {
		if strings.Contains(got, bad) {
			t.Errorf("the HAProxy node compose still mentions %q", bad)
		}
	}
	for _, want := range []string{
		"  remnawave-haproxy:", "  remnawave-selfsteal:", "  remnanode:", "SECRET_KEY=SECRET-KEY",
		certs.HAProxyBundleDir + ":" + proxy.HAProxyCertDir + ":ro",
		"      - /dev/shm:/dev/shm:rw", // the node and HAProxy meet on the XHTTP socket
		"      - " + fullchain + ":" + fullchain + ":ro",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the HAProxy node compose lacks %q", want)
		}
	}
	if strings.Index(got, "remnawave-haproxy:") > strings.Index(got, "remnanode:") {
		t.Error("the proxy service must come before the node")
	}
}
