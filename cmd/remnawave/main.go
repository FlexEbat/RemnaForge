package main

import (
	"fmt"
	"os"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/maintenance"
	"github.com/FlexEbat/RemnaForge/internal/menu"
	"github.com/FlexEbat/RemnaForge/internal/oscheck"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Entry point: file logging, then language selection, then
// check_root/check_os, then the main menu.
func main() {
	if len(os.Args) > 1 && runSubcommand(os.Args[1]) {
		return
	}

	if cleanup, err := ui.EnableFileLogging(); err == nil {
		defer cleanup()
	}

	i18n.EnsureLanguageSelected()

	if err := oscheck.CheckOS(); err != nil {
		ui.ErrorExit(err.Error())
	}
	if err := oscheck.CheckRoot(); err != nil {
		ui.ErrorExit(err.Error())
	}

	menu.Run()
}

// runSubcommand handles the non-interactive commands and reports whether
// arg was one of them.
func runSubcommand(arg string) bool {
	switch arg {
	case "--version", "-v", "version":
		fmt.Println(menu.AppName, menu.AppVersion)
	case "doctor":
		i18n.SetLanguage("en")
		maintenance.Doctor()
	case "versions":
		i18n.SetLanguage("en")
		maintenance.ShowVersions()
	case "--help", "-h", "help":
		fmt.Println("usage: remnaforge [--version | doctor | versions]")
		fmt.Println("without arguments the interactive menu is started")
	default:
		return false
	}
	return true
}
