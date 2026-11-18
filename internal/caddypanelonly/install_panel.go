// Package caddypanelonly installs the Remnawave panel alone behind
// Caddy: it registers a node config profile and host for a selfsteal
// domain that runs on a separate server via internal/caddynode, not a
// co-located node. This mirrors internal/panelonly (nginx), the "panel
// alone" installer for the Nginx flow.
//
// Like internal/caddynode, this never runs certbot: Caddy issues and
// renews its own TLS certificate via its built-in ACME client.
package caddypanelonly

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/api"
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
// through to where docker-compose.yml/Caddyfile are written.
type panelState struct {
	panelDomain     string
	subDomain       string
	selfstealDomain string
	superadminUser  string
	superadminPass  string
	cookiesRandom1  string
	cookiesRandom2  string
	metricsUser     string
	metricsPass     string
	appSecret       string
}

// dotEnvTemplate is the panel's .env file. It uses a single APP_SECRET
// rather than separate JWT_AUTH_SECRET/JWT_API_TOKENS_SECRET, and drops
// the SWAGGER_PATH/SCALAR_PATH/IS_DOCS_ENABLED toggles, matching
// Remnawave Panel v3.2.0+.
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

// dockerComposeTemplate is the combined panel+node docker-compose.yml,
// with the same backend:2 -> backend:3 fix as dotEnvTemplate above
// (same rationale, same 1.1.1 precedent). Unlike internal/panelonly
// (nginx), this needs no head/tail split: there's no certbot step
// whose result gates any part of this file, so everything is known
// upfront and written in one pass, matching internal/caddynode's
// approach.
const dockerComposeTemplate = `x-common: &common
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

  remnawave-caddy:
      image: caddy:2.11.2
      container_name: remnawave-caddy
      hostname: remnawave-caddy
      <<: [*common, *logging]
      network_mode: host
      volumes:
          - ./Caddyfile:/etc/caddy/Caddyfile
          - /var/www/html:/var/www/html:ro
          - /dev/shm:/dev/shm:rw
          - caddy_data:/data
      command: sh -c 'rm -f /dev/shm/nginx.sock && caddy run --config /etc/caddy/Caddyfile --adapter caddyfile'
      environment:
          - PANEL_DOMAIN=%s
          - SUB_DOMAIN=%s
          - BACKEND_URL=127.0.0.1:3000
          - SUB_BACKEND_URL=127.0.0.1:3010
      healthcheck:
          test: ["CMD", "test", "-S", "/dev/shm/nginx.sock"]
          interval: 2s
          timeout: 5s
          retries: 15
          start_period: 5s

  remnawave-subscription-page:
    image: remnawave/subscription-page:latest
    container_name: remnawave-subscription-page
    hostname: remnawave-subscription-page
    <<: [*common, *logging, *networks]
    environment:
      - REMNAWAVE_PANEL_URL=http://remnawave:3000
      - APP_PORT=3010
      - REMNAWAVE_API_TOKEN=%s
    ports:
      - '127.0.0.1:3010:3010'
    depends_on:
      remnawave:
        condition: service_healthy

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
  caddy_data:
    name: caddy_data
    driver: local
    external: false
`

func installPanelCaddy() (*panelState, error) {
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

	// Unlike internal/panelonly (nginx), this validates the node/selfsteal
	// domain with domain.CheckDomain, not just a bare read.
	selfstealDomain := domain.Read("ENTER_NODE_DOMAIN")
	if domain.CheckDomain(selfstealDomain, true, false) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	if panelDomain == subDomain || panelDomain == selfstealDomain || subDomain == selfstealDomain {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DOMAINS_MUST_BE_UNIQUE"), ui.ColorReset)
		return nil, fmt.Errorf("domains must be unique")
	}

	state := &panelState{
		panelDomain:     panelDomain,
		subDomain:       subDomain,
		selfstealDomain: selfstealDomain,
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

	// The API token line starts out as a placeholder; api.CreateAPIToken
	// replaces its value once the panel has issued a token.
	dockerCompose := fmt.Sprintf(dockerComposeTemplate,
		state.panelDomain, state.subDomain, "$api_token",
	)
	if _, err := cfgfile.Write(filepath.Join(panelDir, "docker-compose.yml"), dockerCompose, cfgfile.Opts{Kind: cfgfile.KindCompose}); err != nil {
		return nil, err
	}
	_ = cfgfile.EnsureGitignore(panelDir)

	caddyfile, err := proxy.Must(proxy.Caddy).Panel(proxy.Params{Layout: proxy.Direct, CookieName: state.cookiesRandom1, CookieValue: state.cookiesRandom2})
	if err != nil {
		return nil, err
	}
	if _, err := cfgfile.Write(filepath.Join(panelDir, "Caddyfile"), caddyfile, cfgfile.Opts{Kind: cfgfile.KindCaddy}); err != nil {
		return nil, err
	}

	return state, nil
}

// InstallationPanelOnly installs the panel alone behind Caddy.
func InstallationPanelOnly() error {
	return txn.Run("caddypanelonly", installationPanelOnly)
}

func installationPanelOnly() error {
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	state, err := installPanelCaddy()
	if err != nil {
		return err
	}

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	_ = execx.Command("ufw", "allow", "80/tcp", "comment", "HTTP").Run()
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

	// Provision the panel through its API. The node is on another host, so
	// it is registered under its selfsteal name.
	if err := provision.Run(provision.Options{
		DomainURL: domainURL, TargetDir: targetDir,
		Username: state.superadminUser, Password: state.superadminPass,
		SelfstealDomain: state.selfstealDomain,
		Inbounds:        api.ConfigProfileInbounds{Raw: true},
		NodeAddress:     state.selfstealDomain,
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
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RELAUNCH_CMD"), ui.ColorReset)
	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("POST_PANEL_INSTRUCTION"), ui.ColorReset)

	return nil
}
