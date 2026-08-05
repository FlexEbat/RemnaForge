// Package uninstall is a port of remove_script() (install_remnawave.sh:
// 242-306): lets the user remove just this tool's own state, or wipe the
// installed panel/node (docker containers, images, volumes) too.
//
// One adaptation (not a 1:1 port): the original always has a fixed
// self-installed location. install_script_if_missing() copies itself to
// ${DIR_REMNAWAVE}remnawave_reverse and symlinks /usr/local/bin/
// remnawave_reverse (install_remnawave.sh:308-316). This fork has no
// installer script yet (see README: build-from-source only for now), so
// no single guaranteed binary path exists to remove. "Remove script
// only" removes the config/state directory
// (/usr/local/remnawave_reverse, which holds the saved panel API token,
// see internal/api.DirRemnawave) and removes a binary at the
// conventional /usr/local/bin/remnawave-easy-install path if one exists
// there, rather than assuming it does.
package uninstall

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// dirRemnawave mirrors DIR_REMNAWAVE="/usr/local/remnawave_reverse/"
// (install_remnawave.sh:5). Duplicated as a local constant (like
// internal/preflight does) to avoid a needless cross-package import for a
// single path string.
const dirRemnawave = "/usr/local/remnawave_reverse"

// conventionalBinPath is where a manually-copied build of this tool would
// plausibly live, matching the README's suggested build/install location.
// The original bash has no equivalent for this specific path; see the
// package doc comment above.
const conventionalBinPath = "/usr/local/bin/remnawave-easy-install"

// Original bash (install_remnawave.sh:242-306): remove_script().
func RemoveScript() {
	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_11"), ui.ColorReset)
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

// Original bash (install_remnawave.sh:254-267).
func removeScriptOnly() {
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CONFIRM_REMOVE_SCRIPT"), ui.ColorReset)
	confirm := ui.Reading("")
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return
	}

	_ = os.RemoveAll(dirRemnawave)
	_ = os.Remove(conventionalBinPath)

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SCRIPT_REMOVED"), ui.ColorReset)
	os.Exit(0)
}

// Original bash (install_remnawave.sh:268-295).
func removeScriptAndPanel() {
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CONFIRM_REMOVE_ALL"), ui.ColorReset)
	confirm := ui.Reading("")
	if confirm != "y" && confirm != "Y" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		return
	}

	for _, dir := range []string{"/opt/remnawave", "/opt/remnanode"} {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
		downCmd := exec.Command("docker", "compose", "down", "-v", "--rmi", "all", "--remove-orphans")
		downCmd.Dir = dir
		if err := downCmd.Run(); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CHANGE_DIR_FAILED"), dir), ui.ColorReset)
		}
		_ = os.RemoveAll(dir)
	}

	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	_ = exec.Command("docker", "system", "prune", "-a", "--volumes", "-f").Run()

	_ = os.RemoveAll(dirRemnawave)
	_ = os.Remove(conventionalBinPath)

	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("ALL_REMOVED"), ui.ColorReset)
	os.Exit(0)
}
