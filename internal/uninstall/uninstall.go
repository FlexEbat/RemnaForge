// Package uninstall removes this tool's own state, or additionally the
// installed panel/node stacks (containers, volumes, images of those
// stacks only; nothing else on the host is touched).
package uninstall

import (
	"fmt"
	"os"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/stack"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// dirRemnawave holds this tool's own config/state directory. Duplicated
// as a local constant (like internal/preflight does) to avoid a
// needless cross-package import for a single path string.
const dirRemnawave = "/usr/local/remnawave_reverse"

// binPaths are the locations the binary is normally installed to.
var binPaths = []string{"/usr/local/bin/remnaforge"}

func RemoveScript() {
	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_12"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("REMOVE_SCRIPT_ONLY"), ui.ColorReset)
		fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("REMOVE_SCRIPT_AND_PANEL"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		fmt.Println()
		option := ui.Reading(i18n.T("CERT_PROMPT1"))

		switch option {
		case "1":
			removeScriptOnly()
			return
		case "2":
			removeScriptAndPanel()
			return
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CERT_INVALID_CHOICE"), ui.ColorReset)
		}
	}
}

func removeScriptOnly() {
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CONFIRM_REMOVE_SCRIPT"), ui.ColorReset)
	confirm := ui.Reading("")
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return
	}

	_ = os.RemoveAll(dirRemnawave)
	removeBinary()

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SCRIPT_REMOVED"), ui.ColorReset)
	ui.Exit(0)
}

func removeScriptAndPanel() {
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CONFIRM_REMOVE_ALL"), ui.ColorReset)
	if !stack.ConfirmDelete() {
		return
	}

	for _, dir := range []string{"/opt/remnawave", "/opt/remnanode"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		stack.PromptBackup(dir)
		fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
		if err := stack.Teardown(dir); err != nil {
			fmt.Printf("%s"+i18n.T("TEARDOWN_FAILED")+"%s\n", ui.ColorRed, dir, ui.ColorReset)
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			return
		}
	}

	_ = os.RemoveAll(dirRemnawave)
	removeBinary()

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("ALL_REMOVED"), ui.ColorReset)
	ui.Exit(0)
}

// removeBinary deletes the installed binary at its conventional paths.
func removeBinary() {
	for _, p := range binPaths {
		_ = os.Remove(p)
	}
}
