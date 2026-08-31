// Package panelfull installs the Remnawave panel and a co-located
// selfsteal node together: a real remnanode container, unix-socket
// Nginx with proxy_protocol, and a random selfsteal template on one
// server. internal/panelonly is the "panel alone" installer.
package panelfull

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
const dockerComposeHead = `x-common: &common
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

  remnawave-nginx:
    image: nginx:1.28
    container_name: remnawave-nginx
    hostname: remnawave-nginx
    <<: [*common, *logging]
    network_mode: host
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
`

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
		panelDomain:         panelDomain,
		subDomain:           subDomain,
		selfstealDomain:     selfstealDomain,
		panelBaseDomain:     domain.ExtractDomain(panelDomain),
		subBaseDomain:       domain.ExtractDomain(subDomain),
		selfstealBaseDomain: domain.ExtractDomain(selfstealDomain),
		superadminUser:      genutil.GenerateUser(),
		superadminPass:      genutil.GeneratePassword(),
		cookiesRandom1:      genutil.GenerateUser(),
		cookiesRandom2:      genutil.GenerateUser(),
		metricsUser:         genutil.GenerateUser(),
		metricsPass:         genutil.GenerateUser(),
		appSecret:           genutil.GenerateAlnumSecret(64),
	}

	dotEnv := fmt.Sprintf(dotEnvTemplate,
		st.appSecret,
		st.panelDomain, st.subDomain,
		st.metricsUser, st.metricsPass,
	)
	if err := os.WriteFile(filepath.Join(panelDir, ".env"), []byte(dotEnv), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(panelDir, "docker-compose.yml"), []byte(dockerComposeHead), 0644); err != nil {
		return nil, err
	}

	return st, nil
}

// nginxConfTemplate is the panel's nginx.conf, the unix-socket and
// proxy_protocol variant. This server also terminates TLS for the
// co-located node, unlike panelonly's plain `listen 443 ssl`.
const nginxConfTemplate = `server_names_hash_bucket_size 64;

# Gzip Compression
gzip on;
gzip_vary on;
gzip_proxied any;
gzip_comp_level 6;
gzip_min_length 1024;
gzip_types
    application/javascript
    application/json
    application/manifest+json
    application/xml
    application/wasm
    font/opentype
    font/eot
    font/otf
    font/ttf
    image/svg+xml
    text/css
    text/javascript
    text/plain
    text/xml;

upstream remnawave {
    server 127.0.0.1:3000;
}

upstream json {
    server 127.0.0.1:3010;
}

map $http_upgrade $connection_upgrade {
    default upgrade;
    ""      close;
}

map $http_cookie $auth_cookie {
    default 0;
    "~*%[1]s=%[2]s" 1;
}

map $arg_%[1]s $auth_query {
    default 0;
    "%[2]s" 1;
}

map "$auth_cookie$auth_query" $authorized {
    "~1" 1;
    default 0;
}

map $arg_%[1]s $set_cookie_header {
    "%[2]s" "%[1]s=%[2]s; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=31536000";
    default "";
}

ssl_protocols TLSv1.2 TLSv1.3;
ssl_ecdh_curve X25519:prime256v1:secp384r1;
ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305;
ssl_prefer_server_ciphers on;
ssl_session_timeout 1d;
ssl_session_cache shared:MozSSL:10m;
ssl_session_tickets off;

server {
    server_name %[3]s;
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol;
    http2 on;

    ssl_certificate "/etc/nginx/ssl/%[4]s/fullchain.pem";
    ssl_certificate_key "/etc/nginx/ssl/%[4]s/privkey.pem";
    ssl_trusted_certificate "/etc/nginx/ssl/%[4]s/fullchain.pem";

    add_header Set-Cookie $set_cookie_header;

    location / {

        if ($authorized = 0) {
            return 418;
        }

        error_page 418 = @unauthorized;
        recursive_error_pages on;

        proxy_http_version 1.1;
        proxy_pass http://remnawave;
        proxy_set_header Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    location @unauthorized {
        root /var/www/html;
        index index.html;
    }

    # OAuth2 Telegram login
    location ^~ /oauth2/ {

        if ($http_referer !~ "^https://oauth\.telegram\.org/") {
            return 444;
        }

        proxy_http_version 1.1;
        proxy_pass http://remnawave;
        proxy_set_header Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}

server {
    server_name %[5]s;
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol;
    http2 on;

    ssl_certificate "/etc/nginx/ssl/%[6]s/fullchain.pem";
    ssl_certificate_key "/etc/nginx/ssl/%[6]s/privkey.pem";
    ssl_trusted_certificate "/etc/nginx/ssl/%[6]s/fullchain.pem";

    location / {
        proxy_http_version 1.1;
        proxy_pass http://json;
        proxy_set_header Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header X-Real-IP $proxy_protocol_addr;
        proxy_set_header X-Forwarded-For $proxy_protocol_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
        proxy_intercept_errors on;
        error_page 400 404 500 502 @redirect;
    }

    location @redirect {
        return 444;
    }
}

server {
    server_name %[7]s;
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol;
    http2 on;

    ssl_certificate "/etc/nginx/ssl/%[8]s/fullchain.pem";
    ssl_certificate_key "/etc/nginx/ssl/%[8]s/privkey.pem";
    ssl_trusted_certificate "/etc/nginx/ssl/%[8]s/fullchain.pem";

    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;

    location /api/v2/stream-events {
        proxy_http_version 1.1;
        proxy_pass http://unix:/dev/shm/xhttp.sock;
        proxy_set_header Host $host;
        proxy_set_header Connection "";
        proxy_set_header X-Real-IP $proxy_protocol_addr;
        proxy_set_header X-Forwarded-For $proxy_protocol_addr;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;

        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    location / {
        root /var/www/html;
        index index.html;
    }
}

server {
    listen unix:/dev/shm/nginx.sock ssl proxy_protocol default_server;
    server_name _;
    add_header X-Robots-Tag "noindex, nofollow, noarchive, nosnippet, noimageindex" always;
    ssl_reject_handshake on;
    return 444;
}
`

