package proxy

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// HAProxySocket is the unix socket Xray hands the SNI of the proxy's names
// to, the counterpart of /dev/shm/nginx.sock in the nginx layout.
const HAProxySocket = "/dev/shm/haproxy.sock"

// HAProxyCertDir is where the concatenated PEM bundles (certificate chain
// and private key in one file) are mounted inside the container.
const HAProxyCertDir = "/etc/haproxy/certs"

// SelfstealPort is the loopback port of the static selfsteal site. HAProxy
// cannot serve files, so a small nginx container serves them.
const SelfstealPort = 8080

type haproxy struct{}

func (haproxy) Kind() Kind         { return HAProxy }
func (haproxy) ConfigFile() string { return "haproxy.cfg" }

// haproxyTemplate renders all three layouts. Every profile is routed by
// SNI (ssl_fc_sni) and every name is served over TLS that HAProxy
// terminates; there is no Reality here.
var haproxyTemplate = template.Must(template.New("haproxy.cfg").Parse(`global
    log stdout format raw local0
    maxconn 20000
    ssl-default-bind-options ssl-min-ver TLSv1.2 no-tls-tickets
    ssl-default-bind-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256

defaults
    mode http
    log global
    option dontlognull
    timeout connect 5s
    timeout client 60s
    timeout server 60s
    timeout tunnel 1h
    timeout http-request 10s

{{if .Socket}}# Port 443 is read as TCP and routed by SNI. A name that belongs to this
# proxy goes to the TLS frontend below over a unix socket with PROXY
# protocol, so the client address survives. Any other name is refused here,
# which is also where a later inbound can take over the default.
frontend tcp_in
    mode tcp
    bind :443
    tcp-request inspect-delay 5s
    tcp-request content accept if { req.ssl_hello_type 1 }
    use_backend https_socket if { req.ssl_sni -i{{range .Names}} {{.}}{{end}} }
    default_backend refuse

backend https_socket
    mode tcp
    server tls unix@{{.SocketPath}} send-proxy-v2

backend refuse
    mode tcp

{{end -}}
frontend https_in
{{- if .Socket}}
    bind unix@{{.SocketPath}} mode 666 accept-proxy ssl crt {{.CertDir}}/ strict-sni alpn h2,http/1.1
{{- else}}
    bind :443 ssl crt {{.CertDir}}/ strict-sni alpn h2,http/1.1
{{- end}}

    # The client address comes from PROXY protocol on the socket, and from
    # the connection itself when HAProxy owns 443.
    http-request set-header X-Real-IP %[src]
    http-request set-header X-Forwarded-For %[src]
    http-request set-header X-Forwarded-Proto https
    http-request set-header X-Forwarded-Port 443
{{- if .Panel}}

    acl host_panel ssl_fc_sni -i {{.PanelDomain}}
    acl host_sub ssl_fc_sni -i {{.SubDomain}}
    acl auth_cookie req.cook({{.CookieName}}) -m str {{.CookieValue}}
    acl auth_query url_param({{.CookieName}}) -m str {{.CookieValue}}
    acl oauth_path path_beg /oauth2/
    acl oauth_from_telegram req.hdr(referer) -m beg https://oauth.telegram.org/

    http-response set-header Set-Cookie "{{.CookieName}}={{.CookieValue}}; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=31536000" if host_panel auth_query
{{- end}}
{{- if .Selfsteal}}
    acl host_selfsteal ssl_fc_sni -i {{.SelfstealDomain}}
    acl xhttp_path path_beg /api/v2/stream-events
{{- end}}
{{if .Panel}}
    use_backend panel if host_panel auth_cookie
    use_backend panel if host_panel auth_query
    use_backend panel if host_panel oauth_path oauth_from_telegram
{{- if .Selfsteal}}
    # Without the cookie the panel name shows the selfsteal page.
    use_backend selfsteal if host_panel
{{- end}}
    use_backend subscription if host_sub
{{- end}}
{{- if .Selfsteal}}
    use_backend xhttp if host_selfsteal xhttp_path
    use_backend selfsteal if host_selfsteal
{{- end}}

    # Everything else gets no answer.
    http-request silent-drop
{{- if .Panel}}

backend panel
    server remnawave 127.0.0.1:3000

backend subscription
    http-response silent-drop if { status 400 404 500 502 }
    server json 127.0.0.1:3010
{{- end}}
{{- if .Selfsteal}}

backend selfsteal
    server site 127.0.0.1:{{.SelfstealPort}}

backend xhttp
    timeout server 300s
    server xhttp unix@/dev/shm/xhttp.sock
{{- end}}
`))

