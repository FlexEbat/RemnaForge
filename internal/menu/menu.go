// Package menu is a port of the top-level menu and dispatch logic in
// install_remnawave.sh: show_menu() (lines 418-445), show_webserver_select()
// (448-458), show_install_menu()/manage_install() (461-642), and the
// top-level `case $OPTION in` block (2208-2313).
//
// Scope for this port (per project decision): Nginx only. Every place the
// original offers a Caddy path, or the WARP module, is replaced with an
// "in development" stub rather than a real implementation. Everything else
// not yet ported (nginx installers, manage_panel, selfsteal_templates,
// custom legiz templates, certificates, updater, uninstaller, backup) is
// likewise stubbed and clearly marked, so it's obvious what's real.
package menu

import (
	"fmt"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/addnode"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ipv6"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// ScriptVersion mirrors SCRIPT_VERSION="3.0.0" (install_remnawave.sh:3).
const ScriptVersion = "3.0.0"

// UpdateAvailable mirrors UPDATE_AVAILABLE=false (install_remnawave.sh:4).
// check_update_status() (the code that flips this to true) isn't ported
// yet, so it always reads false here - see stubUpdateCheck().
var UpdateAvailable = false

// Original bash (install_remnawave.sh:418-445): show_menu().
func showMenu() {
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_TITLE"), ui.ColorReset)
	if UpdateAvailable {
		fmt.Printf("%s%s%s\n", ui.ColorGray, fmt.Sprintf(i18n.T("VERSION_LABEL"), ScriptVersion+" "+ui.ColorRed+i18n.T("AVAILABLE_UPDATE")+ui.ColorReset), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorGray, fmt.Sprintf(i18n.T("VERSION_LABEL"), ScriptVersion), ui.ColorReset)
	}
	fmt.Printf("%sWiki: https://wiki.egam.es/%s\n", ui.ColorGray, ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("MENU_1"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("MENU_2"), ui.ColorReset)
	fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("MENU_3"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s4. %s%s\n", ui.ColorYellow, i18n.T("MENU_4"), ui.ColorReset)
	fmt.Printf("%s5. %s%s\n", ui.ColorYellow, i18n.T("MENU_5"), ui.ColorReset)
	fmt.Printf("%s6. %s%s\n", ui.ColorYellow, i18n.T("MENU_6"), ui.ColorReset)
	fmt.Printf("%s7. %s%s\n", ui.ColorYellow, i18n.T("MENU_7"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s8. %s%s\n", ui.ColorYellow, i18n.T("MENU_8"), ui.ColorReset)
	fmt.Printf("%s9. %s%s\n", ui.ColorYellow, i18n.T("MENU_9"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s10. %s%s\n", ui.ColorYellow, i18n.T("MENU_10"), ui.ColorReset)
	fmt.Printf("%s11. %s%s\n", ui.ColorYellow, i18n.T("MENU_11"), ui.ColorReset)
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
				stub("nginx install_panel_node") // src/nginx/install_panel_node.sh (538 lines) - not ported yet
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
				stub("nginx install_panel") // src/nginx/install_panel.sh (610 lines) - not ported yet
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
				stub("nginx install_node") // src/nginx/install_node.sh (203 lines) - not ported yet
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

// Run is the Go equivalent of the top-level script body (install_remnawave.sh:
// 2202-2313): show_menu, read OPTION, dispatch. The bash version re-execs the
// whole script (via the `remnawave_reverse`/`rr` alias) to redraw the main
// menu; here that's just the outer loop.
func Run() {
	for {
		showMenu()
		option := ui.Reading(i18n.T("PROMPT_ACTION"))

		switch option {
		case "1":
			manageInstall()

		case "2": // choose_reinstall_type — install_remnawave.sh:658+
			stub("choose_reinstall_type")

		case "3": // manage_panel module — src/modules/manage_panel.sh (467 lines)
			stub("manage_panel")

		case "4": // selfsteal_templates module — src/modules/selfsteal_templates.sh (150 lines)
			stub("selfsteal_templates")

		case "5": // custom legiz templates
			stub("manage_custom_legiz")

		case "6": // WARP native — out of scope
			stub("warp")

		case "7": // backup and restore (external script)
			stub("backup-restore")

		case "8": // IPv6 — fully ported
			ipv6.ManageIPv6(ipv6.MenuNav{ReturnToMainMenu: func() {}})

		case "9": // certificate management
			stub("manage_certificates")

		case "10": // updater
			stub("update_remnawave_reverse")

		case "11": // uninstaller
			stub("remove_script")
			return

		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return

		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INVALID_CHOICE"), ui.ColorReset)
		}

		time.Sleep(2 * time.Second)
		_ = ui.LogClear()
	}
}
