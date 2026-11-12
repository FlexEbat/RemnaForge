# TECH.md

**Version: v1.9.12** (2026-11-12)

Changelog (newest first, one entry per pushed unit of work):

- **v1.9.12**: `TECH.md` is translated into English in full. Section numbers, package names, i18n keys and code identifiers are unchanged. The language rule in `CONTRIBUTING.md` (section "Language") now lists `TECH.md` as an English document. `CONTRIBUTING.md` itself stays Russian. The code is unchanged.

- **v1.9.11**: 117 unused keys are removed from `internal/i18n/i18n.go` (117 lines each in `english` and `russian`). The check ran in three steps. A key counted as live if its name appears as a string literal anywhere in the code outside the tables. That found 10 keys that are not called through `i18n.T("…")` directly but are passed to the menu (`MENU_1`, `MENU_2`, `MENU_4`, `MENU_7`, `MENU_8`, `MENU_10`, `MENU_11`) and to the language selector (`CHOOSE_LANG`, `LANG_EN`, `LANG_RU`). Those stay. The code builds no key names dynamically: the only non-literal call, `i18n.T(key)` in `menu.go`, receives literals. The texts of the 117 removed keys are not duplicated as hardcoded strings in the code. Among the removed keys: the whole `WARP_*` set (WARP Native is not implemented, section 5), `BRANDING_*`, `SUB_*`, `ALIAS_*`, `EXTENSIONS_*`, `YQ_*`, `UPDATE_*`, and `DOCKER_COMPOSE_DOWN_FAILED` (replaced by `TEARDOWN_FAILED`). The new test `TestNoUnusedKeys` stops keys from piling up again: it fails when a key is added without a use, which was verified.

- **v1.9.10**: the section "Files this tool creates and touches" in all four `README` files reflects the changes of v1.9.2 and v1.9.4. The first dependency install touches `/etc/sysctl.d/99-remnaforge-bbr.conf` (not `/etc/sysctl.conf`, which only the IPv6 switch edits), and before enabling ufw the tool allows the port sshd listens on. The edit landed in one commit, following the rule in the "Language" section of `CONTRIBUTING.md`.

- **v1.9.9**: `TECH.md` is aligned with the code (section 10, item 5). The folder tree (section 3) gains `maintenance`, `nodeprofile` and `stack`, and the table of implemented parts (section 5) gains `maintenance` and `stack`. The second `Version: v1.7.0` header in the middle of the changelog is removed: the file has one version, and each entry has its own. In "Not implemented" the description of auto-update is refined: the release pipeline is set up, but the repository has no `v*` tags yet.

- **v1.9.8**: tests for pure functions that had none. `internal/genutil`: password composition and length, the `GenerateAlnumSecret` alphabet, `HardenEnv` (the password in `DATABASE_URL` and `POSTGRES_PASSWORD` matches, foreign lines stay untouched, two runs produce different secrets). `internal/selfsteal`: `extractZip` rejects a path outside the target directory (Zip Slip) and extracts nested files. `internal/certs`: the order of `sortNatural`. `internal/i18n`: identical key sets in en and ru, no empty values, matching format verbs, and every `i18n.T("KEY")` literal in the code is defined in both locales. For the Zip Slip guard and for `HardenEnv` it was verified that the test fails when the matching check is disabled. The product code did not change.

- **v1.9.7**: `managepanel` (opening and closing panel access on port 8443) wrote `nginx.conf` and `Caddyfile` with mode 0644, although installers have created them with 0600 since v1.9.0. `os.WriteFile` does not change the mode of an existing file, so behavior on servers that are already installed stays the same. The difference shows only when the file is created anew. The mode is now 0600 in four places of `internal/managepanel/access.go`.

- **v1.9.6**: `api.GetPanelToken`: the doc comment referred to a "line-by-line match" with the original bash script, and a comment carried over verbatim, `local auth_status=$(make_api_request ...)`, remained inside. That broke the v1.2.0 rule that the code holds no traces of the earlier implementation. The comments are replaced with a description of the behavior. The path to the token file is built with `filepath.Join` instead of string concatenation (which produced `.../remnawave_reverse//token`). The behavior of the function did not change.

- **v1.9.5**: `certs.ensureDNSCredentials` created the DNS token file (`cloudflare.ini`/`gcore.ini`) with `_ = os.MkdirAll` and `_ = os.WriteFile`: on failure the operator saw nothing, and certificate renewal failed later. The write moved to `writeSecretFile` (directory 0700, file 0600), which `issue.go` also uses, where the directory used to be created with 0755. `ensureDNSCredentials` now prints the error (new key `CERT_CREDENTIALS_WRITE_FAILED`, en+ru). Tests in `internal/certs/secret_test.go`.

