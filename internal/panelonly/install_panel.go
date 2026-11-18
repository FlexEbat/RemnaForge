// Package panelonly installs the Remnawave panel alone: it registers a
// node config profile and host for a selfsteal domain that runs on a
// separate server via the standalone install_node flow, not a
// co-located node. The "panel + node on one server" installer is
// internal/panelfull.
package panelonly

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/api"
	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
	"github.com/FlexEbat/RemnaForge/internal/domain"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/fsx"
	"github.com/FlexEbat/RemnaForge/internal/genutil"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/preflight"
	"github.com/FlexEbat/RemnaForge/internal/provision"
	"github.com/FlexEbat/RemnaForge/internal/proxy"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/txn"
	"github.com/FlexEbat/RemnaForge/internal/ui"
	"github.com/FlexEbat/RemnaForge/internal/validate"
	"github.com/FlexEbat/RemnaForge/internal/verify"
)

const panelDir = "/opt/remnawave"

// panelState carries the values gathered while prompting the user
// through to where docker-compose.yml/.env/nginx.conf are written.
type panelState struct {
	panelDomain     string
	subDomain       string
	selfstealDomain string
	panelBaseDomain string
	subBaseDomain   string
	superadminUser  string
	superadminPass  string
	cookiesRandom1  string
	cookiesRandom2  string
	metricsUser     string
	metricsPass     string
	appSecret       string
}

// dockerComposeHead is the first half of docker-compose.yml: everything
// up to and including the remnawave-nginx service's still-open
// volumes: list.
const composeBase = `x-common: &common
  ulimits:
    nofile:
      soft: 1048576
      hard: 1048576
  restart: always

x-networks: &networks
  networks:
    - remnawave-network

x-logging: &logging
  logging:
    driver: json-file
    options:
      max-size: 100m
      max-file: 5

x-env: &env
  env_file: .env

services:
  remnawave-db:
    image: postgres:18.3
    container_name: 'remnawave-db'
    hostname: remnawave-db
    <<: [*common, *logging, *env, *networks]
    environment:
      - POSTGRES_USER=${POSTGRES_USER}
      - POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
      - POSTGRES_DB=${POSTGRES_DB}
      - TZ=UTC
    ports:
      - '127.0.0.1:6767:5432'
    volumes:
      - remnawave-db-data:/var/lib/postgresql
    healthcheck:
      test: ['CMD-SHELL', 'pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}']
      interval: 3s
      timeout: 10s
      retries: 3

  remnawave:
    image: remnawave/backend:3
    container_name: remnawave
    hostname: remnawave
    <<: [*common, *logging, *env, *networks]
    volumes:
      - valkey-socket:/var/run/valkey
    ports:
      - '127.0.0.1:3000:${APP_PORT:-3000}'
      - '127.0.0.1:3001:${METRICS_PORT:-3001}'
    healthcheck:
      test: ['CMD-SHELL', 'curl -f http://localhost:${METRICS_PORT:-3001}/health']
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 30s
    depends_on:
      remnawave-db:
        condition: service_healthy
      remnawave-redis:
        condition: service_healthy

  remnawave-redis:
    image: valkey/valkey:9.0.3-alpine
    container_name: remnawave-redis
    hostname: remnawave-redis
    <<: [*common, *logging, *networks]
    volumes:
      - valkey-socket:/var/run/valkey
    command: >
      valkey-server
      --save ""
      --appendonly no
      --maxmemory-policy noeviction
      --loglevel warning
      --unixsocket /var/run/valkey/valkey.sock
      --unixsocketperm 777
      --port 0
    healthcheck:
      test: ['CMD', 'valkey-cli', '-s', '/var/run/valkey/valkey.sock', 'ping']
      interval: 3s
      timeout: 10s
      retries: 3

`

// nginxService is the nginx service with its volumes list still open.
const nginxService = `  remnawave-nginx:
    image: nginx:1.28
    container_name: remnawave-nginx
    hostname: remnawave-nginx
    <<: [*common, *logging]
    network_mode: host
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
`

// dockerComposeHead is the compose file up to and including the nginx
// service's still-open volumes list.
const dockerComposeHead = composeBase + nginxService

