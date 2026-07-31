package main

import (
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/menu"
)

// Entry point for the Go port of remnawave-reverse-proxy
// (originally install_remnawave.sh + src/**, Nginx-only scope).
func main() {
	i18n.SetLanguage("en")
	menu.Run()
}
