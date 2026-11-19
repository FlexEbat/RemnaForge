package addnode

import (
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/api"
	"github.com/FlexEbat/RemnaForge/internal/fakepanel"
)

// registered returns a panel with a superadmin and the token it was issued.
func registered(t *testing.T) (*fakepanel.Panel, string) {
	t.Helper()
	p := fakepanel.New()
	token := api.RegisterRemnawave(p.Host(), "admin", "Zq8vLm2xT9pRw4Yk7Nb", "")
	if token == "" {
		p.Close()
		t.Fatal("no token from the fake panel")
	}
	p.Reset()
	return p, token
}

func requestKeys(p *fakepanel.Panel) []string {
	var keys []string
	for _, r := range p.Requests() {
		keys = append(keys, r.Method+" "+r.Path)
	}
	return keys
}

func TestCreateNodeObjectsCreatesProfileNodeAndHost(t *testing.T) {
	p, token := registered(t)
	defer p.Close()
	inbound, ok := createNodeObjects(p.Host(), token, "mynode", "node.example.com", "privkey")
	if !ok || inbound == "" {
		t.Fatalf("ok=%v inbound=%q", ok, inbound)
	}
	got := strings.Join(requestKeys(p), "\n")
	for _, want := range []string{"POST /api/config-profiles", "POST /api/nodes", "POST /api/hosts"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in:\n%s", want, got)
		}
	}
}

// A profile left behind by a failed node keeps its name taken, so the
// operator's second try with the same name was refused.
func TestCreateNodeObjectsDeletesTheProfileWhenTheNodeFails(t *testing.T) {
	p, token := registered(t)
	defer p.Close()
	p.FailOn["POST /api/nodes"] = true
	if _, ok := createNodeObjects(p.Host(), token, "mynode", "node.example.com", "privkey"); ok {
		t.Fatal("a failed node counted as success")
	}
	got := strings.Join(requestKeys(p), "\n")
	if !strings.Contains(got, "DELETE /api/config-profiles/") {
		t.Errorf("the profile was not deleted:\n%s", got)
	}
	if strings.Contains(got, "POST /api/hosts") {
		t.Errorf("a host was created for a node that failed:\n%s", got)
	}
}