// dotEnvTemplate is the panel's .env file.
const dotEnvTemplate = `### APP ###
APP_PORT=3000
METRICS_PORT=3001

### API ###
API_INSTANCES=1

### DATABASE ###
DATABASE_URL="postgresql://postgres:postgres@remnawave-db:5432/postgres"

### REDIS ###
REDIS_SOCKET=/var/run/valkey/valkey.sock

### SECURITY ###
APP_SECRET=%s
JWT_AUTH_LIFETIME=168

### TELEGRAM NOTIFICATIONS ###
IS_TELEGRAM_NOTIFICATIONS_ENABLED=false
TELEGRAM_BOT_TOKEN=change_me
TELEGRAM_NOTIFY_USERS=change_me
TELEGRAM_NOTIFY_NODES=change_me
TELEGRAM_NOTIFY_CRM=change_me
TELEGRAM_NOTIFY_SERVICE=change_me
TELEGRAM_NOTIFY_TBLOCKER=change_me

### FRONT_END ###
FRONT_END_DOMAIN=%s

### SUBSCRIPTION PUBLIC DOMAIN ###
SUB_PUBLIC_DOMAIN=%s

### PROMETHEUS ###
METRICS_USER=%s
METRICS_PASS=%s

### Webhook configuration
WEBHOOK_ENABLED=false
WEBHOOK_URL=https://your-webhook-url.com/endpoint
WEBHOOK_SECRET_HEADER=vsmu67Kmg6R8FjIOF1WUY8LWBHie4scdEqrfsKmyf4IAf8dY3nFS0wwYHkhh6ZvQ

### Bandwidth usage reached notifications
BANDWIDTH_USAGE_NOTIFICATIONS_ENABLED=false
BANDWIDTH_USAGE_NOTIFICATIONS_THRESHOLD=[60, 80]

### Not connected users notification (webhook, telegram)
NOT_CONNECTED_USERS_NOTIFICATIONS_ENABLED=false
NOT_CONNECTED_USERS_NOTIFICATIONS_AFTER_HOURS=[6, 24, 48]

### CLOUDFLARE ###
CLOUDFLARE_TOKEN=ey...

### Database ###
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=postgres
`

func installPanelNginx() (*panelState, error) {
	if err := fsx.MkdirAll(panelDir, 0755); err != nil {
		return nil, err
	}

	panelDomain := domain.Read("ENTER_PANEL_DOMAIN")
	if domain.CheckDomain(panelDomain, true, true) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	subDomain := domain.Read("ENTER_SUB_DOMAIN")
	if domain.CheckDomain(subDomain, true, true) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	selfstealDomain := domain.Read("ENTER_NODE_DOMAIN")

	if panelDomain == subDomain || panelDomain == selfstealDomain || subDomain == selfstealDomain {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DOMAINS_MUST_BE_UNIQUE"), ui.ColorReset)
		return nil, fmt.Errorf("domains must be unique")
	}

	state := &panelState{
		panelDomain:     panelDomain,
		subDomain:       subDomain,
		selfstealDomain: selfstealDomain,
		panelBaseDomain: domain.ExtractDomain(panelDomain),
		subBaseDomain:   domain.ExtractDomain(subDomain),
		superadminUser:  genutil.GenerateUser(),
		superadminPass:  genutil.GeneratePassword(),
		cookiesRandom1:  cfgfile.StableOr("remnawave.cookie1", genutil.GenerateUser),
		cookiesRandom2:  cfgfile.StableOr("remnawave.cookie2", genutil.GenerateUser),
		metricsUser:     cfgfile.StableOr("remnawave.metrics_user", genutil.GenerateUser),
		metricsPass:     cfgfile.StableOr("remnawave.metrics_pass", func() string { return genutil.GenerateAlnumSecret(32) }),
		appSecret:       cfgfile.StableOr("remnawave.app_secret", func() string { return genutil.GenerateAlnumSecret(64) }),
	}

	dotEnv := fmt.Sprintf(dotEnvTemplate,
		state.appSecret,
		state.panelDomain, state.subDomain,
		state.metricsUser, state.metricsPass,
	)
	dotEnv = genutil.HardenEnv(dotEnv)
	if dockercheck.VolumeExists("remnawave-db-data") {
		fmt.Printf("%s"+i18n.T("DB_VOLUME_EXISTS")+"%s\n", ui.ColorRed, "remnawave-db-data", ui.ColorReset)
		return nil, fmt.Errorf("existing database volume")
	}
	if _, err := cfgfile.Write(filepath.Join(panelDir, ".env"), dotEnv, cfgfile.Opts{Kind: cfgfile.KindEnv}); err != nil {
		return nil, err
	}

	return state, nil
}

