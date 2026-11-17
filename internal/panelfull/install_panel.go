// Package panelfull installs the Remnawave panel and a co-located
// selfsteal node together: a real remnanode container, unix-socket
// Nginx with proxy_protocol, and a random selfsteal template on one
// server. internal/panelonly is the "panel alone" installer.
package panelfull

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
	"github.com/FlexEbat/RemnaForge/internal/proxy"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/selfsteal"
	"github.com/FlexEbat/RemnaForge/internal/txn"
	"github.com/FlexEbat/RemnaForge/internal/ui"
	"github.com/FlexEbat/RemnaForge/internal/validate"
	"github.com/FlexEbat/RemnaForge/internal/verify"
)

const panelDir = "/opt/remnawave"

// state carries the values gathered while prompting the user through
// to where docker-compose.yml/.env/nginx.conf are written.
type state struct {
	panelDomain         string
	subDomain           string
	selfstealDomain     string
	panelBaseDomain     string
	subBaseDomain       string
	selfstealBaseDomain string
	superadminUser      string
	superadminPass      string
	cookiesRandom1      string
	cookiesRandom2      string
	metricsUser         string
	metricsPass         string
	appSecret           string
}

// dockerComposeHead is the first half of docker-compose.yml, everything
// up to and including remnawave-nginx's still-open volumes: list.
// Identical to panelonly's, since both share this same preamble
// verbatim.
// composeBase is the compose file up to the proxy service: the database,
// the panel and the cache.
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

// dotEnvTemplate is the panel's .env file, identical in content to
// panelonly's.
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

func installPanelNodeNginx() (*state, error) {
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
	if domain.CheckDomain(selfstealDomain, true, false) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	if panelDomain == subDomain || panelDomain == selfstealDomain || subDomain == selfstealDomain {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DOMAINS_MUST_BE_UNIQUE"), ui.ColorReset)
		return nil, fmt.Errorf("domains must be unique")
	}

	st := &state{
		panelDomain:         panelDomain,
		subDomain:           subDomain,
		selfstealDomain:     selfstealDomain,
		panelBaseDomain:     domain.ExtractDomain(panelDomain),
		subBaseDomain:       domain.ExtractDomain(subDomain),
		selfstealBaseDomain: domain.ExtractDomain(selfstealDomain),
		superadminUser:      genutil.GenerateUser(),
		superadminPass:      genutil.GeneratePassword(),
		cookiesRandom1:      cfgfile.StableOr("remnawave.cookie1", genutil.GenerateUser),
		cookiesRandom2:      cfgfile.StableOr("remnawave.cookie2", genutil.GenerateUser),
		metricsUser:         cfgfile.StableOr("remnawave.metrics_user", genutil.GenerateUser),
		metricsPass:         cfgfile.StableOr("remnawave.metrics_pass", genutil.GenerateUser),
		appSecret:           cfgfile.StableOr("remnawave.app_secret", func() string { return genutil.GenerateAlnumSecret(64) }),
	}

	dotEnv := fmt.Sprintf(dotEnvTemplate,
		st.appSecret,
		st.panelDomain, st.subDomain,
		st.metricsUser, st.metricsPass,
	)
	dotEnv = genutil.HardenEnv(dotEnv)
	if dockercheck.VolumeExists("remnawave-db-data") {
		fmt.Printf("%s"+i18n.T("DB_VOLUME_EXISTS")+"%s\n", ui.ColorRed, "remnawave-db-data", ui.ColorReset)
		return nil, fmt.Errorf("existing database volume")
	}
	if _, err := cfgfile.Write(filepath.Join(panelDir, ".env"), dotEnv, cfgfile.Opts{Kind: cfgfile.KindEnv}); err != nil {
		return nil, err
	}
	return st, nil
}

// nginxTail finishes the nginx service: the shared directories and the command.
const nginxTail = `      - /dev/shm:/dev/shm:rw
      - /var/www/html:/var/www/html:ro
    command: sh -c 'rm -f /dev/shm/nginx.sock && exec nginx -g "daemon off;"'
`

