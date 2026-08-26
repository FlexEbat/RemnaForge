// Package caddypanelonly is a port of src/caddy/install_panel.sh (465
// lines). Despite the filename, this file's own header comment says
// "Module: Install Panel" and its function is install_panel_caddy(). It
// installs the Remnawave panel alone behind Caddy: it registers a node
// config profile and host for a selfsteal domain that runs on a separate
// server via internal/caddynode, not a co-located node. This mirrors
// internal/panelonly (nginx), which is the "panel alone" installer for
// src/nginx/install_panel_node.sh.
//
// The naming confusion here runs the OPPOSITE direction from nginx: for
// nginx, install_panel_node.sh is panel-only and install_panel.sh is
// panel+node (see internal/panelonly's and internal/panelfull's package
// comments). For Caddy, install_panel.sh (this file, panel-only) and
// install_panel_node.sh (panel+node, see internal/caddypanelfull) are
// each named for what they'd naively seem to do, not swapped. Verified
// by downloading and reading both files directly, not by assuming the
// nginx pattern carries over. Go package names describe content either
// way, per project convention.
//
// Like internal/caddynode, this never calls internal/certs: Caddy issues
// and renews its own TLS certificate via its built-in ACME client, so
// there's no certbot step anywhere in this package.
package caddypanelonly

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/api"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/genutil"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const panelDir = "/opt/remnawave"

// panelState carries values threaded between install_panel_caddy() and
// installation_panel_caddy() in the original (all globals there).
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

// dotEnvTemplate mirrors install_panel.sh:49-148, with one intentional
// deviation from that file's literal current content, applied
// proactively rather than discovered as a live bug:
//
// BUG FIX (not a 1:1 port): the upstream src/caddy/install_panel.sh
// still writes JWT_AUTH_SECRET/JWT_API_TOKENS_SECRET plus
// SWAGGER_PATH/SCALAR_PATH/IS_DOCS_ENABLED, and still pins
// remnawave/backend:2 below. That was correct for Remnawave Panel
// pre-3.2.0, but internal/panelfull and internal/panelonly (nginx)
// already had to fix this exact set of fields for the 1.1.1 release
// (see TECH.md changelog) because the panel itself moved on:
// JWT_AUTH_SECRET/JWT_API_TOKENS_SECRET were replaced by a single
// APP_SECRET, and those Swagger/Scalar/docs toggles were removed
// entirely. The Caddy source files just haven't caught up on GitHub
// yet. Shipping a new install flow that writes fields the current
// panel image ignores (and omits the one it now requires) would mean
// building something already known to be broken on day one, so this
// applies the same fix here instead of reproducing the stale upstream
// literally. See internal/api/panelv3_test.go for the regression test
// covering the panel-side API changes this depends on.
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

// dockerComposeTemplate mirrors install_panel.sh:150-295, with the same
// backend:2 -> backend:3 fix as dotEnvTemplate above (same rationale,
// same 1.1.1 precedent). Unlike internal/panelonly (nginx), this needs
// no head/tail split: there's no certbot step whose result gates any
// part of this file, so everything is known upfront and written in one
// pass, matching internal/caddynode's approach.
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

