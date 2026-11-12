# TECH.md

**Version: v1.9.15** (2026-11-12)

Changelog (newest first, one entry per pushed unit of work):

- **v1.9.15**: slice 1, lock part (point 20). New package `internal/runlock`: an exclusive non-blocking `flock` on `/usr/local/remnawave_reverse/remnaforge.lock`, taken in `main` after the root check and before the menu. A second instance prints the PID and start time of the holder (new keys `RUNLOCK_BUSY`, `RUNLOCK_FAILED`, en+ru) and exits with code 1. `doctor`, `versions`, `--version` and `--help` return earlier and take no lock. The kernel drops a flock when its process dies, so a crash leaves no stale lock. Tests in `internal/runlock/runlock_test.go`. The command timeout part (`internal/execx`, 80 `exec.Command` call sites) stays open.

- **v1.9.14**: slice 15, picker part (point 23). `nodeprofile.pickInbounds` marks `Raw Reality` as deprecated (labels `NODE_PROFILE_RAW` in both locales) and refuses to switch it on (`toggleOption`, new key `NODE_PROFILE_DEPRECATED_BLOCKED`). A profile that already carries the inbound keeps it, and the operator can switch it off. The four README files describe this in the item 8 paragraph. The install flows part of slice 15 stays open: the five profile-creating flows still create new profiles with `Raw`, pending the decision in section 11.

- **v1.9.13**: new section 11 splits the installer requirements list (23 numbered points plus the bootstrap paragraph) into 15 slices with dependencies, acceptance criteria and open questions. A code survey before writing found existing code that contradicts the list or duplicates it: `preflight.InstallPackages` turns on BBR, unattended-upgrades and ufw without asking (points 2, 10, 16), three separate Docker checks exist (`preflight.CheckDocker`, `packagesAlreadySatisfied`, `maintenance.runChecks`), `Doctor` and `certs.CheckCertExpiry` already cover part of points 17, 21 and 22, and `domain.CheckDomain` has no AAAA lookup. Each case is assigned to a slice that reworks or removes it. The code is unchanged.

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
  runlock/                exclusive run lock against a second concurrent instance
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
- **`internal/preflight.EnsureInstalled()`.** The entry point for installing dependencies (docker/certbot/ufw/cron/unattended-upgrades/BBR). Every install flow (`nginxnode`, `panelonly`, `panelfull`, `caddynode`, `caddypanelonly`, `caddypanelfull`) calls it first, before the first interactive question to the user. Slice 2 (section 11) narrows what it installs: BBR, unattended-upgrades and the ufw switch-on leave it and become opt-in steps.
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
- **Self-install mechanism.** There is no `install.sh`, only a build from source. It ties logically to auto-update: self-install first, then auto-update has something real to update. Slice 14 (section 11) covers the bootstrap script.

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

---

## 11. Slices for the installer requirements

Source: the requirements list the maintainer supplied on 2026-11-12. It has a bootstrap paragraph (Bash script that detects the architecture, downloads the binary, verifies checksum and signature, launches the installer) and numbered points 1 to 18 and 20 to 23. The source list has no point 19. Points keep their original numbers below, and the bootstrap paragraph is called point 0.

Rules for this section:

- One slice is one or more units of work in the sense of section 7. A slice is done when every line under "Done when" holds and the changelog names the slice.
- Where a slice says "rework" or "remove", the old code goes away in the same commit as the new code. Two implementations of one behavior must not coexist after the slice.
- A slice that meets a question under "Open" stops and asks (section 8). It does not guess.
- The order below respects dependencies. Slices without a dependency line can start any time.

### Code survey: what exists, what conflicts

