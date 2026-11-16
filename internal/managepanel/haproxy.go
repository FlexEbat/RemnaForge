package managepanel

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/proxy"
)

// The temporary panel access on 8443 for an HAProxy stack is one frontend
// between two marker lines, so closing it removes exactly what opening it
// added and nothing else in a file the operator may have edited.
const (
	haproxy8443Begin = "# remnaforge:8443 begin"
	haproxy8443End   = "# remnaforge:8443 end"
)

var (
	haproxyPanelRE  = regexp.MustCompile(`(?m)^\s*acl host_panel ssl_fc_sni -i (\S+)\s*$`)
	haproxyCookieRE = regexp.MustCompile(`(?m)^\s*acl auth_cookie req\.cook\((\S+)\) -m str (\S+)\s*$`)
	haproxy8443RE   = regexp.MustCompile(`(?s)\n?` + regexp.QuoteMeta(haproxy8443Begin) + `.*?` + regexp.QuoteMeta(haproxy8443End) + `\n?`)
)

// haproxyPanel reads the panel name and the access cookie from haproxy.cfg.
func haproxyPanel(cfg string) (domain, cookieName, cookieValue string) {
	if m := haproxyPanelRE.FindStringSubmatch(cfg); m != nil {
		domain = m[1]
	}
	if m := haproxyCookieRE.FindStringSubmatch(cfg); m != nil {
		cookieName, cookieValue = m[1], m[2]
	}
	return domain, cookieName, cookieValue
}

func haproxyHas8443(cfg string) bool { return strings.Contains(cfg, haproxy8443Begin) }

// addHAProxy8443 appends a TLS frontend on 8443 that serves only the panel
// name, with the same cookie gate as the main frontend and no answer
// otherwise. It replaces an earlier block, so opening twice is harmless.
func addHAProxy8443(cfg string) (string, error) {
	domain, name, value := haproxyPanel(cfg)
	if domain == "" || name == "" || value == "" {
		return "", fmt.Errorf("the panel name or its cookie is not in haproxy.cfg")
	}
	cfg, _ = removeHAProxy8443(cfg)
	block := fmt.Sprintf(`
%s
frontend panel_8443
    bind :8443 ssl crt %s/ strict-sni alpn h2,http/1.1

    http-request set-header X-Real-IP %%[src]
    http-request set-header X-Forwarded-For %%[src]
    http-request set-header X-Forwarded-Proto https
    http-request set-header X-Forwarded-Port 8443

    acl host_panel ssl_fc_sni -i %s
    acl auth_cookie req.cook(%s) -m str %s
    acl auth_query url_param(%s) -m str %s

    http-response set-header Set-Cookie "%s=%s; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=31536000" if host_panel auth_query

    use_backend panel if host_panel auth_cookie
    use_backend panel if host_panel auth_query
    http-request silent-drop
%s
`, haproxy8443Begin, proxy.HAProxyCertDir, domain, name, value, name, value, name, value, haproxy8443End)
	return strings.TrimRight(cfg, "\n") + "\n" + block, nil
}

// removeHAProxy8443 takes the block out and reports whether there was one.
func removeHAProxy8443(cfg string) (string, bool) {
	if !haproxyHas8443(cfg) {
		return cfg, false
	}
	return haproxy8443RE.ReplaceAllString(cfg, ""), true
}
