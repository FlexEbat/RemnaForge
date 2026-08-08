package main

import (
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/menu"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/oscheck"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// Entry point for the Go port of remnawave-reverse-proxy
// (originally install_remnawave.sh + src/**, Nginx-only scope).
//
// Original bash (install_remnawave.sh:2189-2206): log_entry, then
// language selection, then check_root/check_os, then the main menu.
// main() follows the same order.
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
