// Package menu is a port of the top-level menu and dispatch logic in
// install_remnawave.sh: show_menu() (lines 418-445), show_webserver_select()
// (448-458), show_install_menu()/manage_install() (461-642), and the
// top-level `case $OPTION in` block (2208-2313).
//
// Two deliberate departures from a literal 1:1 port, per project decision:
//   - Branding (name/version/wiki link) is this fork's own, not the
//     upstream project's (see version.go).
//   - The main menu is data-driven (a []menuItem) instead of a hardcoded
//     block of numbered fmt.Printf/case statements. Numbers are assigned
//     by position, so adding/removing/reordering an entry (like dropping
//     "Custom extensions by legiz" below) doesn't require renumbering
//     every case label by hand and can't drift out of sync with the
//     printed menu the way the bash version's parallel show_menu()/
//     case-statement could.
//
// Scope for this port (per project decision): Nginx only. Every place the
// original offers a Caddy path, or the WARP module, is replaced with an
// "in development" stub rather than a real implementation. Everything else
// not yet ported (nginx installers, manage_panel, selfsteal_templates,
// certificates, updater, uninstaller, backup) is likewise stubbed and
// clearly marked, so it's obvious what's real.
package menu

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/addnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/certs"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ipv6"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/managepanel"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/nginxnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelfull"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/panelonly"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/selfsteal"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// UpdateAvailable mirrors UPDATE_AVAILABLE=false (install_remnawave.sh:4).
// check_update_status() (the code that flips this to true) isn't ported
// yet, so it always reads false here.
var UpdateAvailable = false

// menuItem is one entry of the main menu. newGroup mirrors the blank-line
// grouping the original show_menu() hardcodes between lines 429/430,
// 434/435 etc.
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
	{label: label("MENU_2"), action: stubAction("choose_reinstall_type")},
	{label: label("MENU_3"), action: func() bool { managepanel.ManagePanel(); return false }},
	{label: label("MENU_4"), newGroup: true, action: func() bool { selfsteal.ManageSelfstealTemplates(); return false }},
	// MENU_5 ("Custom extensions by legiz") intentionally dropped from this fork.
	{label: label("MENU_6"), action: stubAction("warp")}, // WARP: out of scope
	{label: label("MENU_7"), action: stubAction("backup-restore")},
	{label: label("MENU_8"), newGroup: true, action: func() bool {
		ipv6.ManageIPv6(ipv6.MenuNav{ReturnToMainMenu: func() {}})
		return false
	}},
	{label: label("MENU_9"), action: func() bool { certs.ManageCertificates(); return false }},
	{label: label("MENU_10"), newGroup: true, action: stubAction("update_remnawave_reverse")},
	{label: label("MENU_11"), action: func() bool { stub("remove_script"); return true }},
}

// Original bash (install_remnawave.sh:418-445): show_menu(), rebranded and
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

// Original bash (install_remnawave.sh:448-458): show_webserver_select().
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

// Original bash (install_remnawave.sh:461-473): show_install_menu().
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

// Original bash (install_remnawave.sh:474-642): manage_install().
// Nginx paths call into (currently stubbed) installers; Caddy paths are
// always stubbed regardless of scope, per project decision.
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
				stub("caddy install_panel_node") // Caddy: out of scope
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
				stub("caddy install_panel") // Caddy: out of scope
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

		case "3": // add node to panel — fully ported (internal/addnode + internal/api)
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
				stub("caddy install_node") // Caddy: out of scope
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

// mainMenuPrompt fixes a staleness issue introduced by dropping an item:
// the original PROMPT_ACTION string is hardcoded to say "(0-11)", which
// would now be wrong since mainMenuItems only has 10 entries. Rebuilding
// the "0-N" range from len(mainMenuItems) keeps it correct automatically,
// no matter how many items the menu ends up with.
func mainMenuPrompt() string {
	base := i18n.T("PROMPT_ACTION")
	if idx := strings.LastIndex(base, "0-"); idx != -1 {
		if end := strings.Index(base[idx:], ")"); end != -1 {
			base = base[:idx] + fmt.Sprintf("0-%d", len(mainMenuItems)) + base[idx+end:]
		}
	}
	return base
}

// Run is the Go equivalent of the top-level script body (install_remnawave.sh:
// 2202-2313): show_menu, read OPTION, dispatch against mainMenuItems.
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
