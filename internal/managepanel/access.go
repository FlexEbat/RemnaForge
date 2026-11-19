package managepanel

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// showPanelAccess uses i18n.T("ACCESS_PANEL") as this submenu's title, the
// label this feature has everywhere else (the parent menu's item 6),
// rather than i18n.T("MENU_9") ("Manage certificates domain") - this menu
// covers temporary panel access on port 8443, unrelated to certificates.
func showPanelAccess() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("ACCESS_PANEL"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("PORT_8443_OPEN"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("PORT_8443_CLOSE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

func managePanelAccess() {
	for {
		showPanelAccess()
		option := ui.Reading(i18n.T("IPV6_PROMPT"))

		switch option {
		case "1":
			openPanelAccess()
		case "2":
			closePanelAccess()
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IPV6_INVALID_CHOICE"), ui.ColorReset)
		}
	}
}

func detectWebserver(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "nginx.conf")); err == nil {
		return "nginx"
	}
	if _, err := os.Stat(filepath.Join(dir, "Caddyfile")); err == nil {
		return "caddy"
	}
	if _, err := os.Stat(filepath.Join(dir, "haproxy.cfg")); err == nil {
		return "haproxy"
	}
	return ""
}

// portChecker is portInUse; tests replace it.
var portChecker = portInUse

func portInUse(port string) (inUse bool, checked bool) {
	if out, err := execx.Command("ss", "-tuln").Output(); err == nil {
		return strings.Contains(string(out), ":"+port), true
	}
	if out, err := execx.Command("netstat", "-tuln").Output(); err == nil {
		return strings.Contains(string(out), ":"+port), true
	}
	return false, false
}

var (
	serverNameRE         = regexp.MustCompile(`server_name\s+([^\s;]+)\s*;`)
	proxyPassRemnawaveRE = regexp.MustCompile(`proxy_pass\s+http://remnawave\s*;`)
	listen8443RE         = regexp.MustCompile(`[ \t]*listen[ \t]+8443[ \t]+ssl[ \t]*;[ \t]*\r?\n?`)
	cookieLineRE         = regexp.MustCompile(`map\s+\$http_cookie\s+\$auth_cookie\s*\{[^}]*"~\*?(\w+)=(\w+)"`)
	caddyPanelDomainRE   = regexp.MustCompile(`PANEL_DOMAIN=(\S*)`)
	caddyCookieLineRE    = regexp.MustCompile(`header \+Set-Cookie "([^=]+)=([^;]+)`)
)

// findPanelServerDomain finds the panel's server_name in nginx.conf by
// walking top-level `server { ... }` blocks (via findTopLevelBlocks'
// brace-depth tracking) and picking the one whose body proxies to
// remnawave, rather than relying on fixed-indentation text matching
// that would break on a hand-edited nginx.conf using different brace
// spacing than what this tool itself generates.
func findPanelServerDomain(conf string) string {
	for _, block := range findTopLevelBlocks(conf, "server") {
		body := block.Body(conf)
		if !proxyPassRemnawaveRE.MatchString(body) {
			continue
		}
		if m := serverNameRE.FindStringSubmatch(body); m != nil && m[1] != "_" {
			return m[1]
		}
	}
	return ""
}

// findServerBlockForDomain locates the specific server{} block whose
// server_name matches domain, using the same brace-depth-aware scan
// (rather than a plain substring search for "server_name X;" which can't
// tell which block it belongs to, or where that block actually ends).
func findServerBlockForDomain(conf, domainName string) (nginxBlock, bool) {
	for _, block := range findTopLevelBlocks(conf, "server") {
		body := block.Body(conf)
		if m := serverNameRE.FindStringSubmatch(body); m != nil && m[1] == domainName {
			return block, true
		}
	}
	return nginxBlock{}, false
}

func findAuthCookies(conf string) (string, string) {
	m := cookieLineRE.FindStringSubmatch(conf)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// caddyPanelDomain finds the real domain value from the docker-compose.yml's
// PANEL_DOMAIN environment entry, unlike everything else in this file's
// Caddy branches, which pattern-match on the literal text "{$PANEL_DOMAIN}"
// (Caddy's own env-var reference, never substituted by the shell or by
// the template's Sprintf) rather than on any actual domain string.
func caddyPanelDomain(compose string) string {
	m := caddyPanelDomainRE.FindStringSubmatch(compose)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// caddyExtractCookies pulls the two random cookie-name-obfuscation
// tokens out of the Caddyfile's Set-Cookie header line.
func caddyExtractCookies(caddyfile string) (string, string) {
	m := caddyCookieLineRE.FindStringSubmatch(caddyfile)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// addLineAfter inserts newLine as a new line immediately after the
// first line that equals target exactly. Line-based, not
// brace-depth-aware: the Caddy install flows always write a fixed,
// predictable line layout, so there's no hand-edited-file fragility
// this needs to guard against, unlike findServerBlockForDomain's
// brace-depth scan above for Nginx configs.
func addLineAfter(text, target, newLine string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == target {
			out := make([]string, 0, len(lines)+1)
			out = append(out, lines[:i+1]...)
			out = append(out, newLine)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n")
		}
	}
	return text
}

// removeLineInBlock deletes any line containing removeSubstr between the
// line equal to blockStart (inclusive) and the next line starting with
// "}" (inclusive).
func removeLineInBlock(text, blockStart, removeSubstr string) string {
	lines := strings.Split(text, "\n")
	inBlock := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if !inBlock && line == blockStart {
			inBlock = true
			out = append(out, line)
			continue
		}
		if inBlock {
			if strings.Contains(line, removeSubstr) {
				continue
			}
			out = append(out, line)
			if strings.HasPrefix(line, "}") {
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// addListen8443 inserts "listen 8443 ssl;" right after the panel domain's
// server_name line (stripping any stale "listen 8443 ssl;" from that
// block first, so re-running this is idempotent, then inserting a
// fresh one).
//
// The block's boundaries come from findServerBlockForDomain, which
// tracks brace depth, so indented or hand-edited configs are handled.
func addListen8443(conf, panelDomain string) (string, error) {
	block, found := findServerBlockForDomain(conf, panelDomain)
	if !found {
		return "", fmt.Errorf("server block for %q not found", panelDomain)
	}

	body := block.Body(conf)
	nameIdx := serverNameRE.FindStringIndex(body)
	if nameIdx == nil {
		return "", fmt.Errorf("server_name not found in block for %q", panelDomain)
	}

	insertAt := block.BodyStart + 1 + nameIdx[1]
	blockEnd := block.BodyEnd - 1 // position of the block's own closing '}'

	rest := listen8443RE.ReplaceAllString(conf[insertAt:blockEnd], "")
	before := conf[:insertAt]
	after := conf[blockEnd:]

	return before + "\n    listen 8443 ssl;" + rest + after, nil
}

// closePort8443UFW is the shared trailing ufw cleanup used by both the
// Nginx and Caddy branches of closing temporary panel access, factored
// into one function instead of duplicated.
func closePort8443UFW() {
	if !ufwActive() {
		printNote("PORT_8443_NO_UFW")
		return
	}
	ufwStatus, _ := execx.Command("ufw", "status").Output()
	if strings.Contains(string(ufwStatus), "8443") && strings.Contains(string(ufwStatus), "ALLOW") {
		_ = execx.Command("ufw", "delete", "allow", "from", "0.0.0.0/0", "to", "any", "port", "8443", "proto", "tcp").Run()
		if err := execx.Command("ufw", "reload").Run(); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("UFW_RELOAD_FAILED"), ui.ColorReset)
			return
		}
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PORT_8443_CLOSED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_ALREADY_CLOSED"), ui.ColorReset)
	}
}

// ufwActive reports whether ufw is installed and switched on. The other
// firewalls the installer can configure are not handled here, and saying
// "already closed" for them would be wrong.
func ufwActive() bool {
	out, err := execx.Command("ufw", "status").Output()
	return err == nil && strings.Contains(string(out), "Status: active")
}

// openPort8443 opens the port in ufw and says so when ufw cannot do it.
func openPort8443() {
	if !ufwActive() {
		printNote("PORT_8443_NO_UFW")
		return
	}
	if err := execx.Command("ufw", "allow", "from", "0.0.0.0/0", "to", "any", "port", "8443", "proto", "tcp").Run(); err != nil {
		printErr("PORT_8443_UFW_FAILED")
		return
	}
	if err := execx.Command("ufw", "reload").Run(); err != nil {
		printErr("UFW_RELOAD_FAILED")
	}
}

var (
	// containerRunning and waitBetweenChecks are variables so tests do not
	// need Docker or a second of waiting.
	containerRunning = func(name string) bool {
		out, err := execx.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
		return err == nil && strings.TrimSpace(string(out)) == "true"
	}
	waitBetweenChecks = func() { time.Sleep(time.Second) }
)

// proxyUp waits up to five seconds for the container to run.
func proxyUp(container string) bool {
	for i := 0; i < 5; i++ {
		if containerRunning(container) {
			return true
		}
		waitBetweenChecks()
	}
	return false
}

// applyPlan writes the plan's configuration and restarts the proxy. When the
// proxy does not come up, the old configuration goes back, so a rejected
// change does not leave the panel without its reverse proxy. It reports
// whether the change took effect.
func applyPlan(p accessProxy, dir string, plan accessPlan) bool {
	old, readErr := os.ReadFile(plan.confPath)
	if err := cfgfile.Replace(plan.confPath, plan.updated, cfgfile.Opts{}); err != nil {
		if p.name == "nginx" {
			printErr("NGINX_CONF_MODIFY_FAILED")
		} else {
			printErr("CONF_MODIFY_FAILED")
		}
		return false
	}
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	p.restart(dir)
	if p.container == "" || proxyUp(p.container) {
		return true
	}
	printErr("PROXY_NOT_RUNNING")
	if readErr != nil {
		return false
	}
	if err := cfgfile.Replace(plan.confPath, string(old), cfgfile.Opts{}); err != nil {
		printErr("CONF_MODIFY_FAILED")
		return false
	}
	p.restart(dir)
	if proxyUp(p.container) {
		printNote("PROXY_CONFIG_RESTORED")
	}
	return false
}

// accessPlan is what one proxy decides when temporary panel access on 8443
// is opened or closed. Everything after that decision is the same for all
// of them and lives in openPanelAccess and closePanelAccess.
type accessPlan struct {
	confPath string // the file to replace
	updated  string // its new text
	link     string // the panel link to print after opening
	// nothingToDo is the i18n key of a message that replaces the whole
	// change, such as "already configured".
	nothingToDo string
}

// accessProxy is what differs between the proxies: how the configuration
// is read and edited, and how the proxy is restarted.
type accessProxy struct {
	// name is the proxy kind: "nginx", "caddy" or "haproxy".
	name string
	// container must be running after a restart. Empty skips the check: HAProxy
	// reloads in place and keeps its old workers when it cannot parse the file.
	container string
	// open and close read dir and return the plan. They print their own
	// errors and return false when nothing can be done.
	open, close func(dir string) (accessPlan, bool)
	// restart makes the proxy read its new configuration.
	restart func(dir string)
}

func accessFor(webserver string) accessProxy {
	switch webserver {
	case "caddy":
		return accessProxy{name: "caddy", container: "remnawave-caddy", open: caddyOpenPlan, close: caddyClosePlan, restart: restartByRecreate("remnawave-caddy")}
	case "haproxy":
		// SIGUSR2 starts a new worker and retires the old one, so no
		// connection is cut, and a configuration HAProxy cannot parse
		// leaves the old workers running.
		return accessProxy{name: "haproxy", open: haproxyOpenPlan, close: haproxyClosePlan, restart: func(dir string) {
			_ = composeRun(dir, "kill", "-s", "USR2", "remnawave-haproxy")
		}}
	}
	return accessProxy{name: "nginx", container: "remnawave-nginx", open: nginxOpenPlan, close: nginxClosePlan, restart: restartByRecreate("remnawave-nginx")}
}

func restartByRecreate(service string) func(string) {
	return func(dir string) {
		_ = composeRun(dir, "down", service)
		_ = composeRun(dir, "up", "-d", service)
	}
}

func printErr(key string) {
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T(key), ui.ColorReset)
}

func printNote(key string) {
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T(key), ui.ColorReset)
}

// port8443Free reports whether the plan may go ahead. Port 8443 in use is
// fine when it is ours already (ours), and an error otherwise.
func port8443Free(ours bool) bool {
	inUse, checked := portChecker("8443")
	switch {
	case !checked:
		printErr("NO_PORT_CHECK_TOOLS")
		return false
	case inUse && !ours:
		printErr("PORT_8443_IN_USE")
		return false
	}
	return true
}

// readConf reads a configuration file, printing errKey when it cannot.
func readConf(path, errKey string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		printErr(errKey)
		return "", false
	}
	return string(data), true
}

func openPanelAccess() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}
	webserver := detectWebserver(dir)
	if webserver == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CONFIG_NOT_FOUND"), dir), ui.ColorReset)
		return
	}
	p := accessFor(webserver)
	plan, ok := p.open(dir)
	if !ok {
		return
	}
	if plan.nothingToDo != "" {
		printNote(plan.nothingToDo)
		return
	}
	if !applyPlan(p, dir, plan) {
		return
	}

	openPort8443()

	registerLinkSecrets(plan.link)
	printNote("OPEN_PANEL_LINK")
	fmt.Printf("%s%s%s\n", ui.ColorWhite, plan.link, ui.ColorReset)
	printErr("PORT_8443_WARNING")
}

