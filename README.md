**English** | [Русский](README.ru.md) | [中文](README.zh.md) | [فارسی](README.fa.md)

# RemnaForge

> RemnaForge is the former Remnawave Easy-Install (fork eGames). The project is renamed and continues development as an independent fork of [remnawave-reverse-proxy](https://github.com/eGamesAPI/remnawave-reverse-proxy).

RemnaForge installs and maintains [Remnawave](https://docs.rw/) on a server through an interactive text menu: the panel, a node, or both at once, behind Nginx or Caddy. Written in Go, ships as a single binary with no external runtime.

## Table of contents

- [Read this before you install](#read-this-before-you-install)
- [Known issues](#known-issues)
- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [First run](#first-run)
- [Menu reference](#menu-reference)
- [Files this tool creates and touches](#files-this-tool-creates-and-touches)
- [Logs](#logs)
- [Repository structure](#repository-structure)
- [Build and test](#build-and-test)
- [Contributing](#contributing)
- [License](#license)

## Read this before you install

RemnaForge generates configuration for [Xray-core](https://github.com/XTLS/Xray-core) (VLESS, TLS/REALITY, transport encryption) and for the [Remnawave](https://github.com/remnawave/panel) panel, but is neither, and doesn't validate the substantive correctness or currency of the security settings it generates beyond what the panel and node need to start.

VLESS and its related mechanisms (XTLS, REALITY, VLESS Encryption) evolve fast. Configuration format and safe-defaults guidance change from release to release. A default that was safe yesterday may not be enough today. Before putting a server into production, especially with real users rather than a test box, read:

- The official VLESS Encryption FAQ: [xraycore.org/en/misc/vless-encryption](https://web.archive.org/web/20260323081134/https://xraycore.org/en/misc/vless-encryption/) (archive.org snapshot, since the site relocates periodically).
- The Xray-core discussion tracking the VLESS Encryption migration: [github.com/XTLS/Xray-core/discussions/4113](https://github.com/XTLS/Xray-core/discussions/4113).
- Xray-core's source: [github.com/XTLS/Xray-core](https://github.com/XTLS/Xray-core), including open [pull requests](https://github.com/XTLS/Xray-core/pulls) and [issues](https://github.com/XTLS/Xray-core/issues) — current configuration problems and known limitations show up there before they reach the docs.
- The official install and setup guide: [xtls.github.io/en/document/install.html](https://xtls.github.io/en/document/install.html).
- The `transport_method` format straight from source, when the docs alone aren't enough: [infra/conf/transport_method.go#L454](https://github.com/XTLS/Xray-core/blob/7d214f8b094f75322fa3990f8aadad1c912f24f5/infra/conf/transport_method.go#L454).
- The panel's own docs and source: [github.com/remnawave/panel](https://github.com/remnawave/panel), [docs.rw](https://docs.rw).

If anything in this README contradicts those sources, trust the sources, not this README, and open an issue.

**Keep the panel and node updated.** The `docker-compose.yml` RemnaForge writes uses floating image tags (`remnawave/backend:3`, `remnawave/node:latest`), so `docker compose pull && docker compose up -d` (menu item "Manage Panel/Node" → Update) pulls the latest patch on the major branch without reinstalling. The panel and node have their own announcement channels covering critical fixes, including security fixes: [announces](https://f.docs.rw/c/announces/14), [panel](https://f.docs.rw/c/announces/rw-panel/16), [node](https://f.docs.rw/c/announces/rw-node/17). Check them periodically — RemnaForge doesn't track or notify about them itself.

## Known issues

No active known bugs right now.

**Caddy installs with a node (co-located or standalone) build their own Caddy image on first run** instead of using a stock one: Xray wraps Reality fallback traffic in the PROXY protocol on a unix socket where Caddy listens for all its domains, and the official `caddy` image doesn't include the module that needs (`github.com/mastercactapus/caddy2-proxyprotocol`). `docker-compose.yml` for `internal/caddynode`/`internal/caddypanelfull` now builds it itself via `xcaddy` (`docker compose up -d --build`), so the first run takes a couple of minutes longer than usual — Docker is compiling the image, not just pulling it.

## Features

- Installs the Remnawave panel, a node, or both on one server.
- Two webservers to choose from: Nginx or Caddy.
- For Nginx, issues and renews TLS certificates via `certbot`: Cloudflare DNS-01, ACME HTTP-01, Gcore DNS-01. Caddy issues and renews its own certificates through its built-in ACME client.
- A node's config profile carries one inbound by default: VLESS+Reality (`Raw`). Every node (co-located or standalone) is already set up to take two more: Hysteria2 (`HYSTERIA-BBR`, TLS terminated by Xray itself) and VLESS+XHTTP (`XHTTP-TLS` over `/api/v2/stream-events`) — the matching nginx.conf/Caddyfile location/route and the node's certificate mount into the container are already in place, turned on later via the "Manage Node Profile" menu item.
- Registers a node with an already-running panel through its HTTP API.
- Installs an HTML template (random, or picked manually from several sources) on the selfsteal domain to disguise unauthorized connections.
- Manages an installed stack: start, stop, update images, tail `docker compose` logs, the `remnawave` CLI inside the container, temporarily opening the panel on port 8443 for domain-less access.
- Reinstalls the panel or node over the current install, with the same webserver choice.
- Turns IPv6 on and off at the OS level.
- Installs dependencies on a fresh server before the first install: `docker`, `docker compose`, `certbot`, `ufw`, `cron`, `unattended-upgrades`, enables BBR.
- Panel backup and restore — delegates to the third-party [distillium/remnawave-backup-restore](https://github.com/distillium/remnawave-backup-restore) (MIT) rather than implementing it itself.
- Removes the install entirely (containers, images, volumes) or just RemnaForge's own state.
- English and Russian interface, language chosen on first run.

## Requirements

- Debian 11 or newer, or Ubuntu 22.04 LTS or newer. OS version is read from `/etc/os-release`, not a fixed codename list, so a new Debian or Ubuntu minor release doesn't need a RemnaForge update.
- Root (checked at startup).
- Domain(s) already pointing at the server's IP, for installing the panel or a node behind Nginx. Behind Caddy the certificate is issued by Caddy itself, but the domain still has to resolve to the server.
- Go 1.22+ — only to build from source, not needed on the target server.

No need to install Docker, `docker compose`, `certbot`, or `ufw` ahead of time: RemnaForge installs whatever's missing the first time any install flow needs it.

## Installation

No prebuilt binaries or packages yet.

```bash
git clone https://github.com/FlexEbat/RemnaForge.git
cd RemnaForge
go build -o remnaforge ./cmd/remnawave
sudo mv remnaforge /usr/local/bin/
```

The only external dependency in `go.mod` is `golang.org/x/net` (the `publicsuffix` package, for correctly resolving the base domain under multi-label public suffixes like `.co.uk`). Everything else is Go's standard library.

## First run

```bash
sudo remnaforge
```

On the very first run the tool asks for an interface language (English/Русский) and saves the choice to `/usr/local/remnawave_reverse/selected_language`. Later runs start directly in that language.

## Menu reference

```
1. Install Remnawave Components   — install: panel+node, panel only, add a node, node only
2. Reinstall panel/node            — tear down the current stack and reinstall
3. Manage Panel/Node                — start/stop/update/logs/CLI/temporary access on 8443
4. Install random template          — change the selfsteal template on a node
5. WARP Native
6. Backup and Restore                — third-party distillium/remnawave-backup-restore
7. Manage IPv6                       — turn IPv6 on or off
8. Manage certificates domain        — renew or reissue a certificate manually
9. Manage Node Profile               — turn Hysteria2/XHTTP on or off on an already-registered node
10. Check for updates script
11. Remove script                    — remove RemnaForge and/or the installed stack
0. Exit
```

Item 1, after the install type is chosen (panel+node / panel only / add a node to an existing panel / node only), asks Nginx or Caddy, then asks everything else interactively: domains, the panel's IP when installing a standalone node, the certificate issuance method.

Item 9 works like item 1's "add a node" flow: it asks for the panel URL, API token, and config profile name directly, rather than reading a local `.env` — the profile can belong to any panel, not just the one installed on this machine. A node carries only `Raw` (VLESS+Reality) by default; this item adds or removes `Hysteria2`/`XHTTP` through a checkbox menu (a number toggles it, Enter applies the selection) without touching the already-issued secrets of inbounds that stay on. Turning on `Hysteria2` asks whether the node's certificate was issued as a wildcard — this menu item didn't install the node itself and has no other way to know.

## Files this tool creates and touches

- `/opt/remnawave/` or `/opt/remnanode/` — the installed stack's `docker-compose.yml`, `.env`, `nginx.conf` or `Caddyfile`.
- `/usr/local/remnawave_reverse/` — RemnaForge's own state: the chosen language, the log file, a marker that dependencies are already installed.
- `/etc/letsencrypt/` — certificates and renewal configuration, if Nginx with certbot was chosen.
- `/etc/sysctl.conf`, `ufw` rules — when turning IPv6 on/off and during the initial dependency install.
- `/var/www/html/` — the selfsteal domain's HTML template.

A full removal (menu item 11, "wipe everything") tears down `/opt/remnawave` or `/opt/remnanode` along with its containers, images, and volumes. It leaves `/etc/letsencrypt` alone — certificates stay on disk.

## Logs

All output, including from child processes (`docker`, `certbot`, `ufw`), goes to the screen and to `/usr/local/remnawave_reverse/remnawave_reverse.log` at the same time.

## Repository structure

```text
cmd/remnawave/          entry point: file logging, language choice, OS/root checks, main menu
templates/               ready-made Xray JSON subscription templates for the panel (not installed automatically, see templates/README.md)
internal/
  addnode/               registers a node with the panel over its API
  api/                    Remnawave panel HTTP client
  backuprestore/          downloads and runs the third-party backup/restore tool
  caddynode/              installs a standalone node behind Caddy
  caddypanelfull/         installs the panel with a co-located node behind Caddy
  caddypanelonly/         installs the panel without a node behind Caddy
  certs/                  issues and renews TLS certificates for Nginx (certbot)
  domain/                 base-domain extraction, DNS checks, Cloudflare detection
  genutil/                password, username, and secret generation
  i18n/                   EN/RU interface strings, language choice and persistence
  ipv6/                   turns IPv6 on and off at the OS level
  managepanel/            manages an installed panel and node
  menu/                   main menu and dispatch
  nginxnode/              installs a standalone node behind Nginx
  nodeprofile/            turns Hysteria2/XHTTP on or off on a node's config profile
  oscheck/                OS version and root checks
  panelfull/              installs the panel with a co-located node behind Nginx
  panelonly/              installs the panel without a node behind Nginx
  preflight/              installs docker/certbot/ufw/cron before the first run
  reinstall/              reinstalls the panel or node over the current install
  selfsteal/              HTML templates for the selfsteal domain
  uninstall/              removes RemnaForge and/or the installed stack
  ui/                     terminal colors, input, file logging
```

## Build and test

```bash
go build ./...
go vet ./...
gofmt -l .      # empty output means formatting is fine
go test ./...
```

Some checks need a real environment the development machine doesn't have: an actual Docker daemon, `systemd`, `apt`, `ufw`. They're tagged `integration`, excluded from a plain `go test ./...`, and run in CI (`.github/workflows/ci.yml`) on a disposable Ubuntu GitHub Actions machine on every push and PR to `main` and `dev`. Run them manually with:

```bash
go test -tags=integration ./internal/preflight/...
```

## Contributing

Code, commit, and comment style live in [CONTRIBUTING.md](CONTRIBUTING.md). Internal technical documentation (per-module implementation status, recorded architecture decisions) lives in [TECH.md](TECH.md).

## License

[GNU GPLv3](LICENSE).
