// Package nginxnode installs a standalone Remnawave node behind Nginx
// (selfsteal reverse proxy on a Unix socket, remnanode container
// alongside it).
package nginxnode

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const nodeDir = "/opt/remnanode"

var ipv4RE = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// dockerComposeHead is the first half of the node's docker-compose.yml.
const dockerComposeHead = `x-common: &common
  ulimits:
    nofile:
      soft: 1048576
      hard: 1048576
  restart: always

x-logging: &logging
  logging:
    driver: json-file
    options:
      max-size: 100m
      max-file: 5

services:
  remnawave-nginx:
    image: nginx:1.28
    container_name: remnawave-nginx
    hostname: remnawave-nginx
    <<: [*common, *logging]
    network_mode: host
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
`

// nodeState carries the values gathered while prompting the user
// through to where the docker-compose.yml/Caddyfile are written.
type nodeState struct {
	selfstealDomain     string
	selfstealBaseDomain string
	panelIP             string
	certificate         string // joined with real newlines, see readCertificate()
}

func installNodeNginx() (*nodeState, error) {
	if err := os.MkdirAll(nodeDir, 0755); err != nil {
		return nil, err
	}

	selfstealDomain := ui.Reading(i18n.T("SELFSTEAL"))

	if domain.CheckDomain(selfstealDomain, true, false) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	var panelIP string
	for {
		panelIP = ui.Reading(i18n.T("PANEL_IP_PROMPT"))
		if isValidIPv4(panelIP) {
			break
		}
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("IP_ERROR"), ui.ColorReset)
	}

	certificate := readCertificate()

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_CONFIRM"), ui.ColorReset)
	confirm := ui.Reading("")
	fmt.Println()
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	selfstealBaseDomain := domain.ExtractDomain(selfstealDomain)

	if err := os.WriteFile(filepath.Join(nodeDir, "docker-compose.yml"), []byte(dockerComposeHead), 0644); err != nil {
		return nil, err
	}

	return &nodeState{
		selfstealDomain:     selfstealDomain,
		selfstealBaseDomain: selfstealBaseDomain,
		panelIP:             panelIP,
		certificate:         certificate,
	}, nil
}

