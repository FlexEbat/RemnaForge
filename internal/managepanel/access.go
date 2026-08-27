package managepanel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// Original bash (install_remnawave.sh:208-218): show_panel_access().
//
// BUG FIX (not a 1:1 port): the original prints LANG[MENU_9] ("Manage
// certificates domain") as this submenu's title. This menu covers
// temporary panel access on port 8443, unrelated to certificates.
// Using LANG[ACCESS_PANEL] instead, the actual label this feature has
// everywhere else (the parent menu's item 6).
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

// Original bash (install_remnawave.sh:220-243): manage_panel_access().
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
	return ""
}

func portInUse(port string) (inUse bool, checked bool) {
	if out, err := exec.Command("ss", "-tuln").Output(); err == nil {
		return strings.Contains(string(out), ":"+port), true
	}
	if out, err := exec.Command("netstat", "-tuln").Output(); err == nil {
		return strings.Contains(string(out), ":"+port), true
	}
	return false, false
}

var (
	serverNameRE         = regexp.MustCompile(`server_name\s+([^\s;]+)\s*;`)
	proxyPassRemnawaveRE = regexp.MustCompile(`proxy_pass\s+http://remnawave\s*;`)
	listen8443RE         = regexp.MustCompile(`[ \t]*listen[ \t]+8443[ \t]+ssl[ \t]*;[ \t]*\r?\n?`)
	cookieLineRE         = regexp.MustCompile(`map\s+\$http_cookie\s+\$auth_cookie\s*\{[^}]*"~\*(\w+)=(\w+)"`)
	caddyPanelDomainRE   = regexp.MustCompile(`PANEL_DOMAIN=(\S*)`)
	caddyCookieLineRE    = regexp.MustCompile(`header \+Set-Cookie "([^=]+)=([^;]+)`)
)

// findPanelServerDomain is the Go equivalent of:
//
//	grep -B 20 "proxy_pass http://remnawave" nginx.conf | grep "server_name" | grep -v "server_name _" | awk '{print $2}' | sed 's/;//' | head -n 1
//
// BUG FIX (not a 1:1 port): implemented via findTopLevelBlocks' brace-depth
// tracking instead of `strings.Split(conf, "server {")` + a literal
// "proxy_pass http://remnawave;" substring match, which broke on any
// hand-edited nginx.conf using different brace spacing/indentation than
// what this tool itself generates (same fragility the original's
// grep/awk/sed pipeline had).
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

// caddyPanelDomain is the Go equivalent of:
//
//	grep 'PANEL_DOMAIN=' docker-compose.yml | head -n 1 | sed 's/.*PANEL_DOMAIN=//; s/[[:space:]]*$//'
//
// Finds the real domain value, unlike everything else in this file's
// Caddy branches, which pattern-match on the literal text "{$PANEL_DOMAIN}"
// (Caddy's own env-var reference, never substituted by bash or by our
// template's Sprintf) rather than on any actual domain string.
func caddyPanelDomain(compose string) string {
	m := caddyPanelDomainRE.FindStringSubmatch(compose)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// caddyExtractCookies is the Go equivalent of:
//
//	cookie_line=$(grep 'header +Set-Cookie' Caddyfile | head -n 1)
//	cookies_random1=$(echo "$cookie_line" | grep -oP 'Set-Cookie "\K[^=]+')
//	cookies_random2=$(echo "$cookie_line" | grep -oP 'Set-Cookie "[^=]+=\K[^;]+')
func caddyExtractCookies(caddyfile string) (string, string) {
	m := caddyCookieLineRE.FindStringSubmatch(caddyfile)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// addLineAfter inserts newLine as a new line immediately after the first
// line that equals target exactly, mirroring:
//
//	sed -i "/target/a \newLine" file
//
// Line-based, not brace-depth aware, same as the original's sed. Unlike
// findServerBlockForDomain's brace-depth scan above (a deliberate
// improvement over the Nginx original's fragile grep/sed pipeline), this
// reproduces the Caddy original's own line-based matching as-is: the
// Caddy install flows always write a fixed, predictable line layout, so
// there's no hand-edited-file fragility this needs to guard against
// beyond what upstream already tolerates.
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
// "}" (inclusive), mirroring:
//
//	sed -i "/blockStart/,/^}/ { /removeSubstr/d }"
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

// Original bash (src/modules/manage_panel.sh:245-371): open_panel_access().
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
	if webserver == "caddy" {
		openPanelAccessCaddy(dir)
		return
	}

	confPath := filepath.Join(dir, "nginx.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_ERROR"), ui.ColorReset)
		return
	}
	conf := string(data)

	panelDomain := findPanelServerDomain(conf)
	cookie1, cookie2 := findAuthCookies(conf)
	if panelDomain == "" || cookie1 == "" || cookie2 == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_ERROR"), ui.ColorReset)
		return
	}

	if inUse, checked := portInUse("8443"); !checked {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NO_PORT_CHECK_TOOLS"), ui.ColorReset)
		return
	} else if inUse {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("PORT_8443_IN_USE"), ui.ColorReset)
		return
	}

	updated, err := addListen8443(conf, panelDomain)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_MODIFY_FAILED"), ui.ColorReset)
		return
	}
	if err := os.WriteFile(confPath, []byte(updated), 0644); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_MODIFY_FAILED"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = composeRun(dir, "down", "remnawave-nginx")
	_ = composeRun(dir, "up", "-d", "remnawave-nginx")

	_ = exec.Command("ufw", "allow", "from", "0.0.0.0/0", "to", "any", "port", "8443", "proto", "tcp").Run()
	_ = exec.Command("ufw", "reload").Run()

	panelLink := fmt.Sprintf("https://%s:8443/auth/login?%s=%s", panelDomain, cookie1, cookie2)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("OPEN_PANEL_LINK"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorWhite, panelLink, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("PORT_8443_WARNING"), ui.ColorReset)
}