// buildCompose assembles docker-compose.yml: the common services, the proxy
// (nginx continues the open volumes list of its service, HAProxy replaces
// it and adds the selfsteal site), the subscription page and the node, which
// mounts the certificate files for its Hysteria2 inbound. certMounts are the
// nginx volume lines the certificate step returned; they belong right after
// the nginx service's open volumes list.
func buildCompose(kind proxy.Kind, certMounts, certFullchain, certPrivkey string) string {
	head, proxyPart := dockerComposeHead, nginxTail
	if kind == proxy.HAProxy {
		head, proxyPart = composeBase, proxy.HAProxyServices(certs.HAProxyBundleDir, proxy.Socket)
	}
	return head + certMounts + proxyPart + fmt.Sprintf(`
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

  remnanode:
    image: remnawave/node:latest
    container_name: remnanode
    hostname: remnanode
    <<: [*common, *logging]
    depends_on:
      remnawave:
        condition: service_healthy
    network_mode: host
    environment:
      - NODE_PORT=2222
      - SECRET_KEY="PUBLIC KEY FROM REMNAWAVE-PANEL"
      # Optional node settings (node 3.4+); uncomment to change:
      # - SNI_VERIFICATION=true
      # - NFTABLES_LOGGING=true
    volumes:
      - /dev/shm:/dev/shm:rw
      - %s:%s:ro
      - %s:%s:ro

networks:
  remnawave-network:
    name: remnawave-network
    driver: bridge
    ipam:
      config:
        - subnet: 172.30.0.0/16
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
`, certFullchain, certFullchain, certPrivkey, certPrivkey)
}

// InstallationPanelNode installs the panel and a node on one host behind
// nginx.
func InstallationPanelNode() error {
	return txn.Run("panelfull", func() error { return installationPanelNode(proxy.Nginx) })
}

// InstallationPanelNodeHAProxy is the same install behind HAProxy: every
// name is routed by SNI and served over TLS that HAProxy terminates.
func InstallationPanelNodeHAProxy() error {
	return txn.Run("panelfull-haproxy", func() error { return installationPanelNode(proxy.HAProxy) })
}

