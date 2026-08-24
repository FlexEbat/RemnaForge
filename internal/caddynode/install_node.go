// Package caddynode is a port of src/caddy/install_node.sh (180 lines):
// installs a standalone Remnawave node behind Caddy (selfsteal reverse
// proxy on a Unix socket, remnanode container alongside it).
//
// Unlike internal/nginxnode, this doesn't call internal/certs at all.
// Caddy issues and renews its own TLS certificate automatically via its
// built-in ACME client (the `tls` listener wrapper plus `auto_https` in
// the Caddyfile), so there's no certbot step here.
package caddynode

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

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const nodeDir = "/opt/remnanode"

var ipv4RE = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// dockerComposeTemplate mirrors src/caddy/install_node.sh:52-109 exactly,
// aside from Go's %s substitutions in place of bash's $VAR interpolation.
const dockerComposeTemplate = `x-common: &common
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
    caddy:
      image: caddy:2.11.2
      container_name: caddy-remnawave
      hostname: caddy-remnawave
      <<: [*common, *logging]
      network_mode: host
      volumes:
          - ./Caddyfile:/etc/caddy/Caddyfile
          - /var/www/html:/var/www/html:ro
          - /dev/shm:/dev/shm:rw
          - caddy_data:/data
      command: sh -c 'rm -f /dev/shm/nginx.sock && caddy run --config /etc/caddy/Caddyfile --adapter caddyfile'
      environment:
          - CADDY_SOCKET_PATH=/dev/shm/nginx.sock
          - SELF_STEAL_DOMAIN=%s
      healthcheck:
          test: ["CMD", "test", "-S", "/dev/shm/nginx.sock"]
          interval: 2s
          timeout: 5s
          retries: 15
          start_period: 5s

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

volumes:
  caddy_data:
    name: caddy_data
    driver: local
    external: false
`

// caddyfileTemplate mirrors src/caddy/install_node.sh:111-139 verbatim.
// {$SELF_STEAL_DOMAIN} and {$CADDY_SOCKET_PATH} are Caddy's own
// environment-variable placeholders, resolved by Caddy itself at runtime
// from the container's environment (set in dockerComposeTemplate above),
// not by this Go code - the Caddyfile content is static, no domain
// substitution happens here.
const caddyfileTemplate = `{
    admin off
    servers {
        listener_wrappers {
            proxy_protocol
            tls
        }
    }
    auto_https disable_redirects
}

http://{$SELF_STEAL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$SELF_STEAL_DOMAIN}{uri} permanent
}

https://{$SELF_STEAL_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}
    root * /var/www/html
    try_files {path} /index.html
    file_server
}

:80 {
    bind 0.0.0.0
    respond 204
}
`

// nodeState carries values threaded between install_node_caddy() and
// installation_node_caddy() in the original (SELFSTEAL_DOMAIN, PANEL_IP,
// CERTIFICATE are all globals there).
type nodeState struct {
	selfstealDomain string
	panelIP         string
	certificate     string // joined with real newlines, see readCertificate()
}

// Original bash (src/caddy/install_node.sh:4-140): install_node_caddy().
func installNodeCaddy() (*nodeState, error) {
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

	dockerCompose := fmt.Sprintf(dockerComposeTemplate, selfstealDomain, certificate)
	if err := os.WriteFile(filepath.Join(nodeDir, "docker-compose.yml"), []byte(dockerCompose), 0644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "Caddyfile"), []byte(caddyfileTemplate), 0644); err != nil {
		return nil, err
	}

	return &nodeState{
		selfstealDomain: selfstealDomain,
		panelIP:         panelIP,
		certificate:     certificate,
	}, nil
}

// isValidIPv4 is the Go equivalent of the four chained checks at
// src/caddy/install_node.sh:21-24 (regex shape + each octet 0-255).
// Duplicated from internal/nginxnode rather than shared, the same
// pattern DirRemnawave duplication follows elsewhere in this codebase.
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

// readCertificate is the Go equivalent of src/caddy/install_node.sh:31-41:
// read pasted multi-line certificate/key content until a blank line
// following non-blank content. Lines join with real newlines here. The
// original joins with the literal two characters "\n" and resolves them
// to real newlines later via `echo -e`, producing the same result.
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

// InstallationNode is the Go equivalent of
// src/caddy/install_node.sh:142-180: installation_node_caddy().
func InstallationNode() error {
	// EnsureInstalled bootstraps docker/certbot/ufw and other dependencies
	// if missing, matching the original's auto-install behavior instead
	// of only reporting the problem. certbot itself is unused by the
	// Caddy flow, but ufw and docker are needed the same as everywhere
	// else, so this still runs.
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING_NODE"), ui.ColorReset)

	state, err := installNodeCaddy()
	if err != nil {
		return err
	}

	// Lines 146-148: ufw (best-effort, matching the original's suppressed errors).
	_ = exec.Command("ufw", "allow", "80/tcp", "comment", "HTTP").Run()
	_ = exec.Command("ufw", "allow", "from", state.panelIP, "to", "any", "port", "2222").Run()
	_ = exec.Command("ufw", "reload").Run()

	// Lines 150-155: bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_NODE"), ui.ColorReset)
	time.Sleep(3 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = nodeDir
	_ = upCmd.Run()

	// Line 157: install a random selfsteal template.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Lines 159-179: poll the node over HTTPS until it responds.
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