type haproxyData struct {
	Params
	Names                    []string // the SNI names this proxy answers for
	Socket, Panel, Selfsteal bool
	SocketPath, CertDir      string
	SelfstealPort            int
}

func (haproxy) render(p Params, panel bool) (string, error) {
	for name, v := range map[string]string{
		"panel domain": p.PanelDomain, "subscription domain": p.SubDomain, "selfsteal domain": p.SelfstealDomain,
		"cookie name": p.CookieName, "cookie value": p.CookieValue,
	} {
		if strings.ContainsAny(v, " \t\r\n\"'#\\$%{}") {
			return "", fmt.Errorf("%s %q cannot be written into a haproxy.cfg", name, v)
		}
	}
	if panel && (p.PanelDomain == "" || p.SubDomain == "" || p.CookieName == "" || p.CookieValue == "") {
		return "", fmt.Errorf("the panel configuration needs the panel and subscription names and the cookie")
	}
	d := haproxyData{
		Params:        p,
		Socket:        p.Layout == Socket,
		Panel:         panel,
		Selfsteal:     p.SelfstealDomain != "",
		SocketPath:    HAProxySocket,
		CertDir:       HAProxyCertDir,
		SelfstealPort: SelfstealPort,
	}
	if !panel && !d.Selfsteal {
		return "", fmt.Errorf("a node configuration needs the selfsteal name")
	}
	if panel {
		d.Names = append(d.Names, p.PanelDomain, p.SubDomain)
	}
	if d.Selfsteal {
		d.Names = append(d.Names, p.SelfstealDomain)
	}
	var b bytes.Buffer
	if err := haproxyTemplate.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (h haproxy) Panel(p Params) (string, error) { return h.render(p, true) }

func (h haproxy) Node(p Params) (string, error) {
	p.Layout = Socket
	return h.render(p, false)
}

// selfstealSite is the nginx configuration of the static selfsteal site
// behind HAProxy.
const selfstealSite = `server {
    listen 127.0.0.1:8080;
    server_name _;

    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;

    root /var/www/html;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }
}
`

// Extra returns the files besides haproxy.cfg that the stack needs: the
// config of the static selfsteal site.
func (haproxy) Extra(Params) map[string]string {
	return map[string]string{"selfsteal.conf": selfstealSite}
}

// HAProxyServices is the compose text of the HAProxy container, and for the
// Socket layout also of the nginx container that only serves the selfsteal
// site. HAProxy runs on the host network with the certificate bundles
// mounted read-only. Behind Xray it shares /dev/shm and removes a stale
// socket at start.
func HAProxyServices(certBundleDir string, layout Layout) string {
	volumes := fmt.Sprintf(`      - ./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro
      - %s:%s:ro
`, certBundleDir, HAProxyCertDir)
	command := `exec haproxy -W -db -f /usr/local/etc/haproxy/haproxy.cfg`
	if layout == Socket {
		volumes += "      - /dev/shm:/dev/shm:rw\n"
		command = "rm -f " + HAProxySocket + " && " + command
	}
	out := fmt.Sprintf(`  remnawave-haproxy:
    image: haproxy:3.0
    container_name: remnawave-haproxy
    hostname: remnawave-haproxy
    <<: [*common, *logging]
    network_mode: host
    volumes:
%s    command: sh -c '%s'
`, volumes, command)
	if layout == Socket {
		out += `
  remnawave-selfsteal:
    image: nginx:1.28
    container_name: remnawave-selfsteal
    hostname: remnawave-selfsteal
    <<: [*common, *logging]
    network_mode: host
    volumes:
      - ./selfsteal.conf:/etc/nginx/conf.d/default.conf:ro
      - /var/www/html:/var/www/html:ro
`
	}
	return out
}