// isValidIPv4 checks the regex shape plus that each octet is <= 255.
func isValidIPv4(s string) bool {
	m := ipv4RE.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	for _, octet := range m[1:] {
		n := 0
		for _, c := range octet {
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

// readCertificate reads pasted multi-line certificate/key content until
// a blank line following non-blank content, joining lines with real
// newlines.
func readCertificate() string {
	fmt.Print(ui.Question(i18n.T("CERT_PROMPT")))
	scanner := bufio.NewScanner(os.Stdin)

	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func InstallationNode() error {
	// EnsureInstalled bootstraps docker/certbot/ufw and other dependencies
	// if missing, so this doesn't just fail deep inside a later step with
	// a confusing error.
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	state, err := installNodeNginx()
	if err != nil {
		return err
	}

	// certs.HandleCertificates returns the actual method it used
	// (wildcard vs per-domain), which is what determines whether the
	// cert-domain field below should be the base domain or the full
	// domain - using that returned value directly avoids re-deriving it
	// from scratch (e.g. from whether a wildcard cert happens to already
	// be on disk) and ever disagreeing with it.
	certResult, certErr := certs.HandleCertificates([]string{state.selfstealDomain}, "", "", nodeDir)
	if certErr != nil {
		return certErr
	}
	certMethod := certResult.Method

	var nodeCertDomain string
	if certMethod == "1" {
		nodeCertDomain = state.selfstealBaseDomain
	} else {
		nodeCertDomain = state.selfstealDomain
	}
	certFullchain, certPrivkey := certs.NginxCertPaths(nodeCertDomain)

	// Append the rest of docker-compose.yml.
	composeTail := fmt.Sprintf(`      - /dev/shm:/dev/shm:rw
      - /var/www/html:/var/www/html:ro
    command: sh -c 'rm -f /dev/shm/nginx.sock && exec nginx -g "daemon off;"'

  remnanode:
    image: remnawave/node:latest
    container_name: remnanode
    hostname: remnanode
    <<: [*common, *logging]
    network_mode: host
    cap_add:
      - NET_ADMIN
    environment:
      - NODE_PORT=2222
      - SECRET_KEY=%s
    volumes:
      - /dev/shm:/dev/shm:rw
      - %s:%s:ro
      - %s:%s:ro
`, state.certificate, certFullchain, certFullchain, certPrivkey, certPrivkey)

	composePath := filepath.Join(nodeDir, "docker-compose.yml")
	existing, _ := os.ReadFile(composePath)
	if err := os.WriteFile(composePath, append(existing, []byte(composeTail)...), 0644); err != nil {
		return err
	}

	// Nginx.conf.
	nginxConf := fmt.Sprintf(`server_names_hash_bucket_size 64;

# Gzip Compression
gzip on;
gzip_vary on;
gzip_proxied any;
gzip_comp_level 6;
gzip_min_length 1024;
gzip_types
    application/javascript
    application/json
    application/manifest+json
    application/xml
    application/wasm
    font/opentype
    font/eot
    font/otf
    font/ttf
    image/svg+xml
    text/css
    text/javascript
    text/plain
    text/xml;

map $http_upgrade $connection_upgrade {
    default upgrade;
    ""      close;
}

ssl_protocols TLSv1.2 TLSv1.3;
ssl_ecdh_curve X25519:prime256v1:secp384r1;
ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305;
ssl_prefer_server_ciphers on;
ssl_session_timeout 1d;
ssl_session_cache shared:MozSSL:10m;
ssl_session_tickets off;

server {
    server_name %s;
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol;
    http2 on;

    ssl_certificate "/etc/nginx/ssl/%s/fullchain.pem";
    ssl_certificate_key "/etc/nginx/ssl/%s/privkey.pem";
    ssl_trusted_certificate "/etc/nginx/ssl/%s/fullchain.pem";

    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;

    location /api/v2/stream-events {
        proxy_http_version 1.1;
        proxy_pass http://unix:/dev/shm/xhttp.sock;
        proxy_set_header Host $host;
        proxy_set_header Connection "";
        proxy_set_header X-Real-IP $proxy_protocol_addr;
        proxy_set_header X-Forwarded-For $proxy_protocol_addr;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;

        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    location / {
        root /var/www/html;
        index index.html;
    }
}

server {
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol default_server;
    server_name _;
    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;
    ssl_reject_handshake on;
    return 444;
}
`, state.selfstealDomain, nodeCertDomain, nodeCertDomain, nodeCertDomain)

	if err := os.WriteFile(filepath.Join(nodeDir, "nginx.conf"), []byte(nginxConf), 0644); err != nil {
		return err
	}

	// Ufw (best-effort: this is a nice-to-have hardening step, not worth
	// failing the whole install over).
	_ = exec.Command("ufw", "allow", "from", state.panelIP, "to", "any", "port", "2222").Run()
	_ = exec.Command("ufw", "reload").Run()

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_NODE"), ui.ColorReset)
	time.Sleep(3 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = nodeDir
	_ = upCmd.Run()

	// Install a random selfsteal template.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Poll the node over HTTPS until it responds.
	fmt.Printf(ui.ColorYellow+i18n.T("NODE_CHECK")+ui.ColorReset+"\n", state.selfstealDomain)
	const maxAttempts = 5
	const delay = 15 * time.Second
	client := &http.Client{Timeout: 10 * time.Second}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf(ui.ColorYellow+i18n.T("NODE_ATTEMPT")+ui.ColorReset+"\n", attempt, maxAttempts)

		ok := false
		if resp, err := client.Get("https://" + state.selfstealDomain); err == nil {
			body := make([]byte, 4096)
			n, _ := resp.Body.Read(body)
			resp.Body.Close()
			ok = resp.StatusCode < 400 && strings.Contains(strings.ToLower(string(body[:n])), "html")
		}

		if ok {
			fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("NODE_LAUNCHED"), ui.ColorReset)
			break
		}

		fmt.Printf(ui.ColorRed+i18n.T("NODE_UNAVAILABLE")+ui.ColorReset+"\n", attempt)
		if attempt == maxAttempts {
			fmt.Printf(ui.ColorRed+i18n.T("NODE_NOT_CONNECTED")+ui.ColorReset+"\n", maxAttempts)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CONFIG"), ui.ColorReset)
			return fmt.Errorf("node not reachable after %d attempts", maxAttempts)
		}
		time.Sleep(delay)
	}

	return nil
}
