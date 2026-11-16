package panelfull

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/cfgfile/cfgfiletest"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

func TestMain(m *testing.M) { cfgfiletest.Main(m) }

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
	if got := buildCompose(proxy.Nginx, "", fullchain, privkey); got != string(want) {
		t.Error("the nginx compose file differs from the one the flow wrote before")
	}
}

var serviceRE = regexp.MustCompile(`(?m)^  ([a-z-]+):$`)

func services(compose string) []string {
	var names []string
	in := false
	for _, l := range strings.Split(compose, "\n") {
		if l == "services:" {
			in = true
			continue
		}
		if in && l != "" && !strings.HasPrefix(l, " ") {
			break
		}
		if in {
			if m := serviceRE.FindStringSubmatch(l); m != nil {
				names = append(names, m[1])
			}
		}
	}
	return names
}

func TestHAProxyCompose(t *testing.T) {
	got := buildCompose(proxy.HAProxy, "", fullchain, privkey)
	want := "remnawave-db remnawave remnawave-redis remnawave-haproxy remnawave-selfsteal remnawave-subscription-page remnanode"
	if s := strings.Join(services(got), " "); s != want {
		t.Errorf("services:\n got %s\nwant %s", s, want)
	}
	for _, bad := range []string{"remnawave-nginx", "nginx.conf", "nginx.sock"} {
		if strings.Contains(got, bad) {
			t.Errorf("the HAProxy compose still mentions %q", bad)
		}
	}
	for _, want := range []string{
		"./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro",
		certs.HAProxyBundleDir + ":" + proxy.HAProxyCertDir + ":ro",
		"./selfsteal.conf:/etc/nginx/conf.d/default.conf:ro",
		"      - " + fullchain + ":" + fullchain + ":ro", // the node still reads the certificate for Hysteria2
		"      - " + privkey + ":" + privkey + ":ro",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the HAProxy compose lacks %q", want)
		}
	}
	if strings.Count(got, "networks:\n  remnawave-network:") != 1 || !strings.HasSuffix(got, "    external: false\n") {
		t.Error("the networks and volumes footer is missing or doubled")
	}
}

// Nothing else about the file depends on the proxy: the services both
// variants share are written once and the same.
func TestComposeSharedPartIsTheSame(t *testing.T) {
	n := buildCompose(proxy.Nginx, "", fullchain, privkey)
	h := buildCompose(proxy.HAProxy, "", fullchain, privkey)
	count := func(names []string, want string) int {
		c := 0
		for _, n := range names {
			if n == want {
				c++
			}
		}
		return c
	}
	for _, shared := range []string{"remnawave-db", "remnawave", "remnawave-redis", "remnawave-subscription-page", "remnanode"} {
		if count(services(n), shared) != 1 || count(services(h), shared) != 1 {
			t.Errorf("service %q must appear once in both variants", shared)
		}
	}
	if strings.Count(n, "name: valkey-socket") != 1 || strings.Count(h, "name: valkey-socket") != 1 {
		t.Error("the volumes footer must appear once in both variants")
	}
}

// The certificate mounts go into the nginx service's volumes list, between
// its config mount and the shared directories, where the flow used to
// append them before it began to write the file whole.
func TestNginxComposeCarriesTheCertificateMounts(t *testing.T) {
	mounts := "      - /etc/letsencrypt/live/a.org/fullchain.pem:/etc/nginx/ssl/a.org/fullchain.pem:ro\n" +
		"      - /etc/letsencrypt/live/a.org/privkey.pem:/etc/nginx/ssl/a.org/privkey.pem:ro\n"
	got := buildCompose(proxy.Nginx, mounts, fullchain, privkey)
	i := strings.Index(got, "./nginx.conf:/etc/nginx/conf.d/default.conf:ro\n")
	j := strings.Index(got, mounts)
	k := strings.Index(got, "      - /dev/shm:/dev/shm:rw")
	if i < 0 || j < 0 || k < 0 || !(i < j && j < k) {
		t.Errorf("order wrong: config mount %d, cert mounts %d, shared dirs %d", i, j, k)
	}
	if strings.Contains(buildCompose(proxy.HAProxy, "", fullchain, privkey), "/etc/nginx/ssl") {
		t.Error("HAProxy has no nginx certificate mounts")
	}
}