// caddyfileTemplate mirrors install_panel.sh:297-366. {$PANEL_DOMAIN},
// {$SUB_DOMAIN}, {$BACKEND_URL}, {$SUB_BACKEND_URL} are Caddy's own
// environment-variable placeholders (resolved by Caddy at runtime from
// the container environment set in dockerComposeTemplate above), so they
// stay as literal text here, same as caddynode's Caddyfile. cookies_random1/
// cookies_random2, by contrast, are NOT escaped in the original's heredoc
// (no backslash before their `$`), so bash substitutes the actual
// generated cookie name/value directly into the file at creation time.
// Go mirrors that with %s placeholders instead of Caddy env-refs.
const caddyfileTemplate = `{
    admin off
}

http://{$PANEL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$PANEL_DOMAIN}{uri} permanent
}

https://{$PANEL_DOMAIN} {

    @has_token_param {
        query %[1]s=%[2]s
    }

    handle @has_token_param {
        header +Set-Cookie "%[1]s=%[2]s; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=2592000"
    }

    @unauthorized {
        not path /oauth2/*
        not header Cookie *%[1]s=%[2]s*
        not query %[1]s=%[2]s
    }

    handle @unauthorized {
        abort
    }

    @oauth2_bad {
        path /oauth2/*
        not header Referer https://oauth.telegram.org/*
    }

    handle @oauth2_bad {
        abort
    }

    @oauth2 {
        path /oauth2/*
        header Referer https://oauth.telegram.org/*
    }

    handle @oauth2 {
        reverse_proxy {$BACKEND_URL} {
            header_up Host {host}
        }
    }

    reverse_proxy {$BACKEND_URL} {
        header_up X-Real-IP {remote}
        header_up Host {host}
    }
}

https://{$SUB_DOMAIN} {
    handle {
        reverse_proxy {$SUB_BACKEND_URL} {
            header_up X-Real-IP {remote}
            header_up Host {host}
        }
    }
}

:80 {
    bind 0.0.0.0
    respond 204
}
`

// Original bash (src/caddy/install_panel.sh:4-367): install_panel_caddy().
func installPanelCaddy() (*panelState, error) {
	if err := os.MkdirAll(panelDir, 0755); err != nil {
		return nil, err
	}

	panelDomain := ui.Reading(i18n.T("ENTER_PANEL_DOMAIN"))
	if domain.CheckDomain(panelDomain, true, true) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	subDomain := ui.Reading(i18n.T("ENTER_SUB_DOMAIN"))
	if domain.CheckDomain(subDomain, true, true) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	// Unlike internal/panelonly (nginx), the original here DOES validate
	// the node/selfsteal domain with check_domain (install_panel.sh:23-29),
	// not just a bare read. Confirmed by reading the actual file, not
	// assumed from the nginx pattern.
	selfstealDomain := ui.Reading(i18n.T("ENTER_NODE_DOMAIN"))
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
		cookiesRandom1:  genutil.GenerateUser(),
		cookiesRandom2:  genutil.GenerateUser(),
		metricsUser:     genutil.GenerateUser(),
		metricsPass:     genutil.GenerateUser(),
		appSecret:       genutil.GenerateAlnumSecret(64),
	}

	dotEnv := fmt.Sprintf(dotEnvTemplate,
		state.appSecret,
		state.panelDomain, state.subDomain,
		state.metricsUser, state.metricsPass,
	)
	if err := os.WriteFile(filepath.Join(panelDir, ".env"), []byte(dotEnv), 0600); err != nil {
		return nil, err
	}

	// The original's REMNAWAVE_API_TOKEN placeholder is the literal text
	// "$api_token" (line 269: `REMNAWAVE_API_TOKEN=\$api_token`, the
	// backslash keeps bash from expanding an unset variable, so this
	// exact string ends up in the file). api.CreateAPIToken's
	// replaceLineInFile matches by line prefix "REMNAWAVE_API_TOKEN=" and
	// replaces the whole line regardless of what follows, so reproducing
	// this exact placeholder text (rather than nginx panelonly/panelfull's
	// empty placeholder) works identically either way. Kept literal here
	// for a faithful port.
	dockerCompose := fmt.Sprintf(dockerComposeTemplate,
		state.panelDomain, state.subDomain, "$api_token",
	)
	if err := os.WriteFile(filepath.Join(panelDir, "docker-compose.yml"), []byte(dockerCompose), 0644); err != nil {
		return nil, err
	}

	caddyfile := fmt.Sprintf(caddyfileTemplate, state.cookiesRandom1, state.cookiesRandom2)
	if err := os.WriteFile(filepath.Join(panelDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
		return nil, err
	}

	return state, nil
}