| Point | Existing code | Verdict |
|---|---|---|
| 0, 1 | `.goreleaser.yaml` builds linux amd64/arm64 and writes `checksums.txt`. No signing, no `install.sh`, no `v*` tag. | Extend (slice 14). |
| 2 | `preflight.InstallPackages` runs `apt-get update`, installs packages and calls `configureUnattendedUpgrades()` unconditionally. No update preview, no reboot check. | Rework (slice 2). |
| 3 | Nginx (`nginxnode`, `panelonly`, `panelfull`) and Caddy (`caddynode`, `caddypanelonly`, `caddypanelfull`). No HAProxy. | Add (slice 13). |
| 4 | `preflight` install marker, `stack` uses compose project names. Install flows overwrite `docker-compose.yml`, `.env`, `nginx.conf`, `Caddyfile` with `os.WriteFile`. | Rework (slice 7). |
| 5 | `oscheck.CheckOS`, `preflight.CheckDocker`, `domain.CheckDomain`. No port, repository or time checks. | Extend (slices 3, 4, 5). |
| 6, 18 | `genutil` generates secrets, `HardenEnv`, files at 0600 in directories at 0700, log without ANSI codes. No log redaction, no rotation, no encrypted backups, no `.gitignore` for `.env`. | Extend (slice 9). |
| 7 | Generated configs were validated by hand once (v1.4.3). No validation step in the flows. | Add (slice 10). |
| 8 | Done in v1.9.0 and v1.9.1 (`stack.Teardown`, `PromptBackup`). | Keep, cover with tests only if a slice touches it. |
| 9 | `ui.EnableFileLogging` (log file), `maintenance.Doctor`. No final summary report. | Extend (slice 11). |
| 10 | `enableBBR()` runs inside `InstallPackages` without asking. `internal/ipv6` is manual. | BBR: rework (slice 2). IPv6: keep. |
| 11 | `oscheck` accepts Debian 11+ and Ubuntu 22.04+ by `/etc/os-release`. `maintenance.diskCheck` reads free disk. No kernel, RAM, package manager, systemd or repository detection. | Extend (slice 3). |
| 12 | Three Docker checks: `preflight.CheckDocker`, `preflight.packagesAlreadySatisfied`, the `docker compose` line in `maintenance.runChecks`. `preflight.ensureDocker` installs. `stack.VolumeExists`. | Merge into one module, remove duplicates (slice 4). |
| 13 | None. | Add (slice 12). |
| 14, 15 | `writeSecretFile`, `setComposeEnv`, `api.MergeProfileConfig` (profile JSON only), database backup in `stack`. No file manifest, no config backup, no atomic write, no rollback. | Add (slices 7, 8). |
| 16 | `preflight.InstallPackages` allows the sshd ports and 443 in ufw, then runs `ufw --force enable` without asking. `sshPorts()` is correct and stays. | Rework (slices 2 and 6). |
| 17 | `domain.CheckDomain` (A record, Cloudflare ranges), `certs.CheckCertExpiry`, `certs` renew hook, `maintenance.certChecks`. No AAAA, no port 80/443 reachability, no clock check, no dry-run issue. | Extend (slice 5). |
| 20 | HTTP clients carry timeouts (10 to 60 s). `exec.Command` calls carry none. No lock. | Add (slice 1). |
| 21 | `maintenance.stackCheck`, `panelAPICheck`. Install flows wait for containers with ad hoc loops. | Replace the ad hoc loops (slice 11). |
| 22 | `maintenance.Doctor` prints one line per check. No report file. | Extend (slice 11). |
| 23 | `nodeprofile.pickInbounds` offers `NODE_PROFILE_RAW` ("Raw Reality"). Five install flows create the profile with `Raw` only (v1.3.1). | Slice 15, blocked on an open question. |

### Slice 1. Run lock and command timeouts (point 20)

Status: lock part done in v1.9.15. `internal/execx` and the migration of the `exec.Command` call sites are open.

- Add `internal/runlock`: an exclusive `flock` on `/usr/local/remnawave_reverse/remnaforge.lock`, taken in `main` before the interactive menu and before any mutating subcommand. A second instance prints which PID and start time hold the lock and exits with a nonzero code. `doctor`, `versions`, `--version` and `--help` do not take the lock.
- Add `internal/execx`: `Run(ctx, timeout, name, args...)` wrapping `exec.CommandContext`, with a default timeout per command class (apt 15 min, docker pull and build 30 min, other 2 min). Rework `preflight.run`, `stack.Teardown`, `stack.backupPanelTo` and every `exec.Command` in `certs` and `maintenance` to call it. Network calls keep their `http.Client` timeouts.
- Done when: a test holds the lock in one process and asserts the second exits with the message and code. A test runs `sleep` through `execx` with a 50 ms timeout and asserts the error. `grep -rn "exec.Command(" internal` returns only `execx` itself and code that streams stdin on purpose.
- Open: none.

### Slice 2. Opt-in system changes (points 2, 10, first half of 16)

