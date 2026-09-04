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
// Remnawave Panel v3.2.0+ (see internal/panelfull's and
// internal/panelonly's (nginx) equivalent templates and TECH.md's
// changelog for the same change made there). See
// internal/api/panelv3_test.go for the regression test covering the
// panel-side API changes this depends on.
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

// caddyfileTemplate is the panel's Caddyfile. {$PANEL_DOMAIN},
// {$SUB_DOMAIN}, {$BACKEND_URL}, {$SUB_BACKEND_URL} are Caddy's own
// environment-variable placeholders (resolved by Caddy at runtime from
// the container environment set in dockerComposeTemplate above), so
// they stay as literal text here, same as caddynode's Caddyfile.
// cookiesRandom1/cookiesRandom2, by contrast, are substituted with Go's
// %s placeholders at file-creation time, since they need to be baked
// into the actual cookie name/value rather than resolved later.
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

	// Unlike internal/panelonly (nginx), this validates the node/selfsteal
	// domain with domain.CheckDomain, not just a bare read.
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

	// The REMNAWAVE_API_TOKEN placeholder written here is the literal
	// text "$api_token" (kept as-is rather than left empty like the
	// nginx panelonly/panelfull templates use). api.CreateAPIToken's
	// replaceLineInFile matches by line prefix "REMNAWAVE_API_TOKEN=" and
	// replaces the whole line regardless of what follows, so either
	// placeholder text works identically.
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

// InstallationPanelOnly installs the panel alone behind Caddy.
func InstallationPanelOnly() error {
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
	_ = exec.Command("ufw", "allow", "80/tcp", "comment", "HTTP").Run()
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = upCmd.Run()

	domainURL := "127.0.0.1:3000"
	targetDir := panelDir
	api.PanelDomain = state.panelDomain

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("REGISTERING_REMNAWAVE"), ui.ColorReset)
	time.Sleep(20 * time.Second)

	// Wait for the panel API to answer.
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

	// Register the superadmin account.
	token := api.RegisterRemnawave(domainURL, state.superadminUser, state.superadminPass, "")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REGISTRATION_SUCCESS"), ui.ColorReset)

	// Generate xray keys. No GetPublicKey call here (unlike
	// internal/caddypanelfull): this variant has no co-located remnanode
	// container to patch a SECRET_KEY into, matching internal/panelonly.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Delete the default config profile.
	_ = api.DeleteConfigProfile(domainURL, token, "")

	// Create our own config profile.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	// The initial profile only carries the stock Raw inbound; a
	// Hysteria2 cert path isn't needed until internal/nodeprofile turns
	// Hysteria2 on later, at which point it asks the operator directly
	// whether the node's certificate is a wildcard (see
	// certs.AskCertDomain) instead of guessing from here, where this
	// flow has no way to know what internal/caddynode's install did.
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, "StealConfig", state.selfstealDomain, privateKey, "", "", "", api.ConfigProfileInbounds{Raw: true})
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	// Create the node, passing the selfsteal domain as its address, same
	// as internal/panelonly.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_NODE"), ui.ColorReset)
	api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, state.selfstealDomain, "")

	// Create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	api.CreateHost(domainURL, token, inboundUUID, state.selfstealDomain, configProfileUUID, "")

	// Default squad.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_DEFAULT_SQUAD"), ui.ColorReset)
	squadUUIDs, _ := api.GetDefaultSquad(domainURL, token)
	if len(squadUUIDs) > 0 {
		_ = api.UpdateSquad(domainURL, token, squadUUIDs[0], inboundUUID)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SQUAD"), ui.ColorReset)

	// Subscription-page API token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_API_TOKEN"), ui.ColorReset)
	_ = api.CreateAPIToken(domainURL, token, targetDir, "")

	// Restart the subscription page so it picks up the token.
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

	// Final summary screen.
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
