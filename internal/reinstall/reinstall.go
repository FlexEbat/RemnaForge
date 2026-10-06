// Package reinstall wipes any existing panel or node install, then
// hands off to the same install flows internal/menu uses for a fresh
// install, for both Nginx and Caddy.
package reinstall

import (
	"fmt"
	"os"

	"github.com/FlexEbat/RemnaForge/internal/caddynode"
	"github.com/FlexEbat/RemnaForge/internal/caddypanelfull"
	"github.com/FlexEbat/RemnaForge/internal/caddypanelonly"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/nginxnode"
	"github.com/FlexEbat/RemnaForge/internal/panelfull"
	"github.com/FlexEbat/RemnaForge/internal/panelonly"
	"github.com/FlexEbat/RemnaForge/internal/preflight"
	"github.com/FlexEbat/RemnaForge/internal/stack"
	"github.com/FlexEbat/RemnaForge/internal/ui"
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

		if err := reinstallRemnawave(); err != nil {
			return
		}

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
	if reinstallOption == "1" || reinstallOption == "3" {
		noteCaddyBuild()
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

// noteCaddyBuild tells the operator the Caddy image is being built
// locally (not just pulled), so a longer wait before containers come
// up is expected. See the identical helper in internal/menu for why.
// Duplicated rather than shared, the same pattern isValidIPv4's
// duplication follows elsewhere in this codebase.
func noteCaddyBuild() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CADDY_BUILD_NOTE"), ui.ColorReset)
}

// reinstallRemnawave tears down any existing panel or node stack before
// a fresh install, printing a plain "please wait" message while it
// waits for `docker compose down` to finish.
func reinstallRemnawave() error {
	for _, dir := range []string{"/opt/remnawave", "/opt/remnanode"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		stack.PromptBackup(dir)
		fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
		if err := stack.Teardown(dir); err != nil {
			fmt.Printf("%s"+i18n.T("TEARDOWN_FAILED")+"%s\n", ui.ColorRed, dir, ui.ColorReset)
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			return err
		}
	}
	return nil
}