func InstallationPanelNode() error {
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
	certResult, certErr := certs.HandleCertificates(
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

	// Append remnawave-nginx's remaining volumes/command,
	// the subscription-page service, remnanode, networks, volumes.
	composeTail := fmt.Sprintf(`      - /dev/shm:/dev/shm:rw
      - /var/www/html:/var/www/html:ro
    command: sh -c 'rm -f /dev/shm/nginx.sock && exec nginx -g "daemon off;"'

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
	composePath := filepath.Join(panelDir, "docker-compose.yml")
	existing, _ := os.ReadFile(composePath)
	if err := os.WriteFile(composePath, append(existing, []byte(composeTail)...), 0644); err != nil {
		return err
	}

	// Nginx.conf.
	nginxConf := fmt.Sprintf(nginxConfTemplate,
		st.cookiesRandom1, st.cookiesRandom2,
		st.panelDomain, panelCertDomain,
		st.subDomain, subCertDomain,
		st.selfstealDomain, nodeCertDomain,
	)
	if err := os.WriteFile(filepath.Join(panelDir, "nginx.conf"), []byte(nginxConf), 0644); err != nil {
		return err
	}

	// Bring the stack up.
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	upCmd := exec.Command("docker", "compose", "up", "-d")
	upCmd.Dir = panelDir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = upCmd.Run()

	// Allow the co-located node's docker subnet to reach itself on 2222
	// (best-effort: nice-to-have hardening, not worth failing the whole
	// install over).
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
	// docker-compose.yml's remnanode SECRET_KEY. This is the key
	// difference from panelonly: the node is co-located here, so it
	// needs its real key, not just a config-profile record.
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
	configProfileUUID, inboundUUID := api.CreateConfigProfile(domainURL, token, "StealConfig", st.selfstealDomain, privateKey, "", certFullchain, certPrivkey)
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	// Create the node without node_address/node_name overrides, unlike
	// panelonly (which passes SELFSTEAL_DOMAIN as the name), so it gets
	// CreateNode's own defaults (172.30.0.1 / "Steal"). Those defaults
	// match the co-located node's actual docker-network address.
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
	upCmd2 := exec.Command("docker", "compose", "up", "-d")
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
