package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

var sample = Params{
	CookieName: "ck", CookieValue: "cv",
	PanelDomain: "panel.example.org", PanelCert: "example.org",
	SubDomain: "sub.example.org", SubCert: "sub.example.org",
	SelfstealDomain: "node.example.org", SelfstealCert: "example.org",
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The golden files are the output of the install flows before their
// templates moved here, so these tests show that the move changed nothing.
func TestBackendsRenderWhatTheFlowsRendered(t *testing.T) {
	n, _ := For(Nginx)
	c, _ := For(Caddy)
	direct, socket := sample, sample
	direct.Layout, socket.Layout = Direct, Socket
	node := Params{SelfstealDomain: "node.example.org", SelfstealCert: "example.org"}
	cases := []struct {
		name string
		b    Backend
		p    Params
		node bool
		file string
	}{
		{"nginx direct", n, direct, false, "nginx-direct.conf"},
		{"nginx socket", n, socket, false, "nginx-socket.conf"},
		{"nginx node", n, node, true, "nginx-node.conf"},
		{"caddy direct", c, direct, false, "caddy-direct.caddyfile"},
		{"caddy socket", c, socket, false, "caddy-socket.caddyfile"},
		{"caddy node", c, node, true, "caddy-node.caddyfile"},
	}
	for _, tc := range cases {
		var got string
		var err error
		if tc.node {
			got, err = tc.b.Node(tc.p)
		} else {
			got, err = tc.b.Panel(tc.p)
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if want := golden(t, tc.file); got != want {
			t.Errorf("%s differs from the golden file %s", tc.name, tc.file)
		}
	}
}

func TestForRejectsUnknown(t *testing.T) {
	if _, err := For("apache"); err == nil {
		t.Error("an unknown proxy must be refused")
	}
}

func TestConfigFiles(t *testing.T) {
	n, _ := For(Nginx)
	c, _ := For(Caddy)
	if n.ConfigFile() != "nginx.conf" || c.ConfigFile() != "Caddyfile" || n.Kind() != Nginx || c.Kind() != Caddy {
		t.Error("kind or file name wrong")
	}
}