// InstallationPanelOnly is the Go equivalent of
// src/caddy/install_panel.sh:369-466: installation_panel_caddy().
func InstallationPanelOnly() error {
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	state, err := installPanelCaddy()
	if err != nil {
		return err
	}

	// Lines 372-378: bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	_ = exec.Command("ufw", "allow", "80/tcp", "comment", "HTTP").Run()
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = upCmd.Run()

	domainURL := "127.0.0.1:3000"
	targetDir := panelDir
	api.PanelDomain = state.panelDomain

	// Lines 383-384.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REGISTERING_REMNAWAVE"), ui.ColorReset)
	time.Sleep(20 * time.Second)

	// Lines 386-399: wait for the panel API to answer.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CHECK_CONTAINERS"), ui.ColorReset)
	const maxAttempts = 5
	client := &http.Client{Timeout: 30 * time.Second}
	for attempt := 1; ; {
		req, _ := http.NewRequest("GET", "http://"+domainURL+"/api/auth/status", nil)
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		req.Header.Set("X-Forwarded-Proto", "https")
		resp, reqErr := client.Do(req)
		if reqErr == nil && resp.StatusCode < 400 {
			if resp.Body != nil {
				resp.Body.Close()
			}
			break
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if attempt >= maxAttempts {
			return fmt.Errorf(i18n.T("CONTAINERS_TIMEOUT"), maxAttempts)
		}
		fmt.Printf(ui.ColorRed+i18n.T("CONTAINERS_NOT_READY_ATTEMPT")+ui.ColorReset+"\n", attempt, maxAttempts)
		time.Sleep(60 * time.Second)
		attempt++
	}

	// Line 402: register the superadmin account.
	token := api.RegisterRemnawave(domainURL, state.superadminUser, state.superadminPass, "")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REGISTRATION_SUCCESS"), ui.ColorReset)

	// Lines 406-409: generate xray keys. No GetPublicKey call here (unlike
	// internal/caddypanelfull): this variant has no co-located remnanode
	// container to patch a SECRET_KEY into, matching internal/panelonly.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Line 412: delete the default config profile.
	_ = api.DeleteConfigProfile(domainURL, token, "")

	// Lines 415-417: create our own config profile.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, "StealConfig", state.selfstealDomain, privateKey, "")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	// Lines 420-421: create the node, passing the selfsteal domain as its
	// address (line 421: `create_node ... "$SELFSTEAL_DOMAIN"`), same as
	// internal/panelonly.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_NODE"), ui.ColorReset)
	api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, state.selfstealDomain, "")

	// Lines 424-425: create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	api.CreateHost(domainURL, token, inboundUUID, state.selfstealDomain, configProfileUUID, "")

	// Lines 428-433: default squad.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_DEFAULT_SQUAD"), ui.ColorReset)
	squadUUIDs, _ := api.GetDefaultSquad(domainURL, token)
	if len(squadUUIDs) > 0 {
		_ = api.UpdateSquad(domainURL, token, squadUUIDs[0], inboundUUID)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SQUAD"), ui.ColorReset)

	// Lines 436-437: subscription-page API token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_API_TOKEN"), ui.ColorReset)
	_ = api.CreateAPIToken(domainURL, token, targetDir, "")

	// Lines 440-448: restart the subscription page so it picks up the token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STOPPING_REMNAWAVE_SUBSCRIPTION_PAGE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	downSub := exec.Command("docker", "compose", "down", "remnawave-subscription-page")
	downSub.Dir = panelDir
	_ = downSub.Run()

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_REMNAWAVE_SUBSCRIPTION_PAGE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	upSub := exec.Command("docker", "compose", "up", "-d", "remnawave-subscription-page")
	upSub.Dir = panelDir
	_ = upSub.Run()

	// Lines 452-465: final summary screen.
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
