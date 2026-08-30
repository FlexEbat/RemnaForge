package main

import (
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/menu"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/oscheck"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// Entry point: file logging, then language selection, then
// check_root/check_os, then the main menu.
func main() {
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
