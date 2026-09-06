// Package caddypanelfull installs the Remnawave panel and a co-located
// selfsteal node together: a real remnanode container, unix-socket
// Caddy with proxy_protocol, and a random selfsteal template on one
// server. This mirrors internal/panelfull (nginx), the "panel + node on
// one server" installer.
//
// Like internal/caddynode, this never runs certbot: Caddy issues and
// renews its own TLS certificate via its built-in ACME client. It does
// import internal/certs for CaddyCertPaths, the path convention for
// reading that Caddy-issued certificate directly (used by the
// co-located node's Hysteria2 inbound).
package caddypanelfull

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/api"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/domain"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/genutil"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const panelDir = "/opt/remnawave"

// state carries the values gathered while prompting the user through to
// where docker-compose.yml/Caddyfile are written.
type state struct {
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

// dotEnvTemplate is the panel's .env file. Same v3.2.0-compatible fields
// as internal/caddypanelonly's dotEnvTemplate (a single APP_SECRET
// rather than JWT_AUTH_SECRET+JWT_API_TOKENS_SECRET, no
// SWAGGER_PATH/SCALAR_PATH/IS_DOCS_ENABLED); see that package's comment
// for the full rationale, not repeated here.
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

// dockerComposeTemplate is the combined panel+node docker-compose.yml.
// Like internal/caddynode and unlike internal/panelfull (nginx), this
// needs no head/tail split: no certbot step gates any part of the
// file, so everything is known upfront and written in one pass. The
// remnanode SECRET_KEY placeholder is kept as the exact literal string
// internal/api.GetPublicKey's replaceInFile call expects to find and
// patch after registration.
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
      build:
        context: .
        dockerfile_inline: |
          FROM caddy:2.11.2-builder AS builder
          RUN xcaddy build --with github.com/mastercactapus/caddy2-proxyprotocol
          FROM caddy:2.11.2
          COPY --from=builder /usr/bin/caddy /usr/bin/caddy
      image: remnaforge-caddy-proxyprotocol:2.11.2
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
          - CADDY_SOCKET_PATH=/dev/shm/nginx.sock
          - SELF_STEAL_DOMAIN=%s
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
    volumes:
      - /dev/shm:/dev/shm:rw
      - caddy_data:/data:ro

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
  caddy_data:
    name: caddy_data
    driver: local
    external: false
`

// caddyfileTemplate is the panel+node Caddyfile. As in internal/caddynode
// and internal/caddypanelonly, the {$VAR} domain references stay literal
// (resolved by Caddy from the container environment at runtime), while
// %[1]s/%[2]s substitute the actual generated cookie name/value at
// file-creation time.
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

    handle /api/v2/stream-events {
        reverse_proxy unix//dev/shm/xhttp.sock {
            header_up Connection ""
        }
    }

    handle {
        root * /var/www/html
        try_files {path} /index.html
        file_server
    }
}

http://{$PANEL_DOMAIN} {
    bind 0.0.0.0
    redir https://{$PANEL_DOMAIN}{uri} permanent
}

https://{$PANEL_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}
    encode zstd gzip

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
        root * /var/www/html
        try_files {path} /index.html
        file_server
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

http://{$SUB_DOMAIN} {
    bind 0.0.0.0
    redir https://{$SUB_DOMAIN}{uri} permanent
}

https://{$SUB_DOMAIN} {
    bind unix/{$CADDY_SOCKET_PATH}
    encode zstd gzip
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

func installPanelNodeCaddy() (*state, error) {
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

	selfstealDomain := ui.Reading(i18n.T("ENTER_NODE_DOMAIN"))
	if domain.CheckDomain(selfstealDomain, true, false) == domain.CheckAbort {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ABORT_MESSAGE"), ui.ColorReset)
		return nil, fmt.Errorf("aborted")
	}

	if panelDomain == subDomain || panelDomain == selfstealDomain || subDomain == selfstealDomain {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DOMAINS_MUST_BE_UNIQUE"), ui.ColorReset)
		return nil, fmt.Errorf("domains must be unique")
	}

	st := &state{
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
		st.appSecret,
		st.panelDomain, st.subDomain,
		st.metricsUser, st.metricsPass,
	)
	if err := os.WriteFile(filepath.Join(panelDir, ".env"), []byte(dotEnv), 0600); err != nil {
		return nil, err
	}

	// Same literal-placeholder rationale as internal/caddypanelonly: the
	// original's line 274 is `REMNAWAVE_API_TOKEN=\$api_token` (backslash
	// keeps bash from expanding it), so the file ends up containing the
	// literal text "$api_token", not an empty value. Reproduced as-is;
	// api.CreateAPIToken's replaceLineInFile matches by line prefix
	// regardless of what follows it.
	dockerCompose := fmt.Sprintf(dockerComposeTemplate,
		st.selfstealDomain, st.panelDomain, st.subDomain, "$api_token",
	)
	if err := os.WriteFile(filepath.Join(panelDir, "docker-compose.yml"), []byte(dockerCompose), 0644); err != nil {
		return nil, err
	}

	caddyfile := fmt.Sprintf(caddyfileTemplate, st.cookiesRandom1, st.cookiesRandom2)
	if err := os.WriteFile(filepath.Join(panelDir, "Caddyfile"), []byte(caddyfile), 0644); err != nil {
		return nil, err
	}

	return st, nil
}

// InstallationPanelNode installs the panel plus a co-located node
// behind Caddy.
func InstallationPanelNode() error {
	if err := preflight.EnsureInstalled(); err != nil {
		return err
	}

	st, err := installPanelNodeCaddy()
	if err != nil {
		return err
	}

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	_ = exec.Command("ufw", "allow", "80/tcp", "comment", "HTTP").Run()
	upCmd := exec.Command("docker", "compose", "up", "-d", "--build")
	upCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = upCmd.Run()

	// Allow the co-located node's docker subnet to reach itself on 2222
	// (best-effort, same as internal/panelfull).
	_ = exec.Command("ufw", "allow", "from", "172.30.0.0/16", "to", "any", "port", "2222", "proto", "tcp").Run()

	domainURL := "127.0.0.1:3000"
	targetDir := panelDir
	api.PanelDomain = st.panelDomain

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
	token := api.RegisterRemnawave(domainURL, st.superadminUser, st.superadminPass, "")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REGISTRATION_SUCCESS"), ui.ColorReset)

	// Fetch the real public key and patch it into
	// docker-compose.yml's remnanode SECRET_KEY. Key difference from
	// caddypanelonly: the node is co-located here, so it needs its real
	// key, not just a config-profile record. Same as internal/panelfull.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_PUBLIC_KEY"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	api.GetPublicKey(domainURL, token, targetDir)

	// Generate xray keys.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	privateKey := api.GenerateXrayKeys(domainURL, token)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// Delete the default config profile.
	_ = api.DeleteConfigProfile(domainURL, token, "")

	// Create our config profile.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	// The node is co-located here, so its Hysteria2 inbound reads the
	// same certificate Caddy itself obtained via ACME, out of the
	// caddy_data volume both containers share (see certs.CaddyCertPaths).
	nodeCertFullchain, nodeCertPrivkey := certs.CaddyCertPaths(st.selfstealDomain)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, "StealConfig", st.selfstealDomain, privateKey, "", nodeCertFullchain, nodeCertPrivkey, api.ConfigProfileInbounds{Raw: true})
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	// Create the node without a node_address override (unlike
	// caddypanelonly, which passes SELFSTEAL_DOMAIN as the address), so
	// it gets CreateNode's own defaults (172.30.0.1 / "Steal"), matching
	// the co-located node's actual docker-network address. Same as
	// internal/panelfull.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_NODE"), ui.ColorReset)
	api.CreateNode(domainURL, token, configProfileUUID, inboundUUID, "", "")

	// Create the host.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	api.CreateHost(domainURL, token, inboundUUID, st.selfstealDomain, configProfileUUID, "")

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

	// Restart the whole stack so remnanode picks up its
	// real SECRET_KEY and the subscription page picks up its API token.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STOPPING_REMNAWAVE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	downCmd := exec.Command("docker", "compose", "down")
	downCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = downCmd.Run()

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	upCmd2 := exec.Command("docker", "compose", "up", "-d", "--build")
	upCmd2.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = upCmd2.Run()

	// Install a random selfsteal template for the co-located node.
	if err := selfsteal.RandomHTML(""); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}

	// Final summary screen.
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