- **v1.9.4**: `preflight.enableBBR` edited `/etc/sysctl.conf` with `strings.Contains`: the commented line `# net.ipv4.tcp_congestion_control = bbr` counted as a setting, so BBR was not enabled, and an entry with different spacing (`=bbr`) was appended a second time. The tool now writes its own file `/etc/sysctl.d/99-remnaforge-bbr.conf` (`writeBBRConf`), leaves it alone when the content matches, and applies it with `sysctl -p <file>`. Old lines in `/etc/sysctl.conf` on servers that are already installed stay and are harmless. Test in `internal/preflight/bbr_test.go`.

- **v1.9.3**: `backuprestore` downloaded the third-party `backup-restore.sh` from the moving `main` branch and ran it as root without verification. The URL is now pinned to a commit (`scriptRef`), and the downloaded bytes are checked against a SHA-256 (`scriptSHA256`) before they are written to disk: a swapped file or an interrupted download does not become an executable script. The size is limited to 5 MiB, and exceeding it returns an error instead of silently truncating the file. To update: pick a commit, review the script, replace both constants (instructions in the comment above them). Limitation: pinning protects only the first download. The script updates itself from `SCRIPT_REPO_URL` on `main`. That is the behavior of the third-party project, and RemnaForge does not control it. Tests in `internal/backuprestore/backuprestore_test.go`.

- **v1.9.2**: `preflight.InstallPackages` allowed only `22/tcp` in ufw and immediately ran `ufw --force enable`. On a server with sshd on another port, that cut off SSH access right after the dependency install. The new `sshPorts()` takes the port of the current session from `SSH_CONNECTION` (the only source that stays correct under socket activation: on Ubuntu 24.04 `ssh.socket` sets the port in the socket unit and ignores `Port` from `sshd_config`), adds the ports from `sshd -T`, and falls back to 22 when no source yields a port. For every port found, the tool adds the rule `ufw allow <port>/tcp`. Parsing lives in pure functions with tests in `internal/preflight/sshports_test.go`.

- **v1.9.1**: a failed backup no longer leads to data deletion. `stack.PromptBackup` returned nothing, so when the operator answered `y` to the backup question and `pg_dump` failed, removal and reinstall went on, and `docker compose down -v` erased the volumes that held the only copy of the database. The function now returns `bool`, and `uninstall`/`reinstall` stop before `Teardown` when a backup was requested but not written (new key `BACKUP_ABORTED`). The backup is written directly to `/root`, outside the directory that gets removed: the earlier `os.Rename` left the path inside the directory that `Teardown` deleted next when it failed, and the message "Backup saved" stayed true only on paper. `stack.backupPanelTo` removes the destination directory on any error: a truncated `db.sql.gz` next to an intact `.env` looked like a working backup. The error text now includes the stderr of `pg_dump`. Tests in `internal/stack/backup_test.go` replace `docker` with a script in `PATH`.

- **v1.9.0**: fixes from the code review and new commands. `api.GetPanelToken` built the URL without a scheme (`http://`), so "add node" did not work at all. Every request now goes through `api.BaseURL`, which accepts both `host:port` and `https://host`. The API token for the subscription page was written by prefix matching and missed an indented line in the compose file. That is replaced with `setComposeEnv`. Installers abort when registration, key generation, or the creation of the profile, node, host or token fails, and the `docker compose up` error is no longer ignored. `GetPublicKey` does not overwrite `SECRET_KEY` with an empty value. Removal and reinstall use `internal/stack`: only their own containers, volumes and images, with a backup offer and the `DELETE` confirmation. The stack directory is not removed when `compose down` failed. The global `docker system prune -a --volumes` is gone. The database password and `WEBHOOK_SECRET_HEADER` are generated at install time. Compose files, nginx.conf and Caddyfile are written with mode 0600. The log is 0600 in a 0700 directory, without ANSI codes. Node profile management keeps `dns`, `routing`, `outbounds` and foreign inbounds (`api.MergeProfileConfig`). End of stdin terminates the program instead of looping forever. Template downloads are limited to 10 attempts and 100 MB, replacing `/var/www/html` rolls back on error, and the backup-restore script is downloaded atomically. Nginx uses `$proxy_protocol_addr` for the panel, and the cookie check is case-sensitive. The tool dumps the database before a panel update. New menu item "Versions, diagnostics and migration" (`internal/maintenance`): container versions, state check, panel 2.x to 3.x migration. The same `doctor`, `versions` and `--version` are available from the command line. The WARP and "script update" stubs are removed from the menu and the README. The module is renamed to `github.com/FlexEbat/RemnaForge`.

