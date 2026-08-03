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
func showPanelAccess() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_9"), ui.ColorReset)
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

// Original bash (install_remnawave.sh:245-371): open_panel_access().
// The Caddy branch is stubbed - out of scope per project decision.
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
		fmt.Printf("%s[caddy panel access] %s%s\n", ui.ColorGray, i18n.InDevelopment(), ui.ColorReset)
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
// end via `strings.Index(conf[idx:], "\n}")` - which assumes the closing
// brace sits alone at the start of a line with zero indentation. Any
// hand-edited nginx.conf that indents its closing braces (e.g. "    }")
// would make this either fail to find the block end, or - worse - match
// some other block's closing brace entirely, corrupting the file. Now
// uses findServerBlockForDomain's brace-depth-aware boundaries instead.
func addListen8443(conf, panelDomain string) (string, error) {
	block, found := findServerBlockForDomain(conf, panelDomain)
	if !found {
		return "", fmt.Errorf("server block for %q not found", panelDomain)
	}

	marker := "server_name " + panelDomain + ";"
	body := block.Body(conf)
	nameIdx := serverNameRE.FindStringIndex(body)
	if nameIdx == nil {
		return "", fmt.Errorf("server_name not found in block for %q", panelDomain)
	}
	_ = marker // kept only for the doc comment's sed-equivalence reference

	insertAt := block.BodyStart + 1 + nameIdx[1]
	blockEnd := block.BodyEnd - 1 // position of the block's own closing '}'

	rest := listen8443RE.ReplaceAllString(conf[insertAt:blockEnd], "")
	before := conf[:insertAt]
	after := conf[blockEnd:]

	return before + "\n    listen 8443 ssl;" + rest + after, nil
}

// Original bash (install_remnawave.sh:373-467): close_panel_access().
// The Caddy branch is stubbed - out of scope per project decision.
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
		fmt.Printf("%s[caddy panel access] %s%s\n", ui.ColorGray, i18n.InDevelopment(), ui.ColorReset)
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
	// window after the server_name line for "listen 8443 ssl;" - fragile
	// against differently-formatted/re-indented configs (too small a
	// window misses it; a block containing enough other directives could
	// spill past it and match the next block's line instead). Now uses
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