Depends on slice 1.

- Remove `enableBBR()`, `configureUnattendedUpgrades()`, the `ufw allow` loop and `ufw --force enable` from `preflight.InstallPackages`. Remove `unattended-upgrades` from `aptPackages`.
- Add `internal/systune` with a new main menu item "System tuning":
  - BBR switch (moves `writeBBRConf` and `bbrConfPath` here, same drop-in file, same idempotent write).
  - unattended-upgrades switch (the logic of `configureUnattendedUpgrades`, run only on request).
  - System package update: runs `apt-get -s upgrade`, prints the list of pending packages, asks for confirmation, runs the upgrade, then checks `/var/run/reboot-required` and tells the operator. Never reboots.
- IPv6 stays in `internal/ipv6` as it is: it is already manual.
- `packagesAlreadySatisfied` stops requiring anything that moved out.
- Update README (the "Installs dependencies" line, "Files this tool creates") in all four languages in one commit.
- Done when: a fresh run of an install flow leaves `/etc/sysctl.d/99-remnaforge-bbr.conf`, the unattended-upgrades config and the ufw state untouched (test with a fake `PATH` and a temp root). Each switch in the menu is idempotent. `TestPreflightIntegration` is adjusted to match.
- Open: servers that already run with the old behavior keep their BBR and ufw state. The slice does not undo it.

### Slice 3. Environment detection (points 5, 11)

- Add `internal/sysinfo` returning one struct: distribution and version, CPU architecture, kernel version, package manager (apt, dnf, yum, zypper, pacman, apk), systemd presence, repository state (`apt-get update` exit code and `apt-cache policy` for the packages the tool needs), RAM, free disk on `/opt` and `/var/lib/docker`.
- `oscheck.CheckOS` reads `sysinfo` instead of parsing `/etc/os-release` itself. The support matrix stays Debian 11+ and Ubuntu 22.04+.
- Minimum RAM and disk thresholds are constants with a named reason each.
- Done when: table tests feed fixture `os-release`, `meminfo` and `statfs` values. The output prints as one block at the start of an install flow.
- Open: the list names dnf and other package managers. Decide whether such systems are only detected and refused with a clear message (planned default) or get install support. Support would double the package-install code.

### Slice 4. Docker module (point 12)

Depends on slice 3.

- Add `internal/dockercheck`: engine installed, version, Compose v2, daemon state, registry reachability (`docker pull` of a small image or a manifest request), existing containers, networks and volumes of this tool, compose project name conflicts, docker contexts.
- Move `preflight.ensureDocker` here. Remove `preflight.CheckDocker`, the docker part of `packagesAlreadySatisfied` and the `docker compose` line of `maintenance.runChecks`. Callers use `dockercheck`.
- Done when: `grep -rn '"docker", "info"' internal` finds only `dockercheck`. Name conflict detection has a test with two compose projects that share a name.
- Open: none.

### Slice 5. Pre-flight, DNS and certificate feasibility (points 5, 17)

Depends on slices 1 and 3.

- Extend `domain.CheckDomain`: AAAA records, alignment of A and AAAA with the public IP of the server, domain name validity.
- Add checks: ports 80 and 443 free locally and reachable from outside, system clock (`timedatectl show` NTPSynchronized), `certbot certonly --dry-run` for feasibility, renewal timer or cron presence, expiry of existing certificates (reuse `certs.CheckCertExpiry`, do not duplicate it).
- Add `preflight.Run()`: runs all checks, prints one table, returns the failures with a remedy per line. Install flows call it once before the first question. `maintenance.certChecks` calls the same functions.
- Done when: unit tests cover AAAA mismatch, a Cloudflare-proxied record and a skewed clock with fake resolvers and a fake `timedatectl`.
- Open: reachability of ports 80 and 443 from outside needs an external probe. Decide between a probe service, a temporary listener plus the operator checking, or a local-only check with a documented limit.

### Slice 6. Firewall and intrusion tools (point 16)

Depends on slices 1 and 2.

- Add `internal/firewall`: detect ufw, firewalld, nftables, iptables (nf_tables or legacy), listening ports, NAT rules, conflicting rules for the ports the tool needs, reachability of required external addresses.
- Every change asks for consent. The operator picks the tool to configure (ufw, firewalld, nftables, iptables). The same flow offers fail2ban or CrowdSec, or neither.
- `sshPorts()` moves here unchanged and runs before any rule that enables a firewall.
- Done when: tests use recorded outputs of each tool as fixtures. No code path enables a firewall without a recorded consent.
- Open: none.

