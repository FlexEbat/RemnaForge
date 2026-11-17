package provision

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/api"
	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/cfgfile/cfgfiletest"
	"github.com/FlexEbat/RemnaForge/internal/fakepanel"
)

func TestMain(m *testing.M) {
	sleep = func(time.Duration) {}
	cfgfiletest.Main(m)
}

const compose = "services:\n  remnanode:\n    environment:\n      - NODE_PORT=2222\n      - SECRET_KEY=\"PUBLIC KEY FROM REMNAWAVE-PANEL\"\n  remnawave-subscription-page:\n    environment:\n      - REMNAWAVE_API_TOKEN=\n"

type recording struct {
	Err      string
	Requests []fakepanel.Request
	Compose  string
}

// run provisions a fresh fake panel with o and returns what the panel saw,
// the final compose file and what was printed.
func run(t *testing.T, o Options) (recording, string) {
	t.Helper()
	fp := fakepanel.New()
	defer fp.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	o.DomainURL, o.TargetDir = fp.Host(), dir

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	err := Run(o)
	w.Close()
	os.Stdout = old
	out := <-done

	rec := recording{Requests: fp.Requests()}
	if err != nil {
		rec.Err = err.Error()
	}
	cb, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	rec.Compose = string(cb)
	return rec, out
}

