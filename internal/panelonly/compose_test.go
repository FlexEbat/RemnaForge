package panelonly

import (
	"os"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

// The golden file is what the flow wrote before the compose text moved into
// buildCompose, built from the previous revision of the source.
func TestNginxComposeIsUnchanged(t *testing.T) {
	want, err := os.ReadFile("testdata/compose-nginx.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got := buildCompose(proxy.Nginx, ""); got != string(want) {
		t.Error("the nginx compose file differs from the one the flow wrote before")
	}
}

func TestNginxComposeCarriesTheCertificateMounts(t *testing.T) {
	mounts := "      - /etc/letsencrypt/live/a.org/fullchain.pem:/etc/nginx/ssl/a.org/fullchain.pem:ro\n"
	got := buildCompose(proxy.Nginx, mounts)
	i := strings.Index(got, "./nginx.conf:/etc/nginx/conf.d/default.conf:ro\n")
	j := strings.Index(got, mounts)
	k := strings.Index(got, "  remnawave-subscription-page:")
	if i < 0 || j < 0 || k < 0 || !(i < j && j < k) {
		t.Errorf("the mounts must follow the nginx config mount and precede the next service: %d %d %d", i, j, k)
	}
}

func TestHAProxyCompose(t *testing.T) {
	got := buildCompose(proxy.HAProxy, "")
	for _, bad := range []string{"remnawave-nginx", "remnawave-selfsteal", "/dev/shm", "nginx.conf"} {
		if strings.Contains(got, bad) {
			t.Errorf("a panel without a node behind HAProxy must not mention %q", bad)
		}
	}
	for _, want := range []string{
		"  remnawave-haproxy:", "./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro",
		certs.HAProxyBundleDir + ":" + proxy.HAProxyCertDir + ":ro",
		"  remnawave-subscription-page:", "valkey-socket:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the HAProxy compose lacks %q", want)
		}
	}
	if strings.Index(got, "remnawave-haproxy:") > strings.Index(got, "remnawave-subscription-page:") {
		t.Error("the proxy service comes before the subscription page, as with nginx")
	}
}
