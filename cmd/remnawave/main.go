package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/maintenance"
	"github.com/FlexEbat/RemnaForge/internal/menu"
	"github.com/FlexEbat/RemnaForge/internal/oscheck"
	"github.com/FlexEbat/RemnaForge/internal/runctx"
	"github.com/FlexEbat/RemnaForge/internal/runlock"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Entry point: file logging, then language selection, then
// check_root/check_os, then the main menu.
func main() {
	args, dry := splitDryRun(os.Args[1:])
	runctx.SetDryRun(dry)
	if len(args) > 0 && runSubcommand(args[0]) {
		return
	}

	// A dry run changes nothing, so it keeps no log and takes no lock.
	if !dry {
		if cleanup, err := ui.EnableFileLogging(); err == nil {
			defer cleanup()
		}
	}

	i18n.EnsureLanguageSelected()

	if err := oscheck.CheckOS(); err != nil {
		ui.ErrorExit(err.Error())
	}
	if err := oscheck.CheckRoot(); err != nil {
		ui.ErrorExit(err.Error())
	}

	// The lock comes after the root check because creating its directory
	// needs root, and before the menu because every mutating flow starts
	// there. Read-only subcommands return earlier and take no lock.
	if dry {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("DRY_RUN_BANNER"), ui.ColorReset)
	} else {
		lock, err := runlock.Acquire(runlock.DefaultPath)
		var busy *runlock.BusyError
		switch {
		case errors.As(err, &busy):
			ui.ErrorExit(fmt.Sprintf(i18n.T("RUNLOCK_BUSY"), busy.PID, busy.Since.Format("2006-01-02 15:04:05")))
		case err != nil:
			ui.ErrorExit(fmt.Sprintf(i18n.T("RUNLOCK_FAILED"), err))
		}
		defer lock.Release()
	}

	menu.Run()
}

// splitDryRun removes every --dry-run from args and reports whether there
// was one.
func splitDryRun(args []string) (rest []string, dry bool) {
	for _, a := range args {
		if a == "--dry-run" {
			dry = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, dry
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
		fmt.Println("usage: remnaforge [--dry-run] [--version | doctor | versions]")
		fmt.Println("without arguments the interactive menu is started")
		fmt.Println("--dry-run asks the questions and shows what would change, without changing anything")
	default:
		return false
	}
	return true
}
