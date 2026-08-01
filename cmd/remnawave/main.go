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
// Original bash (install_remnawave.sh:2202-2206): the script runs
// check_os and check_root before ever showing the menu; ported here as
// the same two checks, first thing in main().
func main() {
	i18n.SetLanguage("en")

	if err := oscheck.CheckOS(); err != nil {
		ui.ErrorExit(err.Error())
	}
	if err := oscheck.CheckRoot(); err != nil {
		ui.ErrorExit(err.Error())
	}

	menu.Run()
}
