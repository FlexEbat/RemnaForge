// Package menu is the top-level menu and dispatch logic: the main menu,
// the webserver-choice submenu, the install submenu, and routing a
// chosen option to the package that handles it.
//
// The main menu is data-driven (a []menuItem) instead of a hardcoded
// block of numbered fmt.Printf/case statements, so adding, removing, or
// reordering an entry doesn't require renumbering every case label by
// hand and can't drift out of sync with the printed menu.
//
// Scope for this project: as of v1.1.3 all three Caddy install flows
// (node-only via internal/caddynode, panel-only via
// internal/caddypanelonly, panel+node via internal/caddypanelfull) are
// wired in below, alongside their Nginx counterparts. The WARP module is
// still out of scope and stubbed. See TECH.md for the current list of
// what's implemented vs. stubbed.
package menu

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/addnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/backuprestore"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddynode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddypanelfull"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/caddypanelonly"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ipv6"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/managepanel"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/nginxnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelfull"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelonly"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/reinstall"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/uninstall"
)

// UpdateAvailable is always false for now: the auto-update check that
// would flip this to true isn't implemented yet (see TECH.md).
var UpdateAvailable = false

// menuItem is one entry of the main menu. newGroup marks where a blank
// line should print before this item, to visually group related menu
// entries.
type menuItem struct {
	label    func() string // resolved at print time so language switches work
	newGroup bool
	action   func() (exit bool)
}

func label(key string) func() string {
	return func() string { return i18n.T(key) }
}

func stubAction(label string) func() bool {
	return func() bool {
		stub(label)
		return false
	}
}

var mainMenuItems = []menuItem{
	{label: label("MENU_1"), action: func() bool { manageInstall(); return false }},
	{label: label("MENU_2"), action: func() bool { reinstall.ChooseReinstallType(); return false }},
	{label: label("MENU_3"), action: func() bool { managepanel.ManagePanel(); return false }},
	{label: label("MENU_4"), newGroup: true, action: func() bool { selfsteal.ManageSelfstealTemplates(); return false }},
	// MENU_5 ("Custom extensions by legiz") intentionally dropped from this fork.
	{label: label("MENU_6"), action: stubAction("warp")}, // WARP: out of scope
	{label: label("MENU_7"), action: func() bool {
		if err := backuprestore.Run(); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
		}
		return false
	}},
	{label: label("MENU_8"), newGroup: true, action: func() bool {
		ipv6.ManageIPv6(ipv6.MenuNav{ReturnToMainMenu: func() {}})
		return false
	}},
	{label: label("MENU_9"), action: func() bool { certs.ManageCertificates(); return false }},
	{label: label("MENU_10"), newGroup: true, action: stubAction("update_remnawave_reverse")},
	{label: label("MENU_11"), action: func() bool { uninstall.RemoveScript(); return true }},
}

