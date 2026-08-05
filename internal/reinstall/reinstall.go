// Package reinstall is a port of show_reinstall_options(),
// choose_reinstall_type(), and reinstall_remnawave()
// (install_remnawave.sh:646-727). It wipes any existing panel or node
// install, then hands off to the same install flows internal/menu uses
// for a fresh install.
//
// Caddy branches from the original's REINSTALL_OPTION/WEBSERVER_OPTION
// matrix are stubbed. The project supports Nginx only.
package reinstall

import (
	"fmt"
	"os"
	"os/exec"

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
// Original bash (install_remnawave.sh:658-710): choose_reinstall_type().
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
			fmt.Printf("%s[caddy reinstall] %s%s\n", ui.ColorGray, i18n.InDevelopment(), ui.ColorReset)
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

// reinstallRemnawave is the Go equivalent of install_remnawave.sh:712-727:
// tear down any existing panel or node stack before a fresh install.
// The original runs `docker compose down` in the background and shows a
// spinner while it waits; this port runs it synchronously and prints a
// plain "please wait" message instead, the same simplification used
// throughout the install flows for the original's spinner() calls.
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