- **v1.8.1**: five findings of a code review. `RegisterRemnawave` built JSON with `fmt.Sprintf` instead of `json.Marshal` (not exploitable now, but fragile). A truncated doc comment above `ConfigProfileInbounds`. A reference to the nonexistent function `UpdateConfigProfileInbounds` in the comment of `CreateConfigProfile` (the real name is `UpdateConfigProfile`). Two dead i18n keys: `SELFSTEAL_PROMPT` (present only in the EN locale and called nowhere) and `MENU_TITLE` (the menu title has long come from the `AppName` constant). A wrong list of callers in the doc comment of `certs.AskCertDomain` (`internal/addnode` is no longer listed there, only `internal/nodeprofile` calls it). None of these four commits bumped the version or reached the changelog when pushed, which broke section 7, items 6 and 7. This commit closes the gap retroactively with one entry instead of rewriting history that is already pushed.

- **v1.8.0**: `README.md` (English) became the main version and the source of truth for content. Translations with an identical structure were added: `README.ru.md`, `README.zh.md`, `README.fa.md` (Persian, RTL). The language switcher is the first line of each file. `CONTRIBUTING.md`, section "Language": a fact edit in one README version must reach all four in one commit. At that time `TECH.md` and `CONTRIBUTING.md` stayed Russian only (internal documents, not for the end user). Superseded in v1.9.12 for `TECH.md`.