### Slice 7. Configuration file management (points 4, 14)

Depends on slice 1.

- Add `internal/cfgfile`:
  - Manifest at `/usr/local/remnawave_reverse/manifest.json` with path, owner class (`installer`, `user`, `third-party`) and SHA-256 of the content the installer wrote.
  - `Write(path, content, opts)`: compares the file hash with the manifest. An unchanged installer file is replaced. A changed or unknown file is not replaced. The operator sees a diff and picks keep, merge or replace.
  - Backup into `/usr/local/remnawave_reverse/backups/<timestamp>/` before every change, mode 0600.
  - Atomic write: temporary file in the same directory, `fsync`, `rename`.
  - `.env` merge by key: existing keys keep their values, new keys are added. `docker-compose.yml`, `nginx.conf` and `Caddyfile` merge as follows: installer-owned blocks are replaced, user blocks stay (reuse the block parser of `managepanel/nginxconf.go`).
- Migrate every `os.WriteFile` of those four files in `panelonly`, `panelfull`, `nginxnode`, `caddypanelonly`, `caddypanelfull`, `caddynode` and `managepanel`. Fold `writeSecretFile` and `setComposeEnv` into this package when their callers move.
- Done when: re-running any install flow twice leaves the files byte-identical. A user edit survives a re-run. A test kills the write between temporary file and rename and finds the original intact.
- Open: none.

### Slice 8. Install transaction and rollback (points 4, 15)

Depends on slices 7 and 4.

- Add a journal of steps per install run: file writes (through `cfgfile`), containers and networks created, packages installed, services started.
- On failure the installer reverts in reverse order: restore files from the backup, remove containers and networks created in this run, never remove volumes or databases (point 8 stays absolute).
- Packages and system changes are reported and not reverted.
- Done when: a fault-injection test fails the flow after each step and asserts the pre-run state, byte for byte, for files and by name for containers.
- Open: none.

### Slice 9. Secrets (points 6, 18)

Depends on slice 7.

- `ui` log writer redacts known secret values and key patterns (`PASSWORD`, `SECRET`, `TOKEN`, `API_KEY`) before writing to the log file and the report.
- Audit every `exec` call and flag for secrets in argument lists. Move any found to stdin, environment files or file descriptors. List the results in the changelog entry.
- Every secret file is 0600 and its directory 0700, checked by a test over the paths `cfgfile` knows. Install flows write a `.gitignore` that excludes `.env` and the secret files into the stack directory.
- Rotation: a menu item that generates a new database password and `WEBHOOK_SECRET_HEADER`, applies them, restarts the dependent services, verifies health and rolls back through slice 8 on failure.
- Backups: the database backup from `stack` can be encrypted. Secrets in `.env` are part of the backup.
- Done when: tests cover redaction and permissions. A rotation test with a fake `docker` verifies the order of steps.
- Open: pick the encryption tool for backups: `age` (new dependency on the server), `gpg` (present or installable through apt) or `openssl enc`. Default proposal: `gpg --symmetric` with a passphrase read from the terminal, because it needs no new binary.

### Slice 10. Pre-deployment validation (point 7)

Depends on slice 7.

- Before every `docker compose up`, run `docker compose config -q` on the generated compose file.
- Validate the selected proxy config in a throwaway container: `nginx -t`, `caddy validate`, and `haproxy -c` after slice 13.
- A failed validation stops the flow before any change is applied and prints the tool output.
- Done when: a test feeds a broken `nginx.conf` and a broken compose file and asserts the flow stops before `up`.
- Open: none.

### Slice 11. Post-install verification, diagnostics and report (points 9, 21, 22)

Depends on slices 4 and 5.

