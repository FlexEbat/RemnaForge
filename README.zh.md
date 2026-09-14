[English](README.md) | [Русский](README.ru.md) | **中文** | [فارسی](README.fa.md)

# RemnaForge

> RemnaForge 是原 Remnawave Easy-Install（fork eGames）项目的更名版本，作为独立分支继续开发，源自 [remnawave-reverse-proxy](https://github.com/eGamesAPI/remnawave-reverse-proxy)。

RemnaForge 通过交互式文本菜单在服务器上安装和维护 [Remnawave](https://docs.rw/)：面板、节点，或两者同时安装，后端使用 Nginx 或 Caddy。使用 Go 编写，以单个二进制文件分发，无需外部运行时。

## 目录

- [安装前必读](#安装前必读)
- [已知问题](#已知问题)
- [功能](#功能)
- [环境要求](#环境要求)
- [安装](#安装)
- [首次运行](#首次运行)
- [菜单说明](#菜单说明)
- [此工具创建和修改的文件](#此工具创建和修改的文件)
- [日志](#日志)
- [仓库结构](#仓库结构)
- [构建与测试](#构建与测试)
- [参与贡献](#参与贡献)
- [许可证](#许可证)

## 安装前必读

RemnaForge 为 [Xray-core](https://github.com/XTLS/Xray-core)（VLESS、TLS/REALITY、传输层加密）和 [Remnawave](https://github.com/remnawave/panel) 面板生成配置，但它本身既不是 Xray-core 也不是面板,除了保证面板和节点能够启动之外,它不会校验所生成安全配置的实质正确性或时效性。

VLESS 协议及其相关机制（XTLS、REALITY、VLESS Encryption）发展很快，配置格式和安全默认值的建议每个版本都可能变化。昨天还安全的默认值，今天未必够用。在把服务器投入生产使用之前，尤其是面向真实用户而非测试环境时，请阅读：

- VLESS Encryption 官方 FAQ：[xraycore.org/en/misc/vless-encryption](https://web.archive.org/web/20260323081134/https://xraycore.org/en/misc/vless-encryption/)（archive.org 存档链接，因为该站点会定期迁移）。
- Xray-core 中关于 VLESS Encryption 迁移的讨论：[github.com/XTLS/Xray-core/discussions/4113](https://github.com/XTLS/Xray-core/discussions/4113)。
- Xray-core 源码：[github.com/XTLS/Xray-core](https://github.com/XTLS/Xray-core)，包括正在进行中的 [pull request](https://github.com/XTLS/Xray-core/pulls) 和 [issue](https://github.com/XTLS/Xray-core/issues) —— 当前的配置问题和已知限制往往先出现在这里，之后才会写进文档。
- 官方安装与配置指南：[xtls.github.io/en/document/install.html](https://xtls.github.io/en/document/install.html)。
- 如果文档说明不够清楚，可直接查看 `transport_method` 的源码实现：[infra/conf/transport_method.go#L454](https://github.com/XTLS/Xray-core/blob/7d214f8b094f75322fa3990f8aadad1c912f24f5/infra/conf/transport_method.go#L454)。
- 面板自身的文档和源码：[github.com/remnawave/panel](https://github.com/remnawave/panel)，[docs.rw](https://docs.rw)。

如果这些来源的说法与本 README 有出入，请以来源为准,并提交 issue 反馈。

**请及时更新面板和节点。** RemnaForge 写入的 `docker-compose.yml` 使用浮动镜像标签（`remnawave/backend:3`、`remnawave/node:latest`），所以运行 `docker compose pull && docker compose up -d`（菜单「Manage Panel/Node」→ Update）即可获取该大版本分支的最新补丁，无需重新安装。面板和节点各自有独立的公告频道，会发布关键修复信息，包括安全修复：[announces](https://f.docs.rw/c/announces/14)、[panel](https://f.docs.rw/c/announces/rw-panel/16)、[node](https://f.docs.rw/c/announces/rw-node/17)。请定期关注这些频道 —— RemnaForge 本身不会追踪或提醒这些更新。

## 已知问题

目前没有正在生效的已知 bug。

**带节点的 Caddy 安装（无论是与面板同机还是独立安装）在首次运行时会自行构建 Caddy 镜像**，而不是直接使用官方镜像：Xray 会把 Reality 的 fallback 流量通过 PROXY protocol 包装后转发到一个 unix socket，Caddy 在这个 socket 上监听它的所有域名，而官方 `caddy` 镜像并不包含这个功能所需的模块（`github.com/mastercactapus/caddy2-proxyprotocol`）。`internal/caddynode`/`internal/caddypanelfull` 的 `docker-compose.yml` 现在会通过 `xcaddy` 自行构建镜像（`docker compose up -d --build`），所以首次运行会比平时多花几分钟 —— Docker 在编译镜像，而不只是拉取镜像。

## 功能

- 在一台服务器上安装 Remnawave 面板、节点，或两者同时安装。
- 可选两种反向代理：Nginx 或 Caddy。
- Nginx 模式下通过 `certbot` 签发和续期 TLS 证书：Cloudflare DNS-01、ACME HTTP-01、Gcore DNS-01。Caddy 模式下通过内置 ACME 客户端自行签发和续期证书。
- 节点的配置 profile 默认只带一个 inbound：VLESS+Reality（`Raw`）。每个节点（无论是同机部署还是独立部署）都已经预先配置好，可以随时再启用两个 inbound：Hysteria2（`HYSTERIA-BBR`，TLS 由 Xray 自己终结）和 VLESS+XHTTP（`XHTTP-TLS`，通过 `/api/v2/stream-events`）——对应的 nginx.conf/Caddyfile location/route，以及节点证书到容器的挂载都已经就绪，只是需要之后通过「Manage Node Profile」菜单项手动开启。
- 通过面板的 HTTP API 把节点注册到已运行的面板上。
- 在 selfsteal 域名上安装一个 HTML 模板（随机选取，或从多个来源手动选择），用于伪装未授权连接的流量。
- 管理已安装的服务栈：启动、停止、更新镜像、查看 `docker compose` 日志、容器内置的 `remnawave` CLI、临时在 8443 端口开放面板以便无域名访问。
- 在当前安装基础上重新安装面板或节点，保留原来的反向代理选择。
- 在操作系统层面开启或关闭 IPv6。
- 在全新服务器上、首次安装前预装依赖：`docker`、`docker compose`、`certbot`、`ufw`、`cron`、`unattended-upgrades`，并启用 BBR。
- 面板备份与恢复 —— 委托给第三方工具 [distillium/remnawave-backup-restore](https://github.com/distillium/remnawave-backup-restore)（MIT 许可），自身不做具体实现。
- 完整卸载（容器、镜像、数据卷）或仅移除 RemnaForge 自身的状态。
- 界面支持英语和俄语，首次运行时选择。

## 环境要求

- Debian 11 及以上，或 Ubuntu 22.04 LTS 及以上。系统版本通过 `/etc/os-release` 读取，而不是依赖固定的代号列表，因此 Debian 或 Ubuntu 的新次要版本发布后无需更新 RemnaForge。
- root 权限（启动时会检查）。
- 已经指向服务器 IP 的域名 —— 用于在 Nginx 后安装面板或节点。使用 Caddy 时证书由 Caddy 自己签发，但域名仍需正确解析到服务器。
- Go 1.22+ —— 仅用于从源码构建，目标服务器上不需要。

无需提前安装 Docker、`docker compose`、`certbot`、`ufw`：RemnaForge 会在第一次需要用到某个安装流程时自动安装缺失的依赖。

## 安装

目前还没有预编译的二进制文件或安装包。

```bash
git clone https://github.com/FlexEbat/RemnaForge.git
cd RemnaForge
go build -o remnaforge ./cmd/remnawave
sudo mv remnaforge /usr/local/bin/
```

`go.mod` 中唯一的外部依赖是 `golang.org/x/net`（其中的 `publicsuffix` 包，用于在多级公共后缀域名下正确解析基础域名，例如 `.co.uk`）。其余部分全部基于 Go 标准库。

## 首次运行

```bash
sudo remnaforge
```

第一次运行时，工具会询问界面语言（English/Русский）并将选择保存到 `/usr/local/remnawave_reverse/selected_language`。之后再次运行会直接使用该语言启动。

## 菜单说明

```
1. Install Remnawave Components   — 安装：面板+节点、仅面板、添加节点、仅节点
2. Reinstall panel/node            — 卸载当前服务栈并重新安装
3. Manage Panel/Node                — 启动/停止/更新/日志/CLI/8443 临时访问
4. Install random template          — 更换节点上的 selfsteal 模板
5. WARP Native
6. Backup and Restore                — 调用第三方工具 distillium/remnawave-backup-restore
7. Manage IPv6                       — 开启或关闭 IPv6
8. Manage certificates domain        — 手动续期或重新签发证书
9. Manage Node Profile               — 为已注册的节点开启/关闭 Hysteria2/XHTTP inbound
10. Check for updates script
11. Remove script                    — 移除 RemnaForge 和/或已安装的服务栈
0. Exit
```

选择第 1 项后，先确定安装类型（面板+节点 / 仅面板 / 向已有面板添加节点 / 仅节点），接着询问使用 Nginx 还是 Caddy，然后逐项交互式询问其余信息：域名、独立安装节点时面板的 IP、证书签发方式。

第 9 项的工作方式与第 1 项中的「添加节点」流程类似：直接询问面板 URL、API 令牌和配置 profile 名称，而不是读取本机的 `.env` —— 因为这个 profile 可能属于任意一个面板，不一定是本机安装的那个。节点默认只携带 `Raw`（VLESS+Reality）；这个菜单项通过勾选式菜单（数字键切换，Enter 确认）来添加或移除 `Hysteria2`/`XHTTP`，且不会影响已启用 inbound 已经签发的密钥等信息。开启 `Hysteria2` 时会询问该节点的证书是否为通配符证书 —— 因为这个菜单项本身并没有安装这个节点，无法用其他方式判断。

## 此工具创建和修改的文件

- `/opt/remnawave/` 或 `/opt/remnanode/` —— 已安装服务栈的 `docker-compose.yml`、`.env`、`nginx.conf` 或 `Caddyfile`。
- `/usr/local/remnawave_reverse/` —— RemnaForge 自身的状态：选定的语言、日志文件、依赖已安装的标记。
- `/etc/letsencrypt/` —— 证书和续期配置，仅当选择 Nginx + certbot 时使用。
- `/etc/sysctl.conf`、`ufw` 规则 —— 在开关 IPv6 以及首次安装依赖时会修改。
- `/var/www/html/` —— selfsteal 域名的 HTML 模板。

完整卸载（菜单第 11 项，「wipe everything」）会连同容器、镜像、数据卷一起删除 `/opt/remnawave` 或 `/opt/remnanode`。不会动 `/etc/letsencrypt` —— 证书会保留在磁盘上。

## 日志

所有输出，包括子进程（`docker`、`certbot`、`ufw`）的输出，都会同时显示在屏幕上并写入 `/usr/local/remnawave_reverse/remnawave_reverse.log`。

## 仓库结构

```text
cmd/remnawave/          入口：文件日志、语言选择、系统/权限检查、主菜单
templates/               面板可用的现成 Xray JSON 订阅模板（不会自动安装，见 templates/README.md）
internal/
  addnode/               通过 API 将节点注册到面板
  api/                    Remnawave 面板 HTTP 客户端
  backuprestore/          下载并运行第三方备份/恢复工具
  caddynode/              在 Caddy 后安装独立节点
  caddypanelfull/         在 Caddy 后安装面板及同机节点
  caddypanelonly/         在 Caddy 后安装面板（不含节点）
  certs/                  为 Nginx 签发和续期 TLS 证书（certbot）
  domain/                 提取基础域名、DNS 检查、Cloudflare 检测
  genutil/                生成密码、用户名、密钥
  i18n/                   英语/俄语界面文本，语言选择与持久化
  ipv6/                   在操作系统层面开启和关闭 IPv6
  managepanel/            管理已安装的面板和节点
  menu/                   主菜单及分发逻辑
  nginxnode/              在 Nginx 后安装独立节点
  nodeprofile/            为节点的配置 profile 开启/关闭 Hysteria2/XHTTP
  oscheck/                系统版本及 root 权限检查
  panelfull/              在 Nginx 后安装面板及同机节点
  panelonly/              在 Nginx 后安装面板（不含节点）
  preflight/              首次运行前安装 docker/certbot/ufw/cron
  reinstall/              在当前安装基础上重新安装面板或节点
  selfsteal/              selfsteal 域名使用的 HTML 模板
  uninstall/              移除 RemnaForge 和/或已安装的服务栈
  ui/                     终端配色、输入读取、文件日志
```

## 构建与测试

```bash
go build ./...
go vet ./...
gofmt -l .      # 无输出表示格式正确
go test ./...
```

部分测试需要开发机器上不具备的真实环境：真正的 Docker daemon、`systemd`、`apt`、`ufw`。这些测试打了 `integration` 构建标签，不包含在普通的 `go test ./...` 中，在 CI（`.github/workflows/ci.yml`）里通过一次性的 GitHub Actions Ubuntu 虚拟机运行，每次向 `main` 和 `dev` 推送或提交 PR 时触发。手动运行方式：

```bash
go test -tags=integration ./internal/preflight/...
```

## 参与贡献

代码、提交信息和注释风格见 [CONTRIBUTING.md](CONTRIBUTING.md)。内部技术文档（各模块实现状态、已确定的架构决策）见 [TECH.md](TECH.md)。

## 许可证

[GNU GPLv3](LICENSE)。