// renumbered per mainMenuItems above.
func showMenu() {
	fmt.Printf("%s%s%s\n", ui.ColorGreen, AppName, ui.ColorReset)
	if UpdateAvailable {
		fmt.Printf("%sVersion %s %s%s%s%s\n", ui.ColorGray, AppVersion, ui.ColorRed, i18n.T("AVAILABLE_UPDATE"), ui.ColorReset, ui.ColorReset)
	} else {
		fmt.Printf("%sVersion %s%s\n", ui.ColorGray, AppVersion, ui.ColorReset)
	}
	fmt.Printf("%sWiki: %s%s\n", ui.ColorGray, WikiURL, ui.ColorReset)
	fmt.Println()

	for i, item := range mainMenuItems {
		if item.newGroup {
			fmt.Println()
		}
		fmt.Printf("%s%d. %s%s\n", ui.ColorYellow, i+1, item.label(), ui.ColorReset)
	}
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

// Returns the chosen option string, same as reading WEBSERVER_OPTION.
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

func showInstallMenu() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("INSTALL_MENU_TITLE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_PANEL_NODE"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_PANEL"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_ADD_NODE"), ui.ColorReset)
	fmt.Printf("%s4. %s%s\n", ui.ColorYellow, i18n.T("INSTALL_NODE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

func stub(label string) {
	fmt.Printf("%s[%s] %s%s\n", ui.ColorGray, label, i18n.InDevelopment(), ui.ColorReset)
}

func manageInstall() {
	for {
		showInstallMenu()
		option := ui.Reading(i18n.T("INSTALL_PROMPT"))

		switch option {
		case "1": // install panel+node
			fmt.Println()
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("WARNING_LABEL"), ui.ColorReset)
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PANEL_NODE_SINGLE_SERVER_WARNING"), ui.ColorReset)
			fmt.Println()
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("PANEL_NODE_SINGLE_SERVER_RECOMMENDATION"), ui.ColorReset)
			fmt.Println()
			confirm := ui.Reading(i18n.T("CONFIRM_CONTINUE"))
			if confirm != "y" && confirm != "Y" {
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
				_ = ui.LogClear()
				continue
			}
			switch showWebserverSelect() {
			case "1":
				if err := panelfull.InstallationPanelNode(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "2":
				if err := caddypanelfull.InstallationPanelNode(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "0":
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
				_ = ui.LogClear()
				return
			default:
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_INVALID_CHOICE"), ui.ColorReset)
				time.Sleep(2 * time.Second)
				_ = ui.LogClear()
				continue
			}
			time.Sleep(2 * time.Second)
			_ = ui.LogClear()

		case "2": // install panel only
			switch showWebserverSelect() {
			case "1":
				if err := panelonly.InstallationPanelOnly(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "2":
				if err := caddypanelonly.InstallationPanelOnly(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "0":
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
				_ = ui.LogClear()
				return
			default:
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_INVALID_CHOICE"), ui.ColorReset)
				time.Sleep(2 * time.Second)
				_ = ui.LogClear()
				continue
			}
			time.Sleep(2 * time.Second)
			_ = ui.LogClear()

		case "3": // add node to panel, fully ported (internal/addnode + internal/api)
			addnode.AddNodeToPanel()
			_ = ui.LogClear()
			return

		case "4": // install node only
			switch showWebserverSelect() {
			case "1":
				if err := nginxnode.InstallationNode(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "2":
				if err := caddynode.InstallationNode(); err != nil {
					fmt.Printf("%s%s%s\n", ui.ColorRed, err.Error(), ui.ColorReset)
				}
			case "0":
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
				_ = ui.LogClear()
				return
			default:
				fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_INVALID_CHOICE"), ui.ColorReset)
				time.Sleep(2 * time.Second)
				_ = ui.LogClear()
				continue
			}
			time.Sleep(2 * time.Second)
			_ = ui.LogClear()

		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			_ = ui.LogClear()
			return

		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_INVALID_CHOICE"), ui.ColorReset)
			time.Sleep(2 * time.Second)
			_ = ui.LogClear()
		}
	}
}

// mainMenuPrompt builds the "0-N" range from len(mainMenuItems) rather
// than hardcoding it, so it stays correct automatically no matter how
// many items the menu ends up with.
func mainMenuPrompt() string {
	base := i18n.T("PROMPT_ACTION")
	if idx := strings.LastIndex(base, "0-"); idx != -1 {
		if end := strings.Index(base[idx:], ")"); end != -1 {
			base = base[:idx] + fmt.Sprintf("0-%d", len(mainMenuItems)) + base[idx+end:]
		}
	}
	return base
}

// Run shows the main menu, reads a choice, and dispatches against
// mainMenuItems, looping until the user exits.
func Run() {
	for {
		showMenu()
		option := ui.Reading(mainMenuPrompt())

		if option == "0" {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		}

		n, err := strconv.Atoi(option)
		if err != nil || n < 1 || n > len(mainMenuItems) {
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INVALID_CHOICE"), ui.ColorReset)
			time.Sleep(2 * time.Second)
			_ = ui.LogClear()
			continue
		}

		if exit := mainMenuItems[n-1].action(); exit {
			return
		}

		time.Sleep(2 * time.Second)
		_ = ui.LogClear()
	}
}