- Add `internal/verify` with named checks: container exists, container running, healthcheck passed, PostgreSQL accepts connections (`pg_isready` in the db container), the application answers HTTP, the reverse proxy routes to it, TLS handshake and chain valid, the Telegram integration can reach its external services.
- Every failed check carries a short description and a remedy line.
- Install flows end with `verify.Run()` and a summary report. The report is also saved to a file at 0600 with secrets redacted (slice 9).
- `maintenance.Doctor` runs the same checks over the whole infrastructure and writes the report. It reuses `stackCheck`, `panelAPICheck`, `envCheck` and `certChecks` through `verify`, with no second copy. The ad hoc container wait loops in the install flows go away.
- Done when: a test with a fake `docker` and a local HTTP server covers pass and fail of each check. `doctor` and an install flow produce the same lines for the same state.
- Open: "the Telegram bot can connect to the necessary external services" has two readings. One is an egress check to `api.telegram.org`. The other adds a `getMe` call with the bot token from `.env`. The planned default is both, with the token sent only to `api.telegram.org` and never logged.

### Slice 12. Dry run (point 13)

Depends on slices 1, 7 and 2.

- Global flag `--dry-run`, carried in a package-level run context. `execx`, `cfgfile`, `systune` and `firewall` check it: mutating calls print the planned action (command line with secrets redacted, or the file diff) and do nothing. Read-only checks still run.
- The flag works in the interactive flows and with the subcommands.
- Done when: a test runs an install flow in dry-run on a temp root and asserts that nothing changed on disk and that no mutating command ran.
- Open: none.

### Slice 13. HAProxy support (point 3)

Depends on slices 7 and 10.

- Introduce a proxy backend interface (`Nginx`, `Caddy`, `HAProxy`) in a new package. The six install packages and `managepanel` share a large amount of near-identical code per proxy. The interface replaces that duplication step by step: the first move extracts the existing Nginx and Caddy code behind the interface with no behavior change, then HAProxy is added as a third implementation.
- HAProxy flows for panel, node and both, with certificates handled like the Nginx flow (certbot, concatenated PEM, reload hook).
- Done when: generated configs for all three proxies pass the slice 10 validation. The removed duplicated code shows in the diff.
- Open: the target design for HAProxy needs a decision: how SNI routing, PROXY protocol, the selfsteal site and the unix sockets used by the Nginx and Caddy flows map onto HAProxy, and whether the panel on port 8443 stays reachable the same way.

### Slice 14. Bootstrap script and signed releases (points 0, 1)

- Add `install.sh`: detects the architecture (`uname -m`, maps to amd64 or arm64, refuses others), downloads the matching archive from the latest GitHub release, verifies the SHA-256 against `checksums.txt` and the signature of `checksums.txt`, extracts the binary and launches it. It runs with `set -euo pipefail`, takes no secrets and exits on any failed verification.
- Extend `.goreleaser.yaml` with signing of `checksums.txt` and extend `release.yml` accordingly. Cut the first `v*` tag after the script is tested.
- Pre-compiled binaries remove the need to install Go (point 1). Update README (install section) in all four languages and move "Self-install mechanism" out of "Not implemented" in section 5.
- Done when: a CI job runs `install.sh` against a test release and a tampered archive, and the tampered one is rejected.
- Open: pick the signature scheme: `cosign` keyless (needs the cosign binary on the server), `minisign` (small binary, key in the repository) or `gpg` (usually present). Default proposal: `minisign`, because the verifier is one small static binary the script can pin by hash.

### Slice 15. Deprecate "Raw Reality" (point 23)

Status: picker part done in v1.9.14. Install flows part blocked on the open question below.

- `nodeprofile.pickInbounds` marks `NODE_PROFILE_RAW` as deprecated (the label gets a deprecation note in both locales) and refuses to switch it on. A profile that already carries the inbound keeps it, and the operator can switch it off.
- Open, blocks the install flows part: since v1.3.1 the five profile-creating flows (`addnode`, `panelfull`, `panelonly`, `caddypanelfull`, `caddypanelonly`) create the profile with `Raw` only, and the picker demands at least one inbound. If `Raw` cannot be selected, a new profile has no valid default. Options: (a) change only the picker and leave the install defaults as they are for now, (b) choose another default inbound for new profiles. Option (b) needs a decision on which inbound that is.
- Done when: the picker test asserts that `Raw` cannot be switched on and an existing `Raw` can be switched off. The remaining part follows the answer to the open question.

### Order of work

Start with slice 15 (small, one question), then 1, 2, 3, 4, 5, 7, 10, 9, 6, 11, 12, 8, 13, 14. Slices 13 and 14 are the largest and carry open design questions, so they go last.
