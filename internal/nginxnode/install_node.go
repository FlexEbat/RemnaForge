// Package nginxnode installs a standalone Remnawave node behind Nginx
// (selfsteal reverse proxy on a Unix socket, remnanode container
// alongside it).
package nginxnode

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/domain"
	"github.com/FlexEbat/RemnaForge/internal/execx"
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

	selfstealBaseDomain := domain.ExtractDomain(selfstealDomain)

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
	return txn.Run("nginxnode", installationNode)
}

func installationNode() error {
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
      # Optional node settings (node 3.4+); uncomment to change:
      # - SNI_VERIFICATION=true
      # - NFTABLES_LOGGING=true
    volumes:
      - /dev/shm:/dev/shm:rw
      - %s:%s:ro
      - %s:%s:ro
`, state.certificate, certFullchain, certFullchain, certPrivkey, certPrivkey)

	composePath := filepath.Join(nodeDir, "docker-compose.yml")
	if _, err := cfgfile.Write(composePath, dockerComposeHead+composeTail, cfgfile.Opts{Kind: cfgfile.KindCompose}); err != nil {
		return err
	}
	_ = cfgfile.EnsureGitignore(filepath.Dir(composePath))

	// Nginx.conf.
	nginxConf, err := proxy.Must(proxy.Nginx).Node(proxy.Params{
		SelfstealDomain: state.selfstealDomain, SelfstealCert: nodeCertDomain,
	})
	if err != nil {
		return err
	}

	if _, err := cfgfile.Write(filepath.Join(nodeDir, "nginx.conf"), nginxConf, cfgfile.Opts{Kind: cfgfile.KindNginx}); err != nil {
		return err
	}

	// Ufw (best-effort: this is a nice-to-have hardening step, not worth
	// failing the whole install over).
	_ = execx.Command("ufw", "allow", "from", state.panelIP, "to", "any", "port", "2222").Run()
	_ = execx.Command("ufw", "reload").Run()

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
	upCmd := execx.Command("docker", "compose", "up", "-d")
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