- **v1.7.0**: reconciliation with the upstream panel and node (the latest panel version at the time was 3.4.3, announcement channels [announces](https://f.docs.rw/c/announces/14), [rw-panel](https://f.docs.rw/c/announces/rw-panel/16), [rw-node](https://f.docs.rw/c/announces/rw-node/17)). Findings:
  - **Important for users, not for this code**: panel v3.4.3 closes GHSA-8mcp-v46j-fp26 (authentication bypass leading to RCE in backend-tools). `docker-compose.yml` in every install flow already uses the floating tag `remnawave/backend:3`, so `docker compose pull && docker compose up -d` pulls the patch without a reinstall, but only if the operator actually runs it. A reminder with links to all three announcement channels is added to the README ("Before you install").
  - **A real bug found and fixed**: `caddypanelfull`/`caddypanelonly` did not compress panel and subscription-page responses (`encode zstd gzip` was missing in the Caddyfile). The panel stopped compressing responses itself starting with v3.0.0 (see announces/354). The Nginx flows got the fix in v1.1.2 (the `# Gzip Compression` block), but the equivalent fix for the Caddy flows was never made. It is not critical (traffic was simply not compressed, with no 500 and no malformed response), but it was extra load on the link for every Caddy installation since v3.0.0.
  - Checked for breaking API changes from v3.2.0 (our last reconciliation point) to 3.4.3. The lists of "endpoint now returns 201/202/204 without a body" touch none of the endpoints this project calls (`CreateConfigProfile`, `CreateNode`, `CreateHost`, `UpdateSquad`, `DeleteConfigProfile`; the last already checks the status code, not the body, since a version before this reconciliation). The changes around numeric user IDs do not touch this project (it does not manage users).
  - `templates/xrayWithoutRU.json`, the Xray JSON subscription template that was requested in an earlier task, is implemented as a file in the repository with instructions (`templates/README.md`), not as an automatic upload through the API: the template is assigned to a specific Host in the panel web UI (Templates, Subscription Templates, then Host, Advanced, Xray JSON template). The choice of Host is specific to each deployment, and RemnaForge cannot and should not guess it during installation. The directive `remnawave.injectHosts` (selector `sameTagAsRecipient`) is added. Without it, the template as originally sent referred in routing to an outbound `proxy` that does not exist in `outbounds` and would not work.
  - The "Not implemented" section is marked explicitly as not an active roadmap. The README does not mark unimplemented menu items separately.

- **v1.6.0**: closes open item (1) from the changelogs of v1.3.0 and v1.3.1 (not knowing whether a node that the current run did not install uses a wildcard certificate). For `panelonly`/`caddypanelonly`/`addnode` the question disappeared: since v1.3.1 the profile carries only `Raw` by default at install time, so computing the Hysteria2 certificate path in those three places was dead code (the argument was computed and passed to `CreateConfigProfile`, which did not use it). The code is removed, and with it `internal/addnode`'s `selectNodeWebserver()` (also dead now). The question stays relevant only in `internal/nodeprofile`, the only place where Hysteria2 is enabled after installation. There `certs.AskCertDomain()` now asks the operator directly whether the node certificate was a wildcard, instead of guessing from the domain. New key `ASK_WILDCARD_CERT` (en+ru).

- **v1.5.1**: `README.md` must not mention limits of the development environment (for example, that the Docker image build is unverified). Such caveats belong in `TECH.md`/`CONTRIBUTING.md`, not in user documentation. The matching line is removed from the "Known issues" section (in TECH.md, section 5, that caveat was already there and stays). The rule is recorded in section 6.

- **v1.5.0**: permanent fix for the bug found in v1.4.2 (Caddy did not start on a co-located and on a standalone node because the stock image lacks the `proxy_protocol` module). `internal/caddynode` and `internal/caddypanelfull` now build their own Caddy image with `xcaddy` directly in `docker-compose.yml` (`build.dockerfile_inline`, the pattern `caddy:2.11.2-builder`, then `xcaddy build --with github.com/mastercactapus/caddy2-proxyprotocol`, then copying the binary into `caddy:2.11.2`, following the example from the official Caddy blog). `docker compose up -d` for these flows now runs with `--build`. The interactive warning from v1.4.2 (`CADDY_PROXY_PROTOCOL_WARNING`, which required confirming with `y`) is gone. A short informational line (`CADDY_BUILD_NOTE`) replaces it and says the image build takes longer than a regular download.

  Verified: the resulting `docker-compose.yml` for both flows is valid YAML (`yaml.safe_load`), and the `build.dockerfile_inline` section parses correctly. **Not verified**: the real compilation of the image (`docker build`). The module `github.com/mastercactapus/caddy2-proxyprotocol` and the `xcaddy` build pattern are confirmed against the official Caddy documentation but were **not built and not run end to end**. Before relying on this fix in production, run one real installation on a server with internet access and confirm that the image builds and Caddy starts.

- **v1.4.3**: `internal/nodeprofile` no longer asks Nginx or Caddy when Hysteria2 is already active on the profile. In that case the existing inbound is reused as a whole (`api.BuildProfileConfig`), and the answer would go nowhere anyway. The question stays only for the case where Hysteria2 is enabled for the first time. Also: the actually generated `nginx.conf`/`Caddyfile` (`nginxnode`, `panelfull`, `caddynode`, `caddypanelfull`) are validated with `nginx -t`/`caddy validate`, and the `docker-compose.yml` of all four as YAML (`yaml.safe_load`, services and volumes checked). Everything is syntactically correct except the already documented `proxy_protocol` (section 5, "Known bugs").

- **v1.4.2**: a long-standing bug is found and documented (not introduced in this session): Caddy does not start when a co-located or standalone node is installed (`internal/caddynode`, `internal/caddypanelfull`). `listener_wrappers { proxy_protocol; tls }` in the Caddyfile refers to a module that the stock image `caddy:2.11.2` lacks. Confirmed locally with `caddy validate` on a really generated config. No permanent fix was chosen (three options with trade-offs, section 5, "Known bugs"). Until then, a warning appears and the confirmation `y` is required before the affected install/reinstall flows run through Caddy. README: new section "Known issues".

- **v1.4.1**: two regressions found by a review right after v1.4.0. (1) `internal/nodeprofile` looked for the Reality inbound by the hardcoded tag `"Raw"`, but `internal/addnode` creates it under a tag equal to the entity name (`entityName`), not `"Raw"`. With "Manage Node Profile" on a node registered through `addnode`, the existing Reality inbound would not be found, the checkbox would show it as off, and confirming the selection would create a NEW inbound with the tag `"Raw"` and a new `privateKey`, losing the old one. That breaks the configs already issued to clients the first time this menu item is used on such a node. Fixed: the Reality inbound is now found by `streamSettings.security == "reality"` instead of the tag name, and the tag found is reused when the profile is rebuilt. (2) After the menu items were renamed in v1.4.0, `internal/uninstall.RemoveScript()` showed the title `i18n.T("MENU_11")` ("Check for updates script") instead of `MENU_12` ("Remove script"). That is the price of renaming items in `menu.go`/`i18n.go` without searching for other places that refer to the same keys by name.

- **v1.4.0**: new main menu item "Manage Node Profile" (`internal/nodeprofile`). It enables or disables the `Hysteria2`/`XHTTP` inbounds on the config profile of an already registered node. It works like `internal/addnode`: it asks for the panel URL, the API token and the profile name directly and does not rely on a local `.env`, so it suits any node, not only a co-located one. The inbound choice is a checkbox menu (`[ ]`/`[x]`, a number toggles, an empty line confirms, at least one item is required).
  API layer (`internal/api`): `FindConfigProfileByName` (finds a profile by name and returns its current `config`) and `UpdateConfigProfile` (`PATCH /api/config-profiles`, body `{uuid, config}`; the panel replaces the config as a whole and does not merge it, confirmed against the real panel API 3.3.2/3.4.0). Building inbounds on update reuses the secrets already issued (`privateKey`/`shortIds`/salamander password) for inbounds that stay enabled and generates new ones only for newly enabled inbounds. Otherwise every toggle through the menu would break all configs already issued to clients.
  Menu: the new item is inserted as `MENU_10` right after "Manage certificates domain". `MENU_10`/`MENU_11` (Check for updates / Remove script) shift to `MENU_11`/`MENU_12`.
  README: item 9 is added to the menu list, plus a paragraph describing it.
  Open item (1) from the v1.3.0 changelog (not knowing whether the certificate of a node that this run did not install is a wildcard) applies here too: the domain and the certificate path for Hysteria2 are taken from the domain as is, without checking which method issued the certificate on the node itself.

- **v1.3.1**: the node config profile again carries only `Raw` (VLESS+Reality) by default, the stock profile. `Hysteria2`/`XHTTP` stay available (the matching location/route and the certificate mount into the node container from v1.3.0 are still there) but are enabled separately and not automatically at install time. `internal/api.CreateConfigProfile` takes a new parameter `ConfigProfileInbounds{Raw, Hysteria2, XHTTP bool}`. The inbound construction moved into the reusable `buildInboundConfig`/`buildProfileConfig`, ready for a future menu item that toggles the inbound set on an installed node. This also closes open item (2) from the v1.3.0 changelog: `inboundUUID` for `CreateNode`/`CreateHost`/`UpdateSquad` is now found by matching the tag (`Raw`) in the panel response and is no longer taken by index 0, so it no longer depends on the order in which the panel returns inbounds. Item (1) (not knowing whether `panelonly`/`caddypanelonly`/`addnode` use a wildcard certificate) stays open and matters for the future profile switcher. README: the section "What the tool does not do" is removed.

- **v1.3.0**: the node config profile (`internal/api.CreateConfigProfile`) now carries three inbounds instead of one: `Raw` (VLESS+Reality as before, the tag is renamed from `Steal`), `HYSTERIA-BBR` (Hysteria2, TLS terminated by Xray itself) and `XHTTP-TLS` (VLESS+XHTTP over a second unix socket). The change touched all five places that create a profile (`addnode`, `panelfull`, `panelonly`, `caddypanelfull`, `caddypanelonly`) and all four node installers (`nginxnode`, `caddynode`, `panelfull`, `caddypanelfull`), which need: 1) a location `/api/v2/stream-events` in nginx.conf, or a `handle` block in the Caddyfile, that proxies to `/dev/shm/xhttp.sock`, and 2) a volume mount of the node certificate into the `remnanode` container, read directly by Xray for Hysteria2 (for Nginx: `/etc/letsencrypt/live/<domain>/{fullchain,privkey}.pem`; for Caddy: the shared volume `caddy_data`, path `certs.CaddyCertPaths`, which depends on the default certificate storage layout of Caddy). The routing rules of the profile are extended (blocking `geosite:category-ru`/`geoip:ru` in addition to private). Two open points that this change does not resolve: (1) `panelonly`/`caddypanelonly`/`addnode` do not manage the files of the node itself and cannot know whether it used a wildcard certificate (in that case the real certificate directory is the base domain, not the full one), so the path is built from the domain as is. (2) `CreateConfigProfile` still takes `inboundUUID` for `CreateNode`/`CreateHost`/`UpdateSquad` from the first element of the `inbounds` array in the panel response, relying on the panel returning inbounds in the order they were sent, which is not verified against the real panel API.