func installationPanelNode(kind proxy.Kind) error {
	// EnsureInstalled bootstraps docker/certbot/ufw and other dependencies
	// if missing, so this doesn't just fail deep inside a later step with
	// a confusing error.
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALLING"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	st, err := installPanelNodeNginx()
	if err != nil {
		return err
	}

	// certs.HandleCertificates returns the actual method it used
	// (wildcard vs per-domain), which is what determines whether the
	// cert-domain fields below should be the base domain or the full
	// domain - using that returned value directly instead of
	// re-deriving it from scratch avoids the two ever disagreeing.
	certResult, certErr := certs.HandleCertificatesFor(kind,
		[]string{st.panelDomain, st.subDomain, st.selfstealDomain}, "", "", panelDir)
	if certErr != nil {
		return certErr
	}
	certMethod := certResult.Method

	var panelCertDomain, subCertDomain, nodeCertDomain string
	if certMethod == "1" {
		panelCertDomain = st.panelBaseDomain
		subCertDomain = st.subBaseDomain
		nodeCertDomain = st.selfstealBaseDomain
	} else {
		panelCertDomain = st.panelDomain
		subCertDomain = st.subDomain
		nodeCertDomain = st.selfstealDomain
	}

	certFullchain, certPrivkey := certs.NginxCertPaths(nodeCertDomain)

	composeText := buildCompose(kind, certResult.ComposeMounts, certFullchain, certPrivkey)
	composePath := filepath.Join(panelDir, "docker-compose.yml")
	if _, err := cfgfile.Write(composePath, composeText, cfgfile.Opts{Kind: cfgfile.KindCompose}); err != nil {
		return err
	}
	_ = cfgfile.EnsureGitignore(filepath.Dir(composePath))

	// The proxy's configuration and any extra file of its stack.
	be := proxy.Must(kind)
	params := proxy.Params{
		Layout:      proxy.Socket,
		CookieName:  st.cookiesRandom1,
		CookieValue: st.cookiesRandom2,
		PanelDomain: st.panelDomain, PanelCert: panelCertDomain,
		SubDomain: st.subDomain, SubCert: subCertDomain,
		SelfstealDomain: st.selfstealDomain, SelfstealCert: nodeCertDomain,
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
	for name, content := range be.Extra(params) {
		if _, err := cfgfile.Write(filepath.Join(panelDir, name), content, cfgfile.Opts{Kind: cfgfile.KindRaw}); err != nil {
			return err
		}
	}

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
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

	// Allow the co-located node's docker subnet to reach itself on 2222
	// (best-effort: nice-to-have hardening, not worth failing the whole
	// install over).
	_ = execx.Command("ufw", "allow", "from", "172.30.0.0/16", "to", "any", "port", "2222", "proto", "tcp").Run()

	domainURL := "127.0.0.1:3000"
	targetDir := panelDir
	api.PanelDomain = st.panelDomain

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REGISTERING_REMNAWAVE"), ui.ColorReset)

	// Wait for the panel API to answer.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CONTAINERS"), ui.ColorReset)
	if err := verify.WaitForApp(verify.DefaultEnv()); err != nil {
		return err
	}

	// Register the superadmin account.
	token := api.RegisterRemnawave(domainURL, st.superadminUser, st.superadminPass, "")
	if token == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_REGISTER"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REGISTRATION_SUCCESS"), ui.ColorReset)

	// Fetch the real public key and patch it into
	// docker-compose.yml's remnanode SECRET_KEY. This is the key
	// difference from panelonly: the node is co-located here, so it
	// needs its real key, not just a config-profile record.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_PUBLIC_KEY"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	if err := api.GetPublicKey(domainURL, token, targetDir); err != nil {
		return err
	}

	// Generate xray keys.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	if privateKey == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_GENERATE_KEYS"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Delete the default config profile.
	_ = api.DeleteConfigProfile(domainURL, token, "")

	// Create our config profile. Behind HAProxy there is no Reality: the
	// profile carries the TLS inbounds, XHTTP through the SNI routing and
	// Hysteria2 on UDP 443.
	profileTag, profileInbounds := "", api.ConfigProfileInbounds{Raw: true}
	if kind == proxy.HAProxy {
		profileTag, profileInbounds = "XHTTP-TLS", api.ConfigProfileInbounds{XHTTP: true, Hysteria2: true}
	}
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, "StealConfig", st.selfstealDomain, privateKey, profileTag, certFullchain, certPrivkey, profileInbounds)
	if configProfileUUID == "" || inboundUUID == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_CREATE_CONFIG_PROFILE"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	// Create the node without node_address/node_name overrides, unlike
	// panelonly (which passes SELFSTEAL_DOMAIN as the name), so it gets
	// CreateNode's own defaults (172.30.0.1 / "Steal"). Those defaults
	// match the co-located node's actual docker-network address.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_NODE"), ui.ColorReset)
	if err := api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, "", ""); err != nil {
		return err
	}

	// Create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	if err := api.CreateHost(domainURL, token, inboundUUID, st.selfstealDomain, configProfileUUID, ""); err != nil {
		return err
	}

	// Default squad.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_DEFAULT_SQUAD"), ui.ColorReset)
	squadUUIDs, _ := api.GetDefaultSquad(domainURL, token)
	if len(squadUUIDs) > 0 {
		_ = api.UpdateSquad(domainURL, token, squadUUIDs[0], inboundUUID)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SQUAD"), ui.ColorReset)

	// Subscription-page API token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_API_TOKEN"), ui.ColorReset)
	if err := api.CreateAPIToken(domainURL, token, targetDir, ""); err != nil {
		return err
	}

	// Restart the whole stack so remnanode picks up its
	// real SECRET_KEY and the subscription page picks up its API token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STOPPING_REMNAWAVE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	downCmd := execx.Command("docker", "compose", "down")
	downCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = downCmd.Run()

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	if err := validate.CheckStack(panelDir); err != nil {
		return err
	}
	upCmd2 := execx.Command("docker", "compose", "up", "-d")
	upCmd2.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	if err := upCmd2.Run(); err != nil {
		fmt.Printf("%sdocker compose up: %v%s\n", ui.ColorRed, err, ui.ColorReset)
		return err
	}

	// Install a random selfsteal template for the co-located node.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Final summary screen.
	verify.Finish(verify.DefaultEnv(), verify.Plan{PanelDir: panelDir, Domains: []string{st.panelDomain, st.subDomain}, Selfsteal: []string{st.selfstealDomain}})

	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("INSTALL_COMPLETE"), ui.ColorReset)
	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PANEL_ACCESS"), ui.ColorReset)
	fmt.Printf("%shttps://%s/auth/login?%s=%s%s\n", ui.ColorWhite, st.panelDomain, st.cookiesRandom1, st.cookiesRandom2, ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADMIN_CREDS"), ui.ColorReset)
	fmt.Printf("%s%s %s%s%s\n", ui.ColorYellow, i18n.T("USERNAME"), ui.ColorWhite, st.superadminUser, ui.ColorReset)
	fmt.Printf("%s%s %s%s%s\n", ui.ColorYellow, i18n.T("PASSWORD"), ui.ColorWhite, st.superadminPass, ui.ColorReset)
	fmt.Printf("%s-------------------------------------------------%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RELAUNCH_CMD"), ui.ColorReset)
	fmt.Printf("%s=================================================%s\n", ui.ColorYellow, ui.ColorReset)

	return nil
}
