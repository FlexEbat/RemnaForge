// Package nginxnode is a port of src/nginx/install_node.sh (203 lines):
// installs a standalone Remnawave node behind Nginx (selfsteal reverse
// proxy on a Unix socket, remnanode container alongside it).
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
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const nodeDir = "/opt/remnanode"

var ipv4RE = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// dockerComposeHead is the first half of docker-compose.yml, written by
// install_node_nginx() (src/nginx/install_node.sh:56-80), verbatim.
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

// nodeState carries values threaded between install_node_nginx() and
// installation_node() in the original (SELFSTEAL_DOMAIN, PANEL_IP,
// CERTIFICATE, SELFSTEAL_BASE_DOMAIN are all globals there).
type nodeState struct {
	selfstealDomain     string
	selfstealBaseDomain string
	panelIP             string
	certificate         string // joined with real newlines, see readCertificate()
}

// Original bash (src/nginx/install_node.sh:4-81): install_node_nginx().
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

// isValidIPv4 is the Go equivalent of the four chained checks at
// src/nginx/install_node.sh:21-24 (regex shape + each octet 0-255).
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

// readCertificate is the Go equivalent of src/nginx/install_node.sh:31-41:
// read pasted multi-line certificate/key content until a blank line
// following non-blank content. Lines are joined with real newlines - the
// original joins with literal "\n" (backslash-n) and later resolves that
// via `echo -e`, which is the same net effect.
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

// Original bash (src/nginx/install_node.sh:83-203): installation_node().
func InstallationNode() error {
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	state, err := installNodeNginx()
	if err != nil {
		return err
	}

	// Lines 90-93: handle_certificates(). NOTE on a bash quirk we're NOT
	// replicating: the original passes CERT_METHOD by value (not nameref)
	// into handle_certificates(), so the method it actually picked/used was
	// never written back to the caller - the "if CERT_METHOD is empty"
	// fallback right after it (lines 95-102, re-deriving the method from
	// whether an existing wildcard cert happens to be on disk) always ran
	// as dead-reckoning instead. That's a bash scoping bug, not an
	// intentional design choice, so here we just use the real method
	// HandleCertificates reports it used - more accurate than re-deriving
	// it from a filesystem side-effect.
	certResult, certErr := certs.HandleCertificates([]string{state.selfstealDomain}, "", "", nodeDir)
	if certErr != nil {
		return certErr
	}
	certMethod := certResult.Method

	// Lines 104-109.
	var nodeCertDomain string
	if certMethod == "1" {
		nodeCertDomain = state.selfstealBaseDomain
	} else {
		nodeCertDomain = state.selfstealDomain
	}

	// Lines 111-129: append the rest of docker-compose.yml.
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
`, state.certificate)

	composePath := filepath.Join(nodeDir, "docker-compose.yml")
	existing, _ := os.ReadFile(composePath)
	if err := os.WriteFile(composePath, append(existing, []byte(composeTail)...), 0644); err != nil {
		return err
	}

	// Lines 131-168: nginx.conf.
	nginxConf := fmt.Sprintf(`server_names_hash_bucket_size 64;

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

    root /var/www/html;
    index index.html;
    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;
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

	// Lines 170-171: ufw (best-effort, errors ignored like the original's 2>&1).
	_ = exec.Command("ufw", "allow", "from", state.panelIP, "to", "any", "port", "2222").Run()
	_ = exec.Command("ufw", "reload").Run()

	// Lines 173-178: bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_NODE"), ui.ColorReset)
	time.Sleep(3 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = nodeDir
	_ = upCmd.Run()

	// Line 180: install a random selfsteal template.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Lines 182-202: poll the node over HTTPS until it responds.
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