- **v1.2.2**: `CONTRIBUTING.md`: the commit title format `type(scope): description` (`feat`/`fix`/`docs`/`refactor`/`test`/`chore`) is added with examples at the scale of the project. Section 7 (Definition of Done), item 8, refers to this format.
- **v1.2.1**: `README.md` is rewritten completely: structure, table of contents, sections on what exactly gets installed and where, which files the tool touches, what is not implemented. A separate section that must be read before installation links to sources on VLESS Encryption/XTLS/REALITY (the Xray-core documentation and source) and to the documentation and source of the Remnawave panel itself. RemnaForge generates configuration for both projects but does not check the substantive correctness and currency of security parameters beyond what is needed to start, and the format and recommendations of those two projects change from release to release. The sentence about the rename in the first line is brought to the format the project uses: "[New name] is the former [old name]. The project is renamed and continues as an independent fork of [upstream link]."
- **v1.2.0**: the project is renamed **RemnaForge** and develops as an independent project from this point. The reason and the consequences are in section 1. The code, the code comments and the internal documentation (`TECH.md`, `CONTRIBUTING.md`) lose the names, links and line numbers that tied the project to its history. The only remaining mention of the rename lives in the first line of `README.md`. The convention `Original bash (file:N-M): funcName()` above every function is dropped (section 4 no longer requires it). Section 5 is rewritten without the "source file" column. Section 8 (formerly "STATUS GAP") and section 9 are rewritten: development no longer checks an outside source to decide on behavior.
- **v1.1.9**: `internal/certs`: `renewHookCommand` is unified. It was used in two different variants in `check.go` and `handle.go` (one with a trailing `docker compose exec remnawave-nginx nginx -s reload`, the other without). Review of the code and comments across the project: line-by-line references to files outside this repository are removed, a Zip Slip guard is added to `internal/selfsteal.extractZip()`, a wrong error text in `internal/uninstall` is fixed (new key `DOCKER_COMPOSE_DOWN_FAILED`), and a bug in `internal/managepanel.closePanelAccessCaddy()` is fixed: the line `bind unix/{$CADDY_SOCKET_PATH}` went back into the Caddyfile unconditionally on `close`, even on an `internal/caddypanelonly` installation where `CADDY_SOCKET_PATH` is defined nowhere. It now goes back only if the `docker-compose.yml` of that installation really defines `CADDY_SOCKET_PATH`.
- **v1.1.4**: `internal/caddypanelonly`/`internal/caddypanelfull` (panel install without a node / with a node behind Caddy) are wired into `internal/menu`.
- **v1.1.3**: `internal/caddynode` is tested line by line.
- **v1.1.2**: gzip block in `nginx.conf` in all three nginx install flows (`nginxnode`, `panelonly`, `panelfull`).
- **v1.1.1**: compatibility with Remnawave Panel v3.2.0: `APP_SECRET` instead of `JWT_AUTH_SECRET`+`JWT_API_TOKENS_SECRET`, `DELETE` endpoints return `204` without a body, `remnawave/backend:3`. Regression test `internal/api/panelv3_test.go`.
- **v1.1.0**: Backup and Restore. Delegates to the third-party `distillium/remnawave-backup-restore` (MIT).
- **v1.0.x**: interface language selection and saving to a file, logging to a file (`ui.EnableFileLogging`), choice of a specific selfsteal template in the interactive menu, CI (`.github/workflows/ci.yml`), README from scratch, `internal/uninstall`, `internal/reinstall`.
- **v0.1.0**: `internal/preflight`: install of docker/certbot/ufw/cron/unattended-upgrades/BBR on a clean server. The path from a clean VPS to a working panel is closed for the nginx variant.
- **v0.0.x**: base version: IPv6, panel API client, add node, selfsteal templates, domain utilities, certificates (Cloudflare DNS-01/ACME HTTP-01/Gcore DNS-01), three nginx install flows, panel/node management, localization, main menu.

