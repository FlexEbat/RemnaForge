// Package reinstall wipes any existing panel or node install, then
// hands off to the same install flows internal/menu uses for a fresh
// install, for both Nginx and Caddy.
package reinstall

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddynode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddypanelfull"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddypanelonly"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/nginxnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelfull"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelonly"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/preflight"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

func showReinstallOptions() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REINSTALL_TYPE_TITLE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_PANEL_NODE"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_PANEL"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_NODE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

func showWebserverSelect() string {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SELECT_WEBSERVER_TITLE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. Nginx%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Printf("%s2. Caddy%s\n", ui.ColorYellow, ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
	return ui.Reading(i18n.T("SELECT_WEBSERVER_PROMPT"))
}

// ChooseReinstallType is the menu-facing entry point.
func ChooseReinstallType() {
	showReinstallOptions()
	option := ui.Reading(i18n.T("REINSTALL_PROMPT"))

	switch option {
	case "1", "2", "3":
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("REINSTALL_WARNING"), ui.ColorReset)
		confirm := ui.Reading("")
		if confirm != "y" && confirm != "Y" {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		}

		reinstallRemnawave()

		if err := preflight.EnsureInstalled(); err != nil {
			return
		}

		switch showWebserverSelect() {
		case "1":
			runInstall(option)
		case "2":
			runInstallCaddy(option)
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_INVALID_CHOICE"), ui.ColorReset)
		}

	case "0":
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)

	default:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INVALID_REINSTALL_CHOICE"), ui.ColorReset)
	}
}

func runInstall(reinstallOption string) {
	var err error
	switch reinstallOption {
	case "1":
		err = panelfull.InstallationPanelNode()
	case "2":
		err = panelonly.InstallationPanelOnly()
	case "3":
		err = nginxnode.InstallationNode()
	}
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}
}

// runInstallCaddy is the Caddy leg of the reinstall type/webserver
// choice: same reinstallOption numbering as runInstall (1=panel+node,
// 2=panel only, 3=node only), routed to the Caddy install packages
// instead of the Nginx ones.
func runInstallCaddy(reinstallOption string) {
	if (reinstallOption == "1" || reinstallOption == "3") && !confirmCaddyProxyProtocolWarning() {
		return
	}
	var err error
	switch reinstallOption {
	case "1":
		err = caddypanelfull.InstallationPanelNode()
	case "2":
		err = caddypanelonly.InstallationPanelOnly()
	case "3":
		err = caddynode.InstallationNode()
	}
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
	}
}

// confirmCaddyProxyProtocolWarning warns about a known issue before an
// install flow that co-locates a node with Caddy: the stock caddy
// Docker image this project uses doesn't include the proxy_protocol
// listener module the Caddyfile needs, so Caddy fails to start (see
// TECH.md). Returns true if the operator wants to proceed anyway.
// Duplicated from internal/menu's identical helper rather than shared,
// the same pattern isValidIPv4's duplication follows elsewhere in this
// codebase.
func confirmCaddyProxyProtocolWarning() bool {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("WARNING_LABEL"), ui.ColorReset)
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CADDY_PROXY_PROTOCOL_WARNING"), ui.ColorReset)
	fmt.Println()
	confirm := ui.Reading(i18n.T("CONFIRM_CONTINUE"))
	return confirm == "y" || confirm == "Y"
}

// reinstallRemnawave tears down any existing panel or node stack before
// a fresh install, printing a plain "please wait" message while it
// waits for `docker compose down` to finish.
func reinstallRemnawave() {
	for _, dir := range []string{"/opt/remnawave", "/opt/remnanode"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
		downCmd := exec.Command("docker", "compose", "down", "-v", "--rmi", "all", "--remove-orphans")
		downCmd.Dir = dir
		_ = downCmd.Run()
		_ = os.RemoveAll(dir)
	}

	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = exec.Command("docker", "system", "prune", "-a", "--volumes", "-f").Run()
}