// registerLinkSecrets makes the log mask the query values of the panel link:
// the link carries the secret cookie value, and the operator needs it on the
// screen but not in the log file.
func registerLinkSecrets(link string) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}
	for _, values := range u.Query() {
		for _, v := range values {
			ui.RegisterSecret(v)
		}
	}
}

func closePanelAccess() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}
	printNote("PORT_8443_CLOSE")

	webserver := detectWebserver(dir)
	if webserver == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CONFIG_NOT_FOUND"), dir), ui.ColorReset)
		return
	}
	p := accessFor(webserver)
	plan, ok := p.close(dir)
	if !ok {
		return
	}
	if plan.nothingToDo != "" {
		printNote(plan.nothingToDo)
	} else {
		if !applyPlan(p, dir, plan) {
			return
		}
	}
	closePort8443UFW()
}

// nginxOpenPlan adds "listen 8443 ssl;" to the panel's server block.
func nginxOpenPlan(dir string) (accessPlan, bool) {
	confPath := filepath.Join(dir, "nginx.conf")
	conf, ok := readConf(confPath, "NGINX_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	panelDomain := findPanelServerDomain(conf)
	cookie1, cookie2 := findAuthCookies(conf)
	if panelDomain == "" || cookie1 == "" || cookie2 == "" {
		printErr("NGINX_CONF_ERROR")
		return accessPlan{}, false
	}
	if !port8443Free(false) {
		return accessPlan{}, false
	}
	updated, err := addListen8443(conf, panelDomain)
	if err != nil {
		printErr("NGINX_CONF_MODIFY_FAILED")
		return accessPlan{}, false
	}
	return accessPlan{
		confPath: confPath, updated: updated,
		link: fmt.Sprintf("https://%s:8443/auth/login?%s=%s", panelDomain, cookie1, cookie2),
	}, true
}

// nginxClosePlan removes the listen line from the panel's server block.
// Removal is scoped to the domain's own block, found with the same
// brace-depth-aware lookup as addListen8443.
func nginxClosePlan(dir string) (accessPlan, bool) {
	confPath := filepath.Join(dir, "nginx.conf")
	conf, ok := readConf(confPath, "NGINX_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	panelDomain := findPanelServerDomain(conf)
	if panelDomain == "" {
		printErr("NGINX_CONF_ERROR")
		return accessPlan{}, false
	}
	block, found := findServerBlockForDomain(conf, panelDomain)
	if !found || !listen8443RE.MatchString(block.Body(conf)) {
		return accessPlan{nothingToDo: "PORT_8443_NOT_CONFIGURED"}, true
	}
	body := listen8443RE.ReplaceAllString(block.Body(conf), "")
	return accessPlan{confPath: confPath, updated: conf[:block.BodyStart+1] + body + conf[block.BodyEnd-1:]}, true
}

// caddyOpenPlan moves the panel's site to 8443. All the Caddyfile pattern
// matching works on the literal text "{$PANEL_DOMAIN}" (Caddy's own
// env-var reference, written verbatim by internal/caddypanelonly and
// internal/caddypanelfull, never substituted at file-creation time), not on
// the real domain, which is only needed for the link and is read from
// docker-compose.yml's PANEL_DOMAIN environment entry.
func caddyOpenPlan(dir string) (accessPlan, bool) {
	composeData, ok := readConf(filepath.Join(dir, "docker-compose.yml"), "CADDY_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	panelDomain := caddyPanelDomain(composeData)
	if panelDomain == "" {
		printErr("CADDY_CONF_ERROR")
		return accessPlan{}, false
	}
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	caddyfile, ok := readConf(caddyfilePath, "CADDY_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	if strings.Contains(caddyfile, "https://{$PANEL_DOMAIN}:8443 {") {
		return accessPlan{nothingToDo: "PORT_8443_ALREADY_CONFIGURED"}, true
	}
	if !port8443Free(false) {
		return accessPlan{}, false
	}
	caddyfile = strings.ReplaceAll(caddyfile,
		"redir https://{$PANEL_DOMAIN}{uri} permanent",
		"redir https://{$PANEL_DOMAIN}:8443{uri} permanent")
	caddyfile = strings.ReplaceAll(caddyfile,
		"https://{$PANEL_DOMAIN} {",
		"https://{$PANEL_DOMAIN}:8443 {")
	// Removes "bind unix/{$CADDY_SOCKET_PATH}" from the now-renamed block if
	// present (internal/caddypanelfull's Caddyfile has it, since it wraps
	// every domain behind the shared proxy_protocol/tls unix socket;
	// internal/caddypanelonly's never had one in this block, so this is a
	// no-op there).
	caddyfile = removeLineInBlock(caddyfile, "https://{$PANEL_DOMAIN}:8443 {", "bind unix/{$CADDY_SOCKET_PATH}")

	link := fmt.Sprintf("https://%s:8443/auth/login", panelDomain)
	if c1, c2 := caddyExtractCookies(caddyfile); c1 != "" && c2 != "" {
		link = fmt.Sprintf("%s?%s=%s", link, c1, c2)
	}
	return accessPlan{confPath: caddyfilePath, updated: caddyfile, link: link}, true
}

// caddyClosePlan moves the panel's site back to 443. The
// "bind unix/{$CADDY_SOCKET_PATH}" line is only reinserted when the
// install's docker-compose.yml defines CADDY_SOCKET_PATH, which only the
// panel+node flow does.
func caddyClosePlan(dir string) (accessPlan, bool) {
	composeData, ok := readConf(filepath.Join(dir, "docker-compose.yml"), "CADDY_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	if caddyPanelDomain(composeData) == "" {
		printErr("CADDY_CONF_ERROR")
		return accessPlan{}, false
	}
	hasCaddySocketPath := strings.Contains(composeData, "CADDY_SOCKET_PATH")

	caddyfilePath := filepath.Join(dir, "Caddyfile")
	caddyfile, ok := readConf(caddyfilePath, "CADDY_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	if !strings.Contains(caddyfile, "https://{$PANEL_DOMAIN}:8443 {") {
		return accessPlan{nothingToDo: "PORT_8443_NOT_CONFIGURED"}, true
	}
	caddyfile = strings.ReplaceAll(caddyfile, "https://{$PANEL_DOMAIN}:8443 {", "https://{$PANEL_DOMAIN} {")
	if hasCaddySocketPath {
		caddyfile = addLineAfter(caddyfile, "https://{$PANEL_DOMAIN} {", "    bind unix/{$CADDY_SOCKET_PATH}")
	}
	caddyfile = strings.ReplaceAll(caddyfile,
		"redir https://{$PANEL_DOMAIN}:8443{uri} permanent",
		"redir https://{$PANEL_DOMAIN}{uri} permanent")
	return accessPlan{confPath: caddyfilePath, updated: caddyfile}, true
}

// haproxyOpenPlan adds the 8443 frontend between its two marker lines.
func haproxyOpenPlan(dir string) (accessPlan, bool) {
	confPath := filepath.Join(dir, "haproxy.cfg")
	conf, ok := readConf(confPath, "NGINX_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	panelDomain, cookie1, cookie2 := haproxyPanel(conf)
	if !port8443Free(haproxyHas8443(conf)) {
		return accessPlan{}, false
	}
	updated, err := addHAProxy8443(conf)
	if err != nil {
		printErr("NGINX_CONF_ERROR")
		return accessPlan{}, false
	}
	return accessPlan{
		confPath: confPath, updated: updated,
		link: fmt.Sprintf("https://%s:8443/auth/login?%s=%s", panelDomain, cookie1, cookie2),
	}, true
}

// haproxyClosePlan removes the block haproxyOpenPlan added.
func haproxyClosePlan(dir string) (accessPlan, bool) {
	confPath := filepath.Join(dir, "haproxy.cfg")
	conf, ok := readConf(confPath, "NGINX_CONF_ERROR")
	if !ok {
		return accessPlan{}, false
	}
	updated, found := removeHAProxy8443(conf)
	if !found {
		return accessPlan{nothingToDo: "PORT_8443_NOT_CONFIGURED"}, true
	}
	return accessPlan{confPath: confPath, updated: updated}, true
}