// composeTail is the subscription page and the networks and volumes.
const composeTail = `
  remnawave-subscription-page:
    image: remnawave/subscription-page:latest
    container_name: remnawave-subscription-page
    hostname: remnawave-subscription-page
    <<: [*common, *logging, *networks]
    depends_on:
      remnawave:
        condition: service_healthy
    environment:
      - REMNAWAVE_PANEL_URL=http://remnawave:3000
      - APP_PORT=3010
      - REMNAWAVE_API_TOKEN=
    ports:
      - '127.0.0.1:3010:3010'

networks:
  remnawave-network:
    name: remnawave-network
    driver: bridge
    external: false

volumes:
  remnawave-db-data:
    driver: local
    external: false
    name: remnawave-db-data
  valkey-socket:
    name: valkey-socket
    driver: local
    external: false
`

// buildCompose assembles docker-compose.yml: the common services, the proxy
// (nginx continues the open volumes list of its service after the
// certificate mounts the certificate step returned, HAProxy owns port 443
// and mounts the bundles), the subscription page, the networks and volumes.
func buildCompose(kind proxy.Kind, certMounts string) string {
	if kind == proxy.HAProxy {
		return composeBase + proxy.HAProxyServices(certs.HAProxyBundleDir, proxy.Direct) + composeTail
	}
	return dockerComposeHead + certMounts + composeTail
}

// InstallationPanelOnly installs the panel alone behind nginx.
func InstallationPanelOnly() error {
	return txn.Run("panelonly", func() error { return installationPanelOnly(proxy.Nginx) })
}

// InstallationPanelOnlyHAProxy installs the panel alone with HAProxy on
// port 443. Its nodes run on other hosts and use TLS profiles.
func InstallationPanelOnlyHAProxy() error {
	return txn.Run("panelonly-haproxy", func() error { return installationPanelOnly(proxy.HAProxy) })
}