// openPanelAccessCaddy is the Caddy branch of open_panel_access()
// (src/modules/manage_panel.sh:316-370). Unlike the Nginx branch above,
// all the Caddyfile pattern matching here operates on the literal text
// "{$PANEL_DOMAIN}" (Caddy's own env-var reference, written verbatim by
// internal/caddypanelonly and internal/caddypanelfull, never substituted
// at file-creation time), not on the real domain value. The real domain
// is only needed for the final panel_link, and is read separately from
// docker-compose.yml's PANEL_DOMAIN environment entry, exactly as the
// original does.
func openPanelAccessCaddy(dir string) {
	composeData, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}
	panelDomain := caddyPanelDomain(string(composeData))
	if panelDomain == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}

	caddyfilePath := filepath.Join(dir, "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}
	caddyfile := string(data)

	if strings.Contains(caddyfile, "https://{$PANEL_DOMAIN}:8443 {") {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_ALREADY_CONFIGURED"), ui.ColorReset)
		return
	}

	if inUse, checked := portInUse("8443"); !checked {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NO_PORT_CHECK_TOOLS"), ui.ColorReset)
		return
	} else if inUse {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("PORT_8443_IN_USE"), ui.ColorReset)
		return
	}

	caddyfile = strings.ReplaceAll(caddyfile,
		"redir https://{$PANEL_DOMAIN}{uri} permanent",
		"redir https://{$PANEL_DOMAIN}:8443{uri} permanent")
	caddyfile = strings.ReplaceAll(caddyfile,
		"https://{$PANEL_DOMAIN} {",
		"https://{$PANEL_DOMAIN}:8443 {")
	// Removes "bind unix/{$CADDY_SOCKET_PATH}" from the now-renamed block
	// if present (internal/caddypanelfull's Caddyfile has it, since it
	// wraps every domain behind the shared proxy_protocol/tls unix
	// socket; internal/caddypanelonly's Caddyfile never had one in this
	// block, so this is a no-op there, same as the original bash's sed
	// against either file).
	caddyfile = removeLineInBlock(caddyfile, "https://{$PANEL_DOMAIN}:8443 {", "bind unix/{$CADDY_SOCKET_PATH}")

	if err := os.WriteFile(caddyfilePath, []byte(caddyfile), 0644); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_MODIFY_FAILED"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = composeRun(dir, "down", "remnawave-caddy")
	_ = composeRun(dir, "up", "-d", "remnawave-caddy")

	_ = exec.Command("ufw", "allow", "from", "0.0.0.0/0", "to", "any", "port", "8443", "proto", "tcp").Run()
	_ = exec.Command("ufw", "reload").Run()

	cookie1, cookie2 := caddyExtractCookies(caddyfile)
	panelLink := fmt.Sprintf("https://%s:8443/auth/login", panelDomain)
	if cookie1 != "" && cookie2 != "" {
		panelLink = fmt.Sprintf("%s?%s=%s", panelLink, cookie1, cookie2)
	}
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("OPEN_PANEL_LINK"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorWhite, panelLink, ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("PORT_8443_WARNING"), ui.ColorReset)
}

// addListen8443 inserts "listen 8443 ssl;" right after the panel domain's
// server_name line, mirroring:
//
//	sed -i "/server_name $PANEL_DOMAIN;/,/}/{/^[[:space:]]*$/d; s/listen 8443 ssl;//}" nginx.conf
//	sed -i "/server_name $PANEL_DOMAIN;/a \    listen 8443 ssl;" nginx.conf
//
// (strip any stale "listen 8443 ssl;" from that block first, so re-running
// this is idempotent, then insert a fresh one).
//
// BUG FIX (not a 1:1 port): the previous implementation found the block's
// end via `strings.Index(conf[idx:], "\n}")`, which assumes the closing
// brace sits alone at the start of a line with zero indentation. A
// hand-edited nginx.conf that indents its closing braces (e.g. "    }")
// makes this fail to find the block end, or match some other block's
// closing brace and corrupt the file. Now uses
// findServerBlockForDomain's brace-depth-aware boundaries instead.
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

