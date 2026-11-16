# TECH.md

**Version: v1.22.2** (2026-11-16)

Changelog (newest first, one entry per pushed unit of work, only the 5 most recent entries are kept):

- **v1.22.2**: second unit of slice 13: the HAProxy backend, configuration only. `proxy.For(HAProxy)` renders `haproxy.cfg` for the three layouts from one template: the panel with HAProxy on 443 (`Direct`), the panel with a co-located node behind Xray (`Socket`), and a node on its own (`Node`). Everything is routed by SNI (`ssl_fc_sni`) and terminated as TLS, with `strict-sni` so an unknown name fails the handshake, TLS 1.2 or later and no TLS tickets, as the nginx layout has; there is no Reality. Behind Xray the frontend binds `/dev/shm/haproxy.sock` with `accept-proxy`, so the client address arrives by PROXY protocol and goes to the panel as `X-Real-IP` and `X-Forwarded-For`, the decision taken at the start of the slice. The nginx cookie gate is carried over: the panel name needs the cookie or the query value, sets the cookie when the query matches, lets `/oauth2/` through only with the Telegram referer, and shows the selfsteal page otherwise (`use_backend selfsteal if host_panel`, the equivalent of nginx's 418 to `@unauthorized`); where HAProxy owns 443 and there is no selfsteal site, an unauthorized request gets no answer. The subscription name goes to `127.0.0.1:3010` and drops the connection on 400, 404, 500 and 502 like nginx's 444. The XHTTP path `/api/v2/stream-events` on the selfsteal name goes to `/dev/shm/xhttp.sock`. The selfsteal site is `backend selfsteal` on `127.0.0.1:8080`, served by a small nginx container whose `selfsteal.conf` the backend returns from `Extra`; `HAProxyServices` is the compose text of the two containers (`haproxy:3.0` and `nginx:1.28`, both on the host network, certificates mounted read-only as a directory of PEM bundles, a stale socket removed at start). Names and the cookie are checked before rendering, so a value with whitespace, quotes or `{}$%#` cannot inject a directive. Proof: `TestHAProxyConfigsPassHAProxyCheck` runs the real `haproxy -c` (2.8 here) on all three layouts, with a generated certificate and the paths moved into a temporary directory, and a deliberate typo in a directive makes it fail; CI now installs haproxy so that test does not skip. Not done yet in slice 13: the flows that use this (panel, node and both), the certificate side (concatenated PEM bundles, the renew hook that rebuilds them and reloads HAProxy, `certs` mounts that today assume nginx), the Xray profile with the HAProxy socket and TLS-terminated inbounds, and the menu entries. The nginx and caddy configs are validated at run time by `internal/validate` through Docker, which is unavailable here, so `haproxy -c` is the check this unit can run.

- **v1.22.1**: first unit of slice 13: the proxy backend interface, with no behavior change. New package `internal/proxy` has `Backend` (`Kind`, `ConfigFile`, `Panel`, `Node`), `Params` (layout, cookie, the panel, subscription and selfsteal names with their certificate directories), `Layout` (`Direct`: the proxy owns 443; `Socket`: Xray owns 443 and hands over the SNI on a unix socket with PROXY protocol), `For` and `Must`. The Nginx backend holds the three templates that were in `panelonly`, `panelfull` and `nginxnode`, the Caddy backend the three that were in `caddypanelonly`, `caddypanelfull` and `caddynode`, moved verbatim (the node's nginx.conf was an inline string and became a constant first). The six flows call the backend and lost about 640 lines between them; the two panel nginx templates differed only in the listen lines, the PROXY protocol variables and the selfsteal server, which the `Layout` now selects. Proof that nothing changed: the six renderings were captured from the code before the move, committed as golden files in `internal/proxy/testdata`, and the new test compares every backend and layout with them byte for byte. Still duplicated and still to move behind the interface: the docker-compose text of each flow, the certificate mount lines and renew hook in `certs`, and the per-flow code that is not configuration. HAProxy is not added yet: `For` refuses it.

- **v1.22.0**: slice 12 is done: `--dry-run`. The flag is read in `main` from anywhere in the arguments and stored in `internal/runctx`, a package without imports so that `execx`, `cfgfile` and the rest can all use it. It works in the menu flows and with the subcommands. In a dry run `main` takes no lock and writes no log, the language choice is not saved, and a banner says so. Three layers carry the rule. `execx` announces a command instead of starting it (`Start` marks it skipped, `Wait` returns nil, `Output` returns nothing) unless `execx.ReadOnly` knows it: the list is closed, so a command or a form it does not name counts as changing the system. Read-only are, among others, `docker info`, `ps`, `inspect`, `logs`, `compose ps`, `config`, `ls`, `docker run --rm` (nginx -t, caddy validate, key generation), `systemctl is-active`, `ufw status`, `dpkg -s`, `apt-get -s`, `certbot certificates` and `--dry-run`, `curl` without `-o`, `-X` or `-d`, and `sysctl` without `-w` or `=`. `docker compose exec` is not on the list, because the same call runs `pg_dump` and SQL, so the PostgreSQL check of `verify` is only announced in a dry run. Announced command lines pass through `ui.Redact`. `internal/fsx` wraps `WriteFile`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `Chmod` and `Symlink`, and every direct `os` call of that kind in the flows, `systune`, `ipv6`, `firewall`, `certs`, `stack`, `backupcrypt`, `selfsteal`, `uninstall`, `preflight`, `api` and `i18n` now goes through it. `cfgfile` announces from `commit`, which is the one place every managed file passes: a new file prints its line count, a changed file prints the redacted diff, a secret file prints only its size, and `atomicWrite` and `saveManifest` return before they touch the disk, so no manifest, backup or snapshot appears. The calls that open files for writing (`OpenFile`, `CreateTemp`) are guarded at their function: `RandomHTML`, `InteractiveInstall`, `backupPanelTo`, the consent log, the certbot log, the compose append of `certs`, the backup-restore download, `validate.stack` and `verify.Save`. Each of the six install flows stops right before `docker compose up` with one line, because everything after it needs the running services (account, keys, profile, node, host, subscription token, final checks). That is the maintainer's decision `--dry-run` keeps asking and reading and changes nothing. What a dry run cannot show: any step that depends on the output of a mutating command, so a flow that needs a package it would install can fail on the next check of that package. Tests in `internal/dryrun`: a before and after hash of the whole tree around every kind of change, a marker file a skipped command would create, the manifest staying absent, a secret value absent from the printed plan, a control run of the same calls without the flag, the read-only table in both directions, a scan that fails on any new direct `os` mutation outside the packages that guard it themselves (each listed with its reason), and a check that every flow stops before `docker compose up`. Disabling the `execx` guard fails 5 assertions and disabling the `cfgfile` guard fails the tree comparison. Deviation from the done-when of the slice, which asks for a test that runs an install flow: the flows read the terminal and start Docker, so no test drives one end to end. The test runs every kind of call a flow makes and checks that the six flows stop before `docker compose up`, which is weaker than a run of the flow itself. Not run against a real server.

- **v1.21.0**: slice 11 is done: post-install verification, diagnostics and report (points 9, 21, 22). The four panel flows and the two node flows wait through `verify` and end with `verify.Finish`, which prints one line per check, saves the report and returns the results. The ad hoc loops are gone from the flows (a guard test reads the six files and fails on `maxAttempts`, `NODE_ATTEMPT` or `CONTAINERS_NOT_READY_ATTEMPT`, and on a flow that does not call `WaitForApp` or `WaitForNode` and `Finish`). `verify.Wait` is the one polling loop: `WaitForApp` keeps the 40 tries 8 seconds apart of the panel flows, `WaitForNode` keeps the 5 tries 15 seconds apart of the node flows, and both print the same progress messages as before. Two criteria moved on purpose. The application check now requires a status below 400, as the old loop did, and `doctor` (which accepted anything below 500) follows it, so a 403 from the backend fails in both places. The node loop's rule, a status below 400 and an HTML body, is the named check `SelfstealServed`, and the node flows and `doctor` use it for selfsteal names. `Plan` has `Domains` (proxy routing and TLS) and `Selfsteal` (HTML page and TLS): the panel flows pass the panel and subscription names as `Domains`, the co-located flows add the selfsteal name, and the node flows pass `NodeDir` and the selfsteal name. The summary with the credentials stays the last thing on the screen, so `Finish` runs before it. A failed check does not roll the install back: the containers are up, and the operator needs the remedy lines. Tests: `Wait` stops on the first pass and gives up after the attempts, `SelfstealServed` passes on HTML and fails on plain text and on 404, `AppAnswers` fails on 403. Not run against a real Docker or a real certificate, as in the entries before.

- **v1.20.1**: the name policy covers the `SELFSTEAL` prompt of `nginxnode` and `caddynode` (maintainer's decision: the standalone node flows apply the same rule as every other prompt). The v1.20.0 reasoning that it asks for an existing name is withdrawn. A node whose selfsteal domain was registered in the panel under a predictable name can no longer be installed through these flows until the domain is renamed in the panel. The guard test now also catches a direct `ui.Reading` of `SELFSTEAL`.










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
  dockercheck/            Docker engine, Compose v2, context, registry, name conflicts, install
  cfgfile/                managed config file writes: manifest, backup, atomic write, merge
  backupcrypt/            gpg encryption of backups
  domain/                 base domain extraction, DNS check
  journal/                notes about changes a failed run cannot revert
  rotate/                 secret rotation with rollback
  txn/                    install transaction: rollback of files, containers, networks
  validate/               compose and proxy config validation before up
  verify/                 post-install checks, report file
  firewall/               firewall detection and opt-in setup, fail2ban, CrowdSec
  execx/                  external commands with a timeout per command class
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
  sysinfo/                host detection: distribution, kernel, memory, disk, repositories
  systune/                opt-in BBR, automatic security updates, system package upgrade
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
- **`internal/preflight.EnsureInstalled()`.** The entry point for installing dependencies (docker/certbot/ufw/cron). Every install flow (`nginxnode`, `panelonly`, `panelfull`, `caddynode`, `caddypanelonly`, `caddypanelfull`) calls it first, before the first interactive question to the user. Since v1.11.0 BBR, unattended-upgrades and the ufw switch-on are not part of it: they are opt-in steps in `internal/systune`.
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
| Domain utilities and name policy | `internal/domain` |
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
| External commands with timeouts | `internal/execx` |
| System tuning (BBR, automatic updates, upgrade) | `internal/systune` |
| Host detection and thresholds | `internal/sysinfo` |
| Docker state, checks and install | `internal/dockercheck` |
| Firewall, fail2ban, CrowdSec | `internal/firewall` |
| Config files: manifest, backups, merge | `internal/cfgfile` |
| Install transaction and rollback | `internal/txn`, `internal/journal` |
| Config validation before start | `internal/validate` |
| Post-install checks and report | `internal/verify` |
| Dry-run switch, file system wrapper | `internal/runctx`, `internal/fsx` |
| Reverse proxy configuration (Nginx, Caddy, HAProxy) | `internal/proxy` |
| Secrets: redaction, rotation, backup encryption | `internal/ui` (redact), `internal/rotate`, `internal/backupcrypt` |
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
     image lacks the proxy_protocol module. Fixed by building the
     Caddy image through xcaddy with the module. -->

---

## 6. Writing rules

Active voice, specifics instead of generic phrases, no filler openers, no long dash, no intensifying adverbs. A comment explains the reason for a decision and does not retell the neighboring line. Do not leave commented-out code. Full rules with examples: `CONTRIBUTING.md`.

`README.md` describes the tool for the end user. It does not mention limits of the development environment or what was or was not verified while the code was written. Such caveats go to `TECH.md` or `CONTRIBUTING.md`.

---

## 7. Definition of Done for one unit of work

1. `go build ./...`: clean.
2. `go vet ./...`: clean.
3. `gofmt -l .`: empty output.
4. `go test -race ./...`: clean, run as an unprivileged user, because CI is not root and a test that touches a system path passes as root and fails there. A package whose code reaches `cfgfile` calls `cfgfiletest.Main` from `TestMain`. Permanent tests live in `_test.go`. One-off verification tests are written, run, then deleted and not committed.
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
| 2 | `preflight.InstallPackages` ran `configureUnattendedUpgrades()` unconditionally and there was no update preview or reboot check. | Done (slice 2, v1.11.0). |
| 3 | Nginx (`nginxnode`, `panelonly`, `panelfull`) and Caddy (`caddynode`, `caddypanelonly`, `caddypanelfull`). No HAProxy. | Add (slice 13). |
| 4 | `preflight` install marker, `stack` uses compose project names. Install flows overwrote `docker-compose.yml`, `.env`, `nginx.conf`, `Caddyfile` with `os.WriteFile`. | Done (slice 7, v1.16.0): `internal/cfgfile`. |
| 5 | `oscheck.CheckOS`, `preflight.CheckDocker`, `domain.CheckDomain`. There were no port, repository or time checks. | Done (slices 3, 4, 5, v1.14.0). |
| 6, 18 | `genutil` generates secrets, `HardenEnv`, files at 0600 in directories at 0700, log without ANSI codes. There was no log redaction, no rotation, no encrypted backups, no `.gitignore` for `.env`. | Done (slice 9, v1.18.0). |
| 7 | Generated configs were validated by hand once (v1.4.3). There was no validation step in the flows. | Done (slice 10, v1.19.0). |
| 8 | Done in v1.9.0 and v1.9.1 (`stack.Teardown`, `PromptBackup`). | Keep, cover with tests only if a slice touches it. |
| 9 | `ui.EnableFileLogging` (log file), `maintenance.Doctor`. No final summary report. | Done (slice 11, v1.21.0). |
| 10 | `enableBBR()` ran inside `InstallPackages` without asking. `internal/ipv6` is manual. | BBR: done (slice 2, v1.11.0). IPv6: keep. |
| 11 | `oscheck` accepted Debian 11+ and Ubuntu 22.04+ by `/etc/os-release`. `maintenance.diskCheck` reads free disk. There was no kernel, RAM, package manager, systemd or repository detection. | Done (slice 3, v1.12.0). |
| 12 | Three Docker checks existed: `preflight.CheckDocker`, `preflight.packagesAlreadySatisfied`, the `docker compose` line in `maintenance.runChecks`, plus `preflight.ensureDocker` and `stack.VolumeExists`. | Done (slice 4, v1.13.0): one module, `internal/dockercheck`. |
| 13 | None. | Done (slice 12, v1.22.0). |
| 14, 15 | `writeSecretFile`, `setComposeEnv`, `api.MergeProfileConfig` (profile JSON only), database backup in `stack`. There was no file manifest, no config backup, no atomic write and there is no rollback. | Manifest, backup, atomic write: done (slice 7, v1.16.0). Rollback: done (slice 8, v1.17.0). |
| 16 | `preflight.InstallPackages` allowed the sshd ports and 443 in ufw, then ran `ufw --force enable` without asking. `sshPorts()` was correct and stays. | Done (slices 2 and 6, v1.15.0). |
| 17 | `domain.CheckDomain` (A record, Cloudflare ranges), `certs.CheckCertExpiry`, `certs` renew hook, `maintenance.certChecks`. There was no AAAA, no port 80/443 reachability, no clock check, no dry-run issue. | Done (slice 5, v1.14.0). |
| 20 | HTTP clients carry timeouts (10 to 60 s). `exec.Command` calls carried none and there was no lock. | Done (slice 1, v1.9.15 and v1.10.0). |
| 21 | `maintenance.stackCheck`, `panelAPICheck`. Install flows wait for containers with ad hoc loops. | Done (slice 11, v1.21.0): `internal/verify`. |
| 22 | `maintenance.Doctor` prints one line per check. No report file. | Done (slice 11, v1.21.0). |
| 23 | `nodeprofile.pickInbounds` offers `NODE_PROFILE_RAW` ("Raw Reality"). Five install flows create the profile with `Raw` only (v1.3.1). | Withdrawn: `Raw Reality` stays a normal protocol (slice 15). |

### Slice 1. Run lock and command timeouts (point 20)

Status: done. The lock part landed in v1.9.15, `internal/execx` and the migration of the `exec.Command` call sites in v1.10.0.

- Add `internal/runlock`: an exclusive `flock` on `/usr/local/remnawave_reverse/remnaforge.lock`, taken in `main` before the interactive menu and before any mutating subcommand. A second instance prints which PID and start time hold the lock and exits with a nonzero code. `doctor`, `versions`, `--version` and `--help` do not take the lock.
- Add `internal/execx`: `Command(name, args...)` and `CommandTimeout(timeout, name, args...)` returning a command that is killed with its process group after the timeout (`Interactive` has none, for commands the operator drives by hand), with a default timeout per command class (apt 15 min, docker pull and build 30 min, other 2 min). Rework `preflight.run`, `stack.Teardown`, `stack.backupPanelTo` and every `exec.Command` in `certs` and `maintenance` to call it. Network calls keep their `http.Client` timeouts.
- Done when: a test holds the lock in one process and asserts the second exits with the message and code. A test runs `sleep` through `execx` with a 50 ms timeout and asserts the error. `grep -rn "exec.Command(" internal` returns only `execx` itself and code that streams stdin on purpose.
- Open: none.

### Slice 2. Opt-in system changes (points 2, 10, first half of 16)

Status: done in v1.11.0. Depends on slice 1.

- Remove `enableBBR()`, `configureUnattendedUpgrades()`, the `ufw allow` loop and `ufw --force enable` from `preflight.InstallPackages`. Remove `unattended-upgrades` from `aptPackages`.
- Add `internal/systune` with a new main menu item "System tuning":
  - BBR switch (moves `writeBBRConf` and `bbrConfPath` here, same drop-in file, same idempotent write).
  - unattended-upgrades switch (the logic of `configureUnattendedUpgrades`, run only on request).
  - System package update: runs `apt-get -s upgrade`, prints the list of pending packages, asks for confirmation, runs the upgrade, then checks `/var/run/reboot-required` and tells the operator. Never reboots.
- IPv6 stays in `internal/ipv6` as it is: it is already manual.
- `packagesAlreadySatisfied` stops requiring anything that moved out.
- Update README (the "Installs dependencies" line, "Files this tool creates") in all four languages in one commit.
- Done when: a fresh run of an install flow leaves `/etc/sysctl.d/99-remnaforge-bbr.conf`, the unattended-upgrades config and the ufw state untouched (test with a fake `PATH` and a temp root). Each switch in the menu is idempotent. `TestPreflightIntegration` is adjusted to match.
- Decided: servers that already run with the old behavior keep their BBR, unattended-upgrades and ufw state. The slice does not undo it.

### Slice 3. Environment detection (points 5, 11)

Status: done in v1.12.0.

- Add `internal/sysinfo` returning one struct: distribution and version, CPU architecture, kernel version, package manager (apt, dnf, yum, zypper, pacman, apk), systemd presence, repository state (`apt-get update` exit code and `apt-cache policy` for the packages the tool needs), RAM, free disk on `/opt` and `/var/lib/docker`.
- `oscheck.CheckOS` reads `sysinfo` instead of parsing `/etc/os-release` itself. The support matrix stays Debian 11+ and Ubuntu 22.04+.
- Minimum RAM and disk thresholds are constants with a named reason each.
- Done when: table tests feed fixture `os-release`, `meminfo` and `statfs` values. The output prints as one block at the start of an install flow.
- Decided: only Debian and Ubuntu are supported. dnf, RHEL-based and other systems are detected and refused with a clear message, for now.

### Slice 4. Docker module (point 12)

Status: done in v1.13.0.

Depends on slice 3.

- Add `internal/dockercheck`: engine installed, version, Compose v2, daemon state, registry reachability (`docker pull` of a small image or a manifest request), existing containers, networks and volumes of this tool, compose project name conflicts, docker contexts.
- Move `preflight.ensureDocker` here. Remove `preflight.CheckDocker`, the docker part of `packagesAlreadySatisfied` and the `docker compose` line of `maintenance.runChecks`. Callers use `dockercheck`.
- Done when: `grep -rn '"docker", "info"' internal` finds only `dockercheck`. Name conflict detection has a test with two compose projects that share a name.
- Open: none.

### Slice 5. Pre-flight, DNS and certificate feasibility (points 5, 17)

Status: done in v1.14.0.

Depends on slices 1 and 3.

- Extend `domain.CheckDomain`: AAAA records, alignment of A and AAAA with the public IP of the server, domain name validity.
- Add checks: ports 80 and 443 free locally and reachable from outside, system clock (`timedatectl show` NTPSynchronized), `certbot certonly --dry-run` for feasibility, renewal timer or cron presence, expiry of existing certificates (reuse `certs.CheckCertExpiry`, do not duplicate it).
- Add `preflight.Run()`: runs all checks, prints one table, returns the failures with a remedy per line. Install flows call it once before the first question. `maintenance.certChecks` calls the same functions.
- Done when: unit tests cover AAAA mismatch, a Cloudflare-proxied record and a skewed clock with fake resolvers and a fake `timedatectl`.
- Decided: no external probe service. Local bind test for 80 and 443, `certbot --dry-run` as the outside check for port 80 (the staging server connects from the internet), and the TLS check of slice 11 for port 443 after install. The limit is documented in the report.

### Slice 6. Firewall and intrusion tools (point 16)

Status: done in v1.15.0.

Depends on slices 1 and 2.

- Add `internal/firewall`: detect ufw, firewalld, nftables, iptables (nf_tables or legacy), listening ports, NAT rules, conflicting rules for the ports the tool needs, reachability of required external addresses.
- Every change asks for consent. The operator picks the tool to configure (ufw, firewalld, nftables, iptables). The same flow offers fail2ban or CrowdSec, or neither.
- `sshPorts()` moves here unchanged and runs before any rule that enables a firewall.
- Done when: tests use recorded outputs of each tool as fixtures. No code path enables a firewall without a recorded consent.
- Open: none.

### Slice 7. Configuration file management (points 4, 14)

Status: done in v1.16.0. Depends on slice 1.

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

Status: done in v1.17.0.

Depends on slices 7 and 4.

- Add a journal of steps per install run: file writes (through `cfgfile`), containers and networks created, packages installed, services started.
- On failure the installer reverts in reverse order: restore files from the backup, remove containers and networks created in this run, never remove volumes or databases (point 8 stays absolute).
- Packages and system changes are reported and not reverted.
- Done when: a fault-injection test fails the flow after each step and asserts the pre-run state, byte for byte, for files and by name for containers.
- Open: none.

### Slice 9. Secrets (points 6, 18)

Status: done in v1.18.0.

Depends on slice 7.

- `ui` log writer redacts known secret values and key patterns (`PASSWORD`, `SECRET`, `TOKEN`, `API_KEY`) before writing to the log file and the report.
- Audit every `exec` call and flag for secrets in argument lists. Move any found to stdin, environment files or file descriptors. List the results in the changelog entry.
- Every secret file is 0600 and its directory 0700, checked by a test over the paths `cfgfile` knows. Install flows write a `.gitignore` that excludes `.env` and the secret files into the stack directory.
- Rotation: a menu item that generates a new database password and `WEBHOOK_SECRET_HEADER`, applies them, restarts the dependent services, verifies health and rolls back through slice 8 on failure.
- Backups: the database backup from `stack` can be encrypted. Secrets in `.env` are part of the backup.
- Done when: tests cover redaction and permissions. A rotation test with a fake `docker` verifies the order of steps.
- Decided: `gpg --symmetric`, passphrase read from the terminal, never passed as an argument.

### Slice 10. Pre-deployment validation (point 7)

Status: done in v1.19.0.

Depends on slice 7.

- Before every `docker compose up`, run `docker compose config -q` on the generated compose file.
- Validate the selected proxy config in a throwaway container: `nginx -t`, `caddy validate`, and `haproxy -c` after slice 13.
- A failed validation stops the flow before any change is applied and prints the tool output.
- Done when: a test feeds a broken `nginx.conf` and a broken compose file and asserts the flow stops before `up`.
- Open: none.

### Slice 11. Post-install verification, diagnostics and report (points 9, 21, 22)

Status: done in v1.21.0 (the package and `doctor` in v1.20.2).

Depends on slices 4 and 5.

- Add `internal/verify` with named checks: container exists, container running, healthcheck passed, PostgreSQL accepts connections (`pg_isready` in the db container), the application answers HTTP, the reverse proxy routes to it, TLS handshake and chain valid, the Telegram integration can reach its external services.
- Every failed check carries a short description and a remedy line.
- Install flows end with `verify.Run()` and a summary report. The report is also saved to a file at 0600 with secrets redacted (slice 9).
- `maintenance.Doctor` runs the same checks over the whole infrastructure and writes the report. It reuses `stackCheck`, `panelAPICheck`, `envCheck` and `certChecks` through `verify`, with no second copy. The ad hoc container wait loops in the install flows go away.
- Done when: a test with a fake `docker` and a local HTTP server covers pass and fail of each check. `doctor` and an install flow produce the same lines for the same state.
- Decided: the Telegram check calls `getMe` with the bot token from `.env`, sent only to `api.telegram.org` and never logged. A failure to reach the host is reported separately from a rejected token.

### Slice 12. Dry run (point 13)

Status: done in v1.22.0, with the deviation noted in its changelog entry. Depends on slices 1, 7 and 2.

- Global flag `--dry-run`, carried in a package-level run context. `execx`, `cfgfile`, `systune` and `firewall` check it: mutating calls print the planned action (command line with secrets redacted, or the file diff) and do nothing. Read-only checks still run.
- The flag works in the interactive flows and with the subcommands.
- Done when: a test runs an install flow in dry-run on a temp root and asserts that nothing changed on disk and that no mutating command ran.
- Open: none.

### Slice 13. HAProxy support (point 3)

Status: in progress. The interface and the Nginx and Caddy backends landed in v1.22.1, the HAProxy configuration in v1.22.2; the flows and the certificate side are open. Depends on slices 7 and 10.

- Introduce a proxy backend interface (`Nginx`, `Caddy`, `HAProxy`) in a new package. The six install packages and `managepanel` share a large amount of near-identical code per proxy. The interface replaces that duplication step by step: the first move extracts the existing Nginx and Caddy code behind the interface with no behavior change, then HAProxy is added as a third implementation.
- HAProxy flows for panel, node and both, with certificates handled like the Nginx flow (certbot, concatenated PEM, reload hook).
- Done when: generated configs for all three proxies pass the slice 10 validation. The removed duplicated code shows in the diff.
- Decided: every profile is routed by SNI, whichever proxy is chosen (Nginx, Caddy or HAProxy). The flow creates the sockets first, then the routing on top of them. All profiles listen on 443. Decided: the HAProxy backend implements TLS everywhere and skips Reality, which does not fit that routing yet, so HAProxy flows offer TLS-terminated profiles only. Decided when the slice started (2026-11-16): HAProxy accepts PROXY protocol from Xray on its unix socket and passes the client address on, the same as the nginx layout; the selfsteal site lives in a small static nginx container behind HAProxy, only for that site, because HAProxy cannot serve files. The panel is decided: it is served on 443 behind the SNI routing, and 8443 stays as the fallback that `managepanel` opens and closes. In the current Nginx layout Xray owns 443 and passes SNI of the panel, docs and selfsteal domains to nginx over `/dev/shm/nginx.sock` with PROXY protocol, and XHTTP goes to `/dev/shm/xhttp.sock`. The Caddy and HAProxy backends keep this socket-first layout.

### Slice 14. Bootstrap script and signed releases (points 0, 1)

- Add `install.sh`: detects the architecture (`uname -m`, maps to amd64 or arm64, refuses others), downloads the matching archive from the latest GitHub release, verifies the SHA-256 against `checksums.txt` and the signature of `checksums.txt`, extracts the binary and launches it. It runs with `set -euo pipefail`, takes no secrets and exits on any failed verification.
- Extend `.goreleaser.yaml` with signing of `checksums.txt` and extend `release.yml` accordingly. Cut the first `v*` tag after the script is tested.
- Pre-compiled binaries remove the need to install Go (point 1). Update README (install section) in all four languages and move "Self-install mechanism" out of "Not implemented" in section 5.
- Done when: a CI job runs `install.sh` against a test release and a tampered archive, and the tampered one is rejected.
- Decided: GPG. The public key is published in the repository, and `install.sh` imports it into a temporary keyring and verifies `checksums.txt.sig`.

### Slice 15. Deprecate "Raw Reality" (point 23): withdrawn

The maintainer withdrew this slice. `Raw Reality` is a normal protocol again. The install flows create the config profile, the host and the squad with `Raw Reality` during the initial setup, and the tag `Raw` stays. The picker in `internal/nodeprofile` lets the operator switch `Raw` on and off. Nothing is left to do here.

### Slice 16. Domain name policy (new)

Status: done in v1.20.0. Depends on slice 5.

- Reject predictable names for the panel, node and selfsteal domains. The first label may not be `vpn`, `node`, `panel`, and may not be a two-letter country code (`nl`, `de`, `us`) or a label that contains one as a separate part (`nl1`, `de-node`). The check runs in every domain prompt (`domain.CheckDomain` callers) before any DNS or certificate work.
- On a match the tool prints that such names are insecure, because scanners and blocklists look for them first, and suggests neutral labels such as `stats` or `metrics`. The operator must enter another name.
- Done when: table tests cover the rejected labels, accepted labels and the two-letter code list, in both locales of the message.
- Decided: the rejected words are `vpn`, `node`, `panel`, country codes, `proxy`, `admin`, `xray`, `vless` and `remna`.

### Order of work

Slices 1 to 12 and 16 are done and slice 15 is withdrawn. Continue with 13. Slice 14 is postponed.

### Decisions of 2026-11-12

- **Workflow.** Work lands on `dev` first. After the checks pass, the slice is merged into `main`, and work stops until the maintainer has reviewed it. Pushes and merges are made as the maintainer (author and committer `FlexEbat`, no tool attribution).
- **Versions.** A finished slice raises the minor version. Fixes inside a slice raise the patch version. A change that breaks existing behavior raises the major version. The exact numbers are confirmed with the maintainer before the first slice.
- **Languages.** Documentation: English, Russian, Chinese. Interface strings (`i18n`): English and Russian.
- **Slice 14 is postponed.** No bootstrap script, no signing and no first release tag for now.
- **Hysteria2.** It runs over UDP on 443 and stays outside the SNI routing. Every TCP profile goes through the routing.
- **YAML (slice 7).** `docker-compose.yml` is edited through the `yaml.v3` node API, which keeps comments and order. The module comes through its GitHub mirror with a `replace`, like `golang.org/x/net`. `nginx.conf` and `Caddyfile` use marked blocks, and `.env` is merged key by key. If the mirror does not resolve, fall back to marked blocks for compose as well.
- **User-edited installer files.** The file is kept, the operator sees a diff and picks keep, merge or replace.
- **Telegram (slice 11).** The operator fills in the bot token. The check runs only when a token is set.
- **Timeouts, thresholds and the rest of slices 1 to 13 and 16.** The defaults proposed on 2026-11-12 apply: apt 15 min, docker pull and build 30 min, other commands 2 min; refuse below 1 GiB RAM and warn below 2 GiB; 10 GB free on `/var/lib/docker` and `/opt`; kernel 4.9 warning; `apt-get upgrade` with preview; domain policy on new input only; ufw as the suggested firewall; no package revert and no certificate removal on rollback; rotate the database password and the webhook header by default; reports in `/usr/local/remnawave_reverse/reports/` at 0600; `--dry-run` keeps asking and reading, and changes nothing; HAProxy in the `haproxy:lts` container.
- **Docker install (slice 4).** Install from the official Docker apt repository: `ca-certificates` and `curl`, the key in `/etc/apt/keyrings/docker.asc`, a deb822 file `/etc/apt/sources.list.d/docker.sources`, then `docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`. The repository URL and the suite follow the distribution: `linux/debian` with `VERSION_CODENAME` on Debian, `linux/ubuntu` with `UBUNTU_CODENAME` on Ubuntu and its derivatives.
- **Automatic fixes (slice 5).** The tool fixes what it owns on its own and reports the rest with an exact remedy. Open for confirmation: see the maintainer's note on the limits of automatic fixing.