func installationPanelOnly(kind proxy.Kind) error {
	// EnsureInstalled bootstraps docker/certbot/ufw and other dependencies
	// if missing, so this doesn't just fail deep inside a later step with
	// a confusing error.
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	state, err := installPanelNginx()
	if err != nil {
		return err
	}

	// certs.HandleCertificates returns the actual method it used
	// (wildcard vs per-domain), which is what determines whether the
	// cert-domain fields below should be the base domain or the full
	// domain - using that returned value directly instead of
	// re-deriving it from scratch avoids the two ever disagreeing (see
	// internal/nginxnode for a fuller walkthrough of the same pattern).
	certResult, certErr := certs.HandleCertificatesFor(kind,
		[]string{state.panelDomain, state.subDomain}, "", "", panelDir)
	if certErr != nil {
		return certErr
	}
	certMethod := certResult.Method

	var panelCertDomain, subCertDomain string
	if certMethod == "1" {
		panelCertDomain = state.panelBaseDomain
		subCertDomain = state.subBaseDomain
	} else {
		panelCertDomain = state.panelDomain
		subCertDomain = state.subDomain
	}

	composeText := buildCompose(kind, certResult.ComposeMounts)
	composePath := filepath.Join(panelDir, "docker-compose.yml")
	if _, err := cfgfile.Write(composePath, composeText, cfgfile.Opts{Kind: cfgfile.KindCompose}); err != nil {
		return err
	}
	_ = cfgfile.EnsureGitignore(filepath.Dir(composePath))

	// The proxy's configuration.
	be := proxy.Must(kind)
	params := proxy.Params{
		Layout:      proxy.Direct,
		CookieName:  state.cookiesRandom1,
		CookieValue: state.cookiesRandom2,
		PanelDomain: state.panelDomain, PanelCert: panelCertDomain,
		SubDomain: state.subDomain, SubCert: subCertDomain,
	}
	proxyConf, err := be.Panel(params)
	if err != nil {
		return err
	}
	confKind := cfgfile.KindNginx
	if kind == proxy.HAProxy {
		confKind = cfgfile.KindHAProxy
	}
	if _, err := cfgfile.Write(filepath.Join(panelDir, be.ConfigFile()), proxyConf, cfgfile.Opts{Kind: confKind}); err != nil {
		return err
	}

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	if runctx.DryRun() {
		runctx.Say("stop here: the stack is not started in a dry run, so the steps that need the running services (account, keys, profile, node, host, subscription token, checks) are skipped")
		return nil
	}
	if err := validate.CheckStack(panelDir); err != nil {
		return err
	}
	upCmd := execx.Command("docker", "compose", "up", "-d")
	upCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	if err := upCmd.Run(); err != nil {
		fmt.Printf("%sdocker compose up: %v%s\n", ui.ColorRed, err, ui.ColorReset)
		return err
	}

	domainURL := "127.0.0.1:3000"
	targetDir := panelDir
	api.PanelDomain = state.panelDomain

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REGISTERING_REMNAWAVE"), ui.ColorReset)

	// Wait for the panel API to answer.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CONTAINERS"), ui.ColorReset)
	if err := verify.WaitForApp(verify.DefaultEnv()); err != nil {
		return err
	}

	// Provision the panel through its API. With HAProxy the profile carries
	// the XHTTP TLS inbound instead of Reality; Hysteria2 waits for the node
	// profile menu, which asks for the node's certificate. The node is on
	// another host, so it is registered under its selfsteal name.
	profileTag, profileInbounds := "", api.ConfigProfileInbounds{Raw: true}
	if kind == proxy.HAProxy {
		profileTag, profileInbounds = "XHTTP-TLS", api.ConfigProfileInbounds{XHTTP: true}
	}
	if err := provision.Run(provision.Options{
		DomainURL: domainURL, TargetDir: targetDir,
		Username: state.superadminUser, Password: state.superadminPass,
		SelfstealDomain: state.selfstealDomain,
		ProfileTag:      profileTag, Inbounds: profileInbounds,
		NodeAddress: state.selfstealDomain,
	}); err != nil {
		return err
	}

	// Restart the subscription page so it picks up the token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STOPPING_REMNAWAVE_SUBSCRIPTION_PAGE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	downSub := execx.Command("docker", "compose", "down", "remnawave-subscription-page")
	downSub.Dir = panelDir
	_ = downSub.Run()

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_REMNAWAVE_SUBSCRIPTION_PAGE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	if err := validate.CheckStack(panelDir); err != nil {
		return err
	}
	upSub := execx.Command("docker", "compose", "up", "-d", "remnawave-subscription-page")
	upSub.Dir = panelDir
	if err := upSub.Run(); err != nil {
		fmt.Printf("%sdocker compose up: %v%s\n", ui.ColorRed, err, ui.ColorReset)
		return err
	}

	// Final summary screen.
	verify.Finish(verify.DefaultEnv(), verify.Plan{PanelDir: panelDir, Domains: []string{state.panelDomain, state.subDomain}})

	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("INSTALL_COMPLETE"), ui.ColorReset)
	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PANEL_ACCESS"), ui.ColorReset)
	fmt.Printf("%shttps://%s/auth/login?%s=%s%s\n", ui.ColorWhite, state.panelDomain, state.cookiesRandom1, state.cookiesRandom2, ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADMIN_CREDS"), ui.ColorReset)
	fmt.Printf("%s%s %s%s%s\n", ui.ColorYellow, i18n.T("USERNAME"), ui.ColorWhite, state.superadminUser, ui.ColorReset)
	fmt.Printf("%s%s %s%s%s\n", ui.ColorYellow, i18n.T("PASSWORD"), ui.ColorWhite, state.superadminPass, ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("POST_PANEL_INSTRUCTION"), ui.ColorReset)

	return nil
}