---

## 0. How to read this file

An internal document for those who continue development. `README.md` describes what the tool does for the end user. This file records what happens inside and what to do next. Update both, each for its own purpose.

---

## 1. Project

RemnaForge installs and maintains [Remnawave](https://remna.st) on a server through an interactive text menu: the panel, the node, or both parts together, behind Nginx or behind Caddy.

Since v1.2.0 this is an independent project: decisions about how the code behaves rest on what is right for this project now, not on matching any outside source. Section 9 records what that means in practice.

---

## 2. Stack and environment constraints

- Go 1.22+, all code on the stdlib except `golang.org/x/net/publicsuffix` (`internal/domain`, base domain detection under multi-level suffixes such as `.co.uk`).
- External dependencies in `go.mod` are not forbidden. Add them when they are really better than the stdlib.
- `golang.org/x/net` is wired through `replace` to the mirror `github.com/golang/net` (`replace golang.org/x/net vX.Y.Z => github.com/golang/net vX.Y.Z`). To bump the version: `go get github.com/golang/net@vX.Y.Z`.
- Paths that need Docker, systemd, ufw and a public domain are checked by CI (`.github/workflows/ci.yml`, the integration jobs `preflight-integration`/`oscheck-integration`) on a GitHub Actions Ubuntu VM.

---

## 3. Folder structure

```
cmd/remnawave/          entry point: logging, language selection, OS and root check, main menu
internal/
  addnode/               add a node to the panel
  api/                    HTTP client of the Remnawave API
  backuprestore/          download and run the third-party backup-restore
  caddynode/              node install behind Caddy
  caddypanelfull/         panel install with a co-located node behind Caddy
  caddypanelonly/         panel install without a node behind Caddy
  certs/                  issue and renew TLS certificates for Nginx
  domain/                 base domain extraction, DNS check
  genutil/                passwords, logins, secrets
  i18n/                   EN/RU localization, language selection and saving
  ipv6/                   enable and disable IPv6
  maintenance/            container versions, state check, panel 2.x to 3.x migration
  managepanel/            manage the installed panel and node
  menu/                   main menu
  nginxnode/              node install behind Nginx
  nodeprofile/            enable and disable the Raw/Hysteria2/XHTTP inbounds on the node profile
  oscheck/                OS version and root check
  panelfull/              panel install with a co-located node behind Nginx
  panelonly/              panel install without a node behind Nginx
  preflight/              install docker, certbot, ufw before the first run
  reinstall/              reinstall the panel or the node over the current one
  selfsteal/              random or chosen HTML template for the selfsteal domain
  stack/                  stack teardown, database backup, docker compose volume check
  uninstall/              remove the tool and the installed panel
  ui/                     terminal colors, input reading, file logging
```

---

## 4. Contracts (frozen)

Touch only deliberately, with an understanding of the consequences:

- **i18n key format.** `internal/i18n/i18n.go` holds all interface strings in English and Russian in two `map[string]string`. Do not rewrite it as a whole by hand. Record a targeted edit of a specific string with a comment above the data. Synthetic strings that are not in the main set (`IN_DEVELOPMENT`, `AvailableTemplates`, `DownloadingBackupRestore`) live in separate mini-maps at the bottom of the file, marked as such.
- **Structure of `docker-compose.yml`/`nginx.conf`/`Caddyfile`.** An external contract with the images `remnawave/backend`, `remnawave/node`, `remnawave/subscription-page`, `nginx`, `caddy`. Environment variables, ports and healthchecks must match what these images expect. On a major panel version change, check the real panel changelog (example: v1.1.1) and do not assume.
- **Go package names by content.** Each package is named after what it does (`panelonly` is the panel without a node, `panelfull` is the panel with a node), whatever the naming history. Check the content of the file whenever it is not obvious from the name, and do not rely on analogy with a similar module.
- **`internal/preflight.EnsureInstalled()`.** The entry point for installing dependencies (docker/certbot/ufw/cron/unattended-upgrades/BBR). Every install flow (`nginxnode`, `panelonly`, `panelfull`, `caddynode`, `caddypanelonly`, `caddypanelfull`) calls it first, before the first interactive question to the user.
- **`ui.Exit()` is the only call site of `os.Exit`.** After `ui.EnableFileLogging()` a bare `os.Exit()` loses the pipe buffer. Do not add new direct `os.Exit()` calls, only `ui.Exit()`.

---

## 5. What the project has

### Implemented

| Area | Package |
|---|---|
| OS and root check | `internal/oscheck` |
| IPv6 | `internal/ipv6` |
| Panel API | `internal/api` |
| Add node | `internal/addnode` |
| Node config profile inbound management (Raw/Hysteria2/XHTTP) | `internal/nodeprofile` |
| Selfsteal templates | `internal/selfsteal` |
| Domain utilities | `internal/domain` |
| Certificates (issue, renew, management menu) | `internal/certs` |
| Node install behind Nginx | `internal/nginxnode` |
| Panel install without a node (Nginx) | `internal/panelonly` |
| Panel install with a node (Nginx) | `internal/panelfull` |
| Node install behind Caddy | `internal/caddynode` |
| Panel install without a node (Caddy) | `internal/caddypanelonly` |
| Panel install with a node (Caddy) | `internal/caddypanelfull` |
| Panel and node management (Nginx and Caddy) | `internal/managepanel` |
| Reinstall (Nginx and Caddy) | `internal/reinstall` |
| Dependency install | `internal/preflight` |
| Removal | `internal/uninstall` |
| Versions, diagnostics and panel 2.x to 3.x migration | `internal/maintenance` |
| Stack teardown and database backup (shared helpers of `uninstall`, `reinstall`, `managepanel`) | `internal/stack` |
| Backup and restore (delegates to the third-party `distillium/remnawave-backup-restore`, MIT) | `internal/backuprestore` |
| Localization, language selection, file logging | `internal/i18n`, `internal/ui` |
| Main menu | `internal/menu` |
| CI | `.github/workflows/ci.yml` |

### Not implemented

Not in active development and not in the near plans. Listed here for completeness, not as a roadmap.

- **WARP Native.** Not started.
- **Auto-update.** Needs published releases with ready builds. The pipeline is set up (`.github/workflows/release.yml`, `.goreleaser.yaml`: linux amd64 and arm64 on a `v*` tag), but at the time of writing the repository has no `v*` tag.
- **Self-install mechanism.** There is no `install.sh`, only a build from source. It ties logically to auto-update: self-install first, then auto-update has something real to update.

### Known bugs

Empty. At the moment there are no known bugs that need a fix. The last one is closed below (the section is not removed, to keep the format for the next finding).

<!-- Closed: Caddy did not start on a co-located and on a standalone node
     (internal/caddynode, internal/caddypanelfull) because the stock caddy
     image lacks the proxy_protocol module. Fixed in the changelog above by
     building through xcaddy. See the changelog history for the full
     description of the finding and the options considered. -->

---

## 6. Writing rules

Active voice, specifics instead of generic phrases, no filler openers, no long dash, no intensifying adverbs. A comment explains the reason for a decision and does not retell the neighboring line. Do not leave commented-out code. Full rules with examples: `CONTRIBUTING.md`.

`README.md` describes the tool for the end user. It does not mention limits of the development environment or what was or was not verified while the code was written. Such caveats go to `TECH.md` or `CONTRIBUTING.md`.

---

## 7. Definition of Done for one unit of work

1. `go build ./...`: clean.
2. `go vet ./...`: clean.
3. `gofmt -l .`: empty output.
4. `go test ./...`: clean. Permanent tests live in `_test.go`. One-off verification tests are written, run, then deleted and not committed.
5. If user-visible behavior changed, update `README.md`.
6. The version in `internal/menu/version.go` is bumped.
7. This file is updated: a new item in the changelog header, and changes in sections 4 and 5 if a contract or the list of features shifted.
8. Committed with a title in the format `type(scope): description` (`CONTRIBUTING.md`) and a detailed commit body: what changed and why, not only what.
9. Pushed to `dev`, and the push is verified by an independent `git clone` + `go build`. Do not trust the local state after a push. Git stays the source of truth.

---

## 8. When a fact about the project is missing

It is unclear what the code should do in a specific situation, what a contract is called, or which package already covers the needed functionality. Work stops until this is clarified. Assumptions by analogy with a similar module are not substituted.

Order of actions:

1. Find the answer in this file or in the code itself (README, TECH.md, comments, tests). If there is no unambiguous answer and the decision touches user-visible behavior or a contract from section 4, ask the person who leads development instead of assuming.
2. Record the decision in section 4 or 5 of this file, in the same commit that uses it.
3. If a fact contradicts what is already recorded here, correct the old record explicitly and do not leave both versions side by side.

---

## 9. Working rules

- Surgical edits: do not improve or refactor neighboring working code without a reason tied to the current task.
- **Bugs are always fixed, without exceptions, regardless of size or age.** A bug found is fixed in the same unit of work where it was found. Matching how the code behaved earlier is not an argument for keeping a defect.
- One unit of work per push. The Definition of Done (section 7) runs before every push, not at the end of a large series of edits.

---

## 10. Review after a unit of work

A separate pass, after the code is written and the Definition of Done is green:

1. Comments above a function explain what it does and why, and do not retell the neighboring line of code.
2. Behavior changes are described in the commit message and in the changelog.
3. No dead code: no unused functions, no commented-out blocks, no stubs without a `🚧` mark.
4. Environment variable names in `docker-compose.yml`/`.env`/`nginx.conf`/`Caddyfile` match what the `remnawave/*` images expect (section 4).
5. Section 5 reflects the real status: nothing is marked ready unless it is built and verified.