func golden(t *testing.T, name string) recording {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var g recording
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func goldenText(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// maskRandom replaces the values the profile builder draws at random, the
// Reality short id and the Hysteria2 obfuscation password, with a marker
// after checking that each is a non-empty string. Everything else in the
// body must match exactly.
func maskRandom(t *testing.T, r fakepanel.Request) fakepanel.Request {
	t.Helper()
	if r.Path != "/api/config-profiles" || r.Method != "POST" {
		return r
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := body["config"].(map[string]any)["inbounds"].([]any)
	mask := func(m map[string]any, key string) {
		if s, ok := m[key].(string); !ok || s == "" {
			t.Errorf("expected a random %q, got %v", key, m[key])
		}
		m[key] = "<random>"
	}
	for _, ib := range inbounds {
		stream, _ := ib.(map[string]any)["streamSettings"].(map[string]any)
		if reality, ok := stream["realitySettings"].(map[string]any); ok {
			ids, _ := reality["shortIds"].([]any)
			if len(ids) == 0 || ids[0] == "" {
				t.Error("expected a random short id")
			}
			reality["shortIds"] = []any{"<random>"}
		}
		if mask2, ok := stream["finalmask"].(map[string]any); ok {
			for _, u := range mask2["udp"].([]any) {
				if settings, ok := u.(map[string]any)["settings"].(map[string]any); ok {
					mask(settings, "password")
				}
			}
		}
	}
	out, _ := json.Marshal(body)
	r.Body = string(out)
	return r
}

// The golden files were recorded by running the provisioning code of each
// install flow, as it was before it moved here, against the fake panel.
// Requests, bodies, the compose file afterwards and the printed lines must
// all be what the old code produced.
func TestProvisionDoesWhatEachFlowDid(t *testing.T) {
	const steal = "steal.example.org"
	chainC, keyC := certs.CaddyCertPaths(steal)
	user := Options{Username: "admin1", Password: "pass1", SelfstealDomain: steal}

	only := user
	only.NodeAddress = steal // a remote node is registered under its selfsteal name
	only.Inbounds = api.ConfigProfileInbounds{Raw: true}
	onlyHA := only
	onlyHA.ProfileTag, onlyHA.Inbounds = "XHTTP-TLS", api.ConfigProfileInbounds{XHTTP: true}

	full := user
	full.PatchNodeKey = true
	full.CertFullchain, full.CertPrivkey = "/le/fullchain.pem", "/le/privkey.pem"
	full.Inbounds = api.ConfigProfileInbounds{Raw: true}
	fullHA := full
	fullHA.ProfileTag, fullHA.Inbounds = "XHTTP-TLS", api.ConfigProfileInbounds{XHTTP: true, Hysteria2: true}
	caddyFull := full
	caddyFull.CertFullchain, caddyFull.CertPrivkey = chainC, keyC

	groups := []struct {
		flow  string
		cases []struct {
			golden string
			o      Options
		}
	}{
		{"panelonly", []struct {
			golden string
			o      Options
		}{{"panelonly-nginx", only}, {"panelonly-haproxy", onlyHA}}},
		{"caddypanelonly", []struct {
			golden string
			o      Options
		}{{"caddypanelonly-caddy", only}}},
		{"panelfull", []struct {
			golden string
			o      Options
		}{{"panelfull-nginx", full}, {"panelfull-haproxy", fullHA}}},
		{"caddypanelfull", []struct {
			golden string
			o      Options
		}{{"caddypanelfull-caddy", caddyFull}}},
	}
	for _, g := range groups {
		var printed strings.Builder
		for _, c := range g.cases {
			got, out := run(t, c.o)
			printed.WriteString(out)
			want := golden(t, c.golden)
			if got.Err != want.Err || got.Compose != want.Compose {
				t.Errorf("%s: err %q, compose differs: %v", c.golden, got.Err, got.Compose != want.Compose)
			}
			if len(got.Requests) != len(want.Requests) {
				t.Errorf("%s: %d requests, the flow made %d", c.golden, len(got.Requests), len(want.Requests))
				continue
			}
			for i := range want.Requests {
				if g, w := maskRandom(t, got.Requests[i]), maskRandom(t, want.Requests[i]); g != w {
					t.Errorf("%s request %d:\n got %+v\nwant %+v", c.golden, i, g, w)
				}
			}
		}
		if want := goldenText(t, g.flow+"-stdout.txt"); printed.String() != want {
			t.Errorf("%s: the printed progress lines differ\n--- got\n%q\n--- want\n%q", g.flow, printed.String(), want)
		}
	}
}

func baseOptions() Options {
	return Options{Username: "admin1", Password: "pass1", SelfstealDomain: "steal.example.org", Inbounds: api.ConfigProfileInbounds{Raw: true}}
}

func runWith(t *testing.T, o Options, setup func(*fakepanel.Panel)) (recording, string) {
	t.Helper()
	fp := fakepanel.New()
	defer fp.Close()
	setup(fp)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	o.DomainURL, o.TargetDir = fp.Host(), dir
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	err := Run(o)
	w.Close()
	os.Stdout = old
	rec := recording{Requests: fp.Requests()}
	if err != nil {
		rec.Err = err.Error()
	}
	return rec, <-done
}

func paths(rec recording) string {
	var p []string
	for _, r := range rec.Requests {
		p = append(p, r.Method+" "+strings.TrimPrefix(r.Path, "/api/"))
	}
	return strings.Join(p, ", ")
}

// Each step that fails stops the run at that step; nothing after it is
// sent, so a half provisioned panel is never presented as finished.
func TestRunStopsAtTheFirstFailure(t *testing.T) {
	cases := []struct {
		name, fail, lastCall string
	}{
		{"register", "POST /api/auth/register", "POST auth/register"},
		{"keys", "GET /api/system/tools/x25519/generate", "GET system/tools/x25519/generate"},
		{"profile", "POST /api/config-profiles", "POST config-profiles"},
		{"node", "POST /api/nodes", "POST nodes"},
		{"host", "POST /api/hosts", "POST hosts"},
		{"token", "POST /api/tokens", "POST tokens"},
	}
	for _, c := range cases {
		rec, _ := runWith(t, baseOptions(), func(p *fakepanel.Panel) { p.FailOn[c.fail] = true })
		if rec.Err == "" {
			t.Errorf("%s: a refused call must be an error", c.name)
		}
		if got := rec.Requests[len(rec.Requests)-1]; got.Method+" "+strings.TrimPrefix(got.Path, "/api/") != c.lastCall {
			t.Errorf("%s: the run must stop at %s, requests: %s", c.name, c.lastCall, paths(rec))
		}
	}
}

func TestNodeKeyOnlyWhenTheNodeIsOnTheHost(t *testing.T) {
	without, _ := runWith(t, baseOptions(), func(*fakepanel.Panel) {})
	if strings.Contains(paths(without), "keygen") {
		t.Errorf("a panel without a node must not ask for its key: %s", paths(without))
	}
	o := baseOptions()
	o.PatchNodeKey = true
	with, _ := runWith(t, o, func(*fakepanel.Panel) {})
	if !strings.Contains(paths(with), "GET keygen") {
		t.Errorf("a co-located node needs the key: %s", paths(with))
	}
}

// A panel without any squad is not an error: the squad step is skipped and
// the token is still created.
func TestNoSquadsIsNotAFailure(t *testing.T) {
	rec, _ := runWith(t, baseOptions(), func(p *fakepanel.Panel) { p.NoSquads = true })
	if rec.Err != "" || strings.Contains(paths(rec), "PATCH") || !strings.HasSuffix(paths(rec), "POST tokens") {
		t.Errorf("err %q, requests: %s", rec.Err, paths(rec))
	}
}

// Everything after registration carries the token the panel issued.
func TestEveryCallAfterRegistrationIsAuthenticated(t *testing.T) {
	rec, _ := runWith(t, baseOptions(), func(*fakepanel.Panel) {})
	for i, r := range rec.Requests {
		if i > 0 && !r.Authed {
			t.Errorf("request %d (%s %s) was sent without the token", i, r.Method, r.Path)
		}
	}
}

// The new inbound is what the squad, the node and the host are tied to.
func TestTheProfileInboundIsWhatTheOthersUse(t *testing.T) {
	o := baseOptions()
	o.ProfileTag, o.Inbounds = "XHTTP-TLS", api.ConfigProfileInbounds{XHTTP: true}
	rec, _ := runWith(t, o, func(*fakepanel.Panel) {})
	var created, nodeBody, hostBody, squadBody string
	for _, r := range rec.Requests {
		switch r.Method + " " + r.Path {
		case "POST /api/nodes":
			nodeBody = r.Body
		case "POST /api/hosts":
			hostBody = r.Body
		case "PATCH /api/internal-squads":
			squadBody = r.Body
		}
	}
	created = "00000000-0000-4000-8000-"
	if !strings.Contains(nodeBody, created) || !strings.Contains(hostBody, created) || !strings.Contains(squadBody, created) {
		t.Fatalf("missing ids:\nnode %s\nhost %s\nsquad %s", nodeBody, hostBody, squadBody)
	}
	var squad struct {
		Inbounds []string `json:"inbounds"`
	}
	_ = json.Unmarshal([]byte(squadBody), &squad)
	if len(squad.Inbounds) != 2 || squad.Inbounds[0] != fakepanel.OldInboundUUID {
		t.Errorf("the squad keeps its old inbound and gains the new one: %v", squad.Inbounds)
	}
}
