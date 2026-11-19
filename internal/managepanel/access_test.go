package managepanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

func freePort(t *testing.T, inUse, checked bool) {
	t.Helper()
	old := portChecker
	portChecker = func(string) (bool, bool) { return inUse, checked }
	t.Cleanup(func() { portChecker = old })
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEveryProxyOpensAndClosesBackToTheOriginal(t *testing.T) {
	freePort(t, false, true)

	nginxConf, err := proxy.Must(proxy.Nginx).Panel(proxy.Params{
		Layout: proxy.Direct, CookieName: "ck", CookieValue: "cv",
		PanelDomain: "panel.example.org", PanelCert: "example.org", SubDomain: "sub.example.org", SubCert: "sub.example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	haConf, err := proxy.Must(proxy.HAProxy).Panel(proxy.Params{
		Layout: proxy.Direct, CookieName: "ck", CookieValue: "cv",
		PanelDomain: "panel.example.org", SubDomain: "sub.example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	caddyConf, err := proxy.Must(proxy.Caddy).Panel(proxy.Params{Layout: proxy.Direct, CookieName: "ck", CookieValue: "cv"})
	if err != nil {
		t.Fatal(err)
	}
	caddyCompose := "services:\n  remnawave-caddy:\n    environment:\n      - PANEL_DOMAIN=panel.example.org\n"

	cases := []struct {
		web, file, original, link string
		extra                     map[string]string
	}{
		{"nginx", "nginx.conf", nginxConf, "https://panel.example.org:8443/auth/login?ck=cv", nil},
		{"haproxy", "haproxy.cfg", haConf, "https://panel.example.org:8443/auth/login?ck=cv", nil},
		{"caddy", "Caddyfile", caddyConf, "https://panel.example.org:8443/auth/login", map[string]string{"docker-compose.yml": caddyCompose}},
	}
	for _, c := range cases {
		dir := t.TempDir()
		put(t, dir, c.file, c.original)
		for name, content := range c.extra {
			put(t, dir, name, content)
		}
		if detectWebserver(dir) != c.web {
			t.Errorf("%s: detected %q", c.web, detectWebserver(dir))
		}
		p := accessFor(c.web)

		open, ok := p.open(dir)
		if !ok || open.nothingToDo != "" {
			t.Fatalf("%s: open: %+v ok=%v", c.web, open, ok)
		}
		if open.confPath != filepath.Join(dir, c.file) || open.updated == c.original || !strings.HasPrefix(open.link, c.link) {
			t.Errorf("%s: plan %+v", c.web, open)
		}
		if err := os.WriteFile(open.confPath, []byte(open.updated), 0o600); err != nil {
			t.Fatal(err)
		}

		closed, ok := p.close(dir)
		if !ok || closed.nothingToDo != "" || closed.updated != c.original {
			t.Errorf("%s: closing must give back the original file (ok=%v, nothingToDo=%q)", c.web, ok, closed.nothingToDo)
		}
		if err := os.WriteFile(closed.confPath, []byte(closed.updated), 0o600); err != nil {
			t.Fatal(err)
		}

		again, ok := p.close(dir)
		if !ok || again.nothingToDo != "PORT_8443_NOT_CONFIGURED" {
			t.Errorf("%s: closing twice must say there is nothing to close: %+v", c.web, again)
		}
	}
}

func TestOpeningTwiceIsNotAnError(t *testing.T) {
	freePort(t, false, true)
	dir := t.TempDir()
	caddyConf, _ := proxy.Must(proxy.Caddy).Panel(proxy.Params{Layout: proxy.Direct, CookieName: "ck", CookieValue: "cv"})
	put(t, dir, "Caddyfile", caddyConf)
	put(t, dir, "docker-compose.yml", "      - PANEL_DOMAIN=panel.example.org\n")
	p := accessFor("caddy")
	open, _ := p.open(dir)
	if err := os.WriteFile(open.confPath, []byte(open.updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, ok := p.open(dir); !ok || again.nothingToDo != "PORT_8443_ALREADY_CONFIGURED" {
		t.Errorf("a second open must report that it is configured: %+v", again)
	}
}

func TestPortChecksStopTheOpening(t *testing.T) {
	dir := t.TempDir()
	haConf, _ := proxy.Must(proxy.HAProxy).Panel(proxy.Params{
		Layout: proxy.Direct, CookieName: "ck", CookieValue: "cv", PanelDomain: "panel.example.org", SubDomain: "sub.example.org",
	})
	put(t, dir, "haproxy.cfg", haConf)
	p := accessFor("haproxy")

	freePort(t, true, true) // something else holds 8443
	if _, ok := p.open(dir); ok {
		t.Error("a busy port must stop the opening")
	}
	freePort(t, false, false) // no tool to check
	if _, ok := p.open(dir); ok {
		t.Error("an unchecked port must stop the opening")
	}
	// Our own block holding the port is not a conflict.
	freePort(t, false, true)
	open, _ := p.open(dir)
	if err := os.WriteFile(open.confPath, []byte(open.updated), 0o600); err != nil {
		t.Fatal(err)
	}
	freePort(t, true, true)
	if _, ok := p.open(dir); !ok {
		t.Error("the port held by our own block must not block opening again")
	}
}

func TestRestartStrategies(t *testing.T) {
	// The HAProxy access is reloaded in place; the others are recreated.
	// Only the distinction is checked here; composeRun needs Docker.
	if accessFor("haproxy").restart == nil || accessFor("nginx").restart == nil || accessFor("caddy").restart == nil {
		t.Error("every proxy needs a restart step")
	}
}

func stubProxyChecks(t *testing.T, running func(string) bool) {
	t.Helper()
	oldRun, oldWait := containerRunning, waitBetweenChecks
	containerRunning = running
	waitBetweenChecks = func() {}
	t.Cleanup(func() { containerRunning, waitBetweenChecks = oldRun, oldWait })
}

// A configuration the proxy rejects used to leave the panel without its
// reverse proxy, with the bad file in place.
func TestApplyPlanPutsTheOldConfigBackWhenTheProxyDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "nginx.conf", "old config\n")
	restarts := 0
	healthy := false
	stubProxyChecks(t, func(string) bool { return healthy })
	p := accessProxy{name: "nginx", container: "remnawave-nginx", restart: func(string) {
		restarts++
		healthy = restarts > 1 // the second restart reads the restored file
	}}
	plan := accessPlan{confPath: filepath.Join(dir, "nginx.conf"), updated: "broken config\n"}

	if applyPlan(p, dir, plan) {
		t.Fatal("a proxy that did not start counted as success")
	}
	got, _ := os.ReadFile(plan.confPath)
	if string(got) != "old config\n" {
		t.Errorf("nginx.conf = %q, want the old text back", got)
	}
	if restarts != 2 {
		t.Errorf("restarts = %d, want 2 (new config, then the restored one)", restarts)
	}
}

func TestApplyPlanKeepsTheNewConfigWhenTheProxyRuns(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "nginx.conf", "old config\n")
	stubProxyChecks(t, func(string) bool { return true })
	p := accessProxy{name: "nginx", container: "remnawave-nginx", restart: func(string) {}}
	plan := accessPlan{confPath: filepath.Join(dir, "nginx.conf"), updated: "new config\n"}
	if !applyPlan(p, dir, plan) {
		t.Fatal("a healthy proxy counted as failure")
	}
	if got, _ := os.ReadFile(plan.confPath); string(got) != "new config\n" {
		t.Errorf("nginx.conf = %q", got)
	}
}

// HAProxy reloads in place and keeps its old workers on a bad file, so there
// is no container to wait for.
func TestApplyPlanSkipsTheCheckForHAProxy(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "haproxy.cfg", "old\n")
	stubProxyChecks(t, func(string) bool { t.Fatal("HAProxy was checked"); return false })
	p := accessProxy{name: "haproxy", restart: func(string) {}}
	if !applyPlan(p, dir, accessPlan{confPath: filepath.Join(dir, "haproxy.cfg"), updated: "new\n"}) {
		t.Error("HAProxy apply failed")
	}
}

func TestEveryRecreatedProxyIsChecked(t *testing.T) {
	for _, web := range []string{"nginx", "caddy"} {
		if accessFor(web).container == "" {
			t.Errorf("%s has no container to check", web)
		}
	}
	if accessFor("haproxy").container != "" {
		t.Error("HAProxy reloads in place and needs no container check")
	}
}

func TestFindTopLevelBlocksIgnoresCommentedOutBlocks(t *testing.T) {
	conf := "# server {\n#     listen 80;\n# }\nserver {\n    listen 443;\n}\n"
	blocks := findTopLevelBlocks(conf, "server")
	if len(blocks) != 1 {
		t.Fatalf("found %d blocks, want 1", len(blocks))
	}
	if body := blocks[0].Body(conf); !strings.Contains(body, "listen 443") || strings.Contains(body, "listen 80") {
		t.Errorf("the block is %q", body)
	}
}

func TestFindTopLevelBlocksIsNotConfusedByBracesInComments(t *testing.T) {
	conf := "server {\n    listen 443; # closes with } soon\n    location / { return 200; }\n}\nserver {\n    listen 8443;\n}\n"
	if blocks := findTopLevelBlocks(conf, "server"); len(blocks) != 2 {
		t.Fatalf("found %d blocks, want 2", len(blocks))
	}
}

func TestFindTopLevelBlocksKeepsAHashInsideQuotes(t *testing.T) {
	conf := "server {\n    add_header X-Tag \"a#b\"; location / { }\n}\n"
	blocks := findTopLevelBlocks(conf, "server")
	if len(blocks) != 1 || !strings.Contains(blocks[0].Body(conf), "location / { }") {
		t.Fatalf("blocks = %+v", blocks)
	}
}
