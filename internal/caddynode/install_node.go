// Package caddynode installs a standalone Remnawave node behind Caddy
// (selfsteal reverse proxy on a Unix socket, remnanode container
// alongside it).
//
// Unlike internal/nginxnode, this doesn't call internal/certs at all.
// Caddy issues and renews its own TLS certificate automatically via its
// built-in ACME client (the `tls` listener wrapper plus `auto_https` in
// the Caddyfile), so there's no certbot step here.
package caddynode

import (
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/domain"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/firewall"
	"github.com/FlexEbat/RemnaForge/internal/fsx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/preflight"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/selfsteal"
	"github.com/FlexEbat/RemnaForge/internal/txn"
	"github.com/FlexEbat/RemnaForge/internal/ui"
	"github.com/FlexEbat/RemnaForge/internal/validate"
	"github.com/FlexEbat/RemnaForge/internal/verify"
)

const nodeDir = "/opt/remnanode"

var ipv4RE = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)

// dockerComposeTemplate is the node's docker-compose.yml.
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
      build:
        context: .
        dockerfile_inline: |
          FROM caddy:2.11.2-builder AS builder
          RUN xcaddy build --with github.com/mastercactapus/caddy2-proxyprotocol
          FROM caddy:2.11.2
          COPY --from=builder /usr/bin/caddy /usr/bin/caddy
      image: remnaforge-caddy-proxyprotocol:2.11.2
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
        # Optional node settings (node 3.4+); uncomment to change:
        # - SNI_VERIFICATION=true
        # - NFTABLES_LOGGING=true
      volumes:
        - /dev/shm:/dev/shm:rw
        - caddy_data:/data:ro

volumes:
  caddy_data:
    name: caddy_data
    driver: local
    external: false
`

// nodeState carries the values gathered while prompting the user
// through to where the docker-compose.yml/Caddyfile are written.
type nodeState struct {
	selfstealDomain string
	panelIP         string
	certificate     string // joined with real newlines, see readCertificate()
}

func installNodeCaddy() (*nodeState, error) {
	if err := fsx.MkdirAll(nodeDir, 0755); err != nil {
		return nil, err
	}

	selfstealDomain := domain.Read("SELFSTEAL")

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
	if _, err := cfgfile.Write(filepath.Join(nodeDir, "docker-compose.yml"), dockerCompose, cfgfile.Opts{Kind: cfgfile.KindCompose}); err != nil {
		return nil, err
	}
	_ = cfgfile.EnsureGitignore(nodeDir)
	caddyfile, err := proxy.Must(proxy.Caddy).Node(proxy.Params{})
	if err != nil {
		return nil, err
	}
	if _, err := cfgfile.Write(filepath.Join(nodeDir, "Caddyfile"), caddyfile, cfgfile.Opts{Kind: cfgfile.KindCaddy}); err != nil {
		return nil, err
	}

	return &nodeState{
		selfstealDomain: selfstealDomain,
		panelIP:         panelIP,
		certificate:     certificate,
	}, nil
}

// isValidIPv4 checks the regex shape plus that each octet is <= 255.
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

// readCertificate reads pasted multi-line certificate/key content until
// a blank line following non-blank content, joining lines with real
// newlines.
func readCertificate() string {
	return ui.ReadSecretBlock(i18n.T("CERT_PROMPT"))
}

// InstallationNode installs a standalone Caddy-fronted node.
func InstallationNode() error {
	return txn.Run("caddynode", installationNode)
}

func installationNode() error {
	// EnsureInstalled bootstraps docker/certbot/ufw and other dependencies
	// if missing, so this doesn't just fail deep inside a later step with
	// a confusing error. certbot itself is unused by the Caddy flow, but
	// ufw and docker are needed the same as everywhere else, so this
	// still runs.
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING_NODE"), ui.ColorReset)

	state, err := installNodeCaddy()
	if err != nil {
		return err
	}

	// Port 80 for Caddy's certificates and the panel's access to the node API.
	// The operator confirms the rules; a refusal or a failure does not stop
	// the install.
	firewall.AllowRules(firewall.DefaultEnv(), ui.Reading, []firewall.Rule{firewall.HTTPRule(), firewall.NodePanelRule(state.panelIP)})

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_NODE"), ui.ColorReset)
	time.Sleep(3 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	if runctx.DryRun() {
		runctx.Say("stop here: the stack is not started in a dry run, so the steps that need the running services (account, keys, profile, node, host, subscription token, checks) are skipped")
		return nil
	}
	if err := validate.CheckStack(nodeDir); err != nil {
		return err
	}
	upCmd := execx.Command("docker", "compose", "up", "-d", "--build")
	upCmd.Dir = nodeDir
	if err := upCmd.Run(); err != nil {
		fmt.Printf("%sdocker compose up: %v%s\n", ui.ColorRed, err, ui.ColorReset)
		return err
	}

	// Install a random selfsteal template.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Wait for the selfsteal site, then run the post-install checks.
	fmt.Printf(ui.ColorYellow+i18n.T("NODE_CHECK")+ui.ColorReset+"\n", state.selfstealDomain)
	env := verify.DefaultEnv()
	if err := verify.WaitForNode(env, state.selfstealDomain); err != nil {
		return err
	}
	verify.Finish(env, verify.Plan{NodeDir: nodeDir, Selfsteal: []string{state.selfstealDomain}})

	return nil
}