// Original bash (src/modules/manage_panel.sh:373-467): close_panel_access().
func closePanelAccess() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_CLOSE"), ui.ColorReset)

	webserver := detectWebserver(dir)
	if webserver == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CONFIG_NOT_FOUND"), dir), ui.ColorReset)
		return
	}
	if webserver == "caddy" {
		closePanelAccessCaddy(dir)
		return
	}

	confPath := filepath.Join(dir, "nginx.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_ERROR"), ui.ColorReset)
		return
	}
	conf := string(data)

	panelDomain := findPanelServerDomain(conf)
	if panelDomain == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_ERROR"), ui.ColorReset)
		return
	}

	// BUG FIX (not a 1:1 port): previously searched only a fixed 300-byte
	// window after the server_name line for "listen 8443 ssl;". A
	// differently-formatted or re-indented config breaks this: too small
	// a window misses the line, and a block with enough other directives
	// spills past it and matches the next block's line instead. Now uses
	// the same brace-depth-aware block lookup as addListen8443, so removal
	// is scoped to exactly the right block regardless of formatting.
	block, found := findServerBlockForDomain(conf, panelDomain)
	if found && listen8443RE.MatchString(block.Body(conf)) {
		before := conf[:block.BodyStart+1]
		body := listen8443RE.ReplaceAllString(block.Body(conf), "")
		after := conf[block.BodyEnd-1:]
		updated := before + body + after

		if err := os.WriteFile(confPath, []byte(updated), 0644); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_MODIFY_FAILED"), ui.ColorReset)
			return
		}
		_ = composeRun(dir, "down", "remnawave-nginx")
		_ = composeRun(dir, "up", "-d", "remnawave-nginx")
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_NOT_CONFIGURED"), ui.ColorReset)
	}

	closePort8443UFW()
}

// closePort8443UFW is the shared trailing ufw cleanup from
// close_panel_access() (src/modules/manage_panel.sh:421-431 for Nginx,
// 455-465 for Caddy). The original repeats this block byte-for-byte in
// both webserver branches; this is factored into one function instead
// of duplicated, with no behavior change.
func closePort8443UFW() {
	ufwStatus, _ := exec.Command("ufw", "status").Output()
	if strings.Contains(string(ufwStatus), "8443") && strings.Contains(string(ufwStatus), "ALLOW") {
		_ = exec.Command("ufw", "delete", "allow", "from", "0.0.0.0/0", "to", "any", "port", "8443", "proto", "tcp").Run()
		if err := exec.Command("ufw", "reload").Run(); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("UFW_RELOAD_FAILED"), ui.ColorReset)
			return
		}
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PORT_8443_CLOSED"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_ALREADY_CLOSED"), ui.ColorReset)
	}
}

// closePanelAccessCaddy is the Caddy branch of close_panel_access()
// (src/modules/manage_panel.sh:432-466). As in openPanelAccessCaddy, the
// Caddyfile edits match the literal "{$PANEL_DOMAIN}" placeholder text,
// not the real domain.
//
// The reinserted "bind unix/{$CADDY_SOCKET_PATH}" line is unconditional
// in the original (sed's "/pattern/a text" always appends, regardless of
// whether that line existed before "open" removed it), reproduced as-is
// here. This means running open then close on an internal/caddypanelonly
// install (whose Caddyfile never had that line, and whose
// remnawave-caddy container never defines CADDY_SOCKET_PATH in its
// environment) leaves behind a reference to an undefined Caddy env var
// in the panel domain's block. This is a real quirk in the upstream
// bash, not something introduced by this port, and not fixed here:
// unlike the v3.2.0 API field mismatch (internal/caddypanelonly's and
// internal/caddypanelfull's dotEnvTemplate comments), it doesn't break
// every install from day one, only this specific open+close sequence on
// a panel-only Caddy install, matching exactly what a user of the
// original bash script would experience today.
func closePanelAccessCaddy(dir string) {
	composeData, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}
	panelDomain := caddyPanelDomain(string(composeData))
	if panelDomain == "" {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}

	caddyfilePath := filepath.Join(dir, "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CADDY_CONF_ERROR"), ui.ColorReset)
		return
	}
	caddyfile := string(data)

	if strings.Contains(caddyfile, "https://{$PANEL_DOMAIN}:8443 {") {
		caddyfile = strings.ReplaceAll(caddyfile,
			"https://{$PANEL_DOMAIN}:8443 {",
			"https://{$PANEL_DOMAIN} {")
		caddyfile = addLineAfter(caddyfile,
			"https://{$PANEL_DOMAIN} {",
			"    bind unix/{$CADDY_SOCKET_PATH}")
		caddyfile = strings.ReplaceAll(caddyfile,
			"redir https://{$PANEL_DOMAIN}:8443{uri} permanent",
			"redir https://{$PANEL_DOMAIN}{uri} permanent")

		if err := os.WriteFile(caddyfilePath, []byte(caddyfile), 0644); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("NGINX_CONF_MODIFY_FAILED"), ui.ColorReset)
			return
		}
		_ = composeRun(dir, "down", "remnawave-caddy")
		_ = composeRun(dir, "up", "-d", "remnawave-caddy")
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PORT_8443_NOT_CONFIGURED"), ui.ColorReset)
	}

	closePort8443UFW()
}
