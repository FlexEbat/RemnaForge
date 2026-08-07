package i18n

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// langFilePath mirrors LANG_FILE="${DIR_REMNAWAVE}selected_language"
// (install_remnawave.sh:6). Duplicated as a local constant, the same
// pattern internal/preflight and internal/uninstall use for
// DIR_REMNAWAVE, to avoid a needless cross-package import for one path.
const langFilePath = "/usr/local/remnawave_reverse/selected_language"

// EnsureLanguageSelected is the Go equivalent of the top-level startup
// sequence at install_remnawave.sh:2191-2200: load the saved language
// choice, or ask for one and save it if none exists yet. Call this once
// from main(), before showing the main menu.
//
// BUG FIX (not a 1:1 port): the original's interactive prompt calls
// error() on an invalid choice, which exits the entire program over a
// single mistyped key on first run. This re-prompts instead until it
// gets a valid choice, matching how every other menu in this port
// handles bad input.
func EnsureLanguageSelected() {
	if lang, ok := loadSavedLanguage(); ok {
		SetLanguage(lang)
		return
	}
	SetLanguage(promptLanguage())
}

// Original bash (install_remnawave.sh:17-30): load_language().
// The original stores "1" or "2"; this port stores "en" or "ru" directly,
// self-documenting instead of a magic digit. Any other content is
// treated as corrupt, matching the original's self-healing behavior:
// delete the file and fall through to prompting again.
func loadSavedLanguage() (string, bool) {
	data, err := os.ReadFile(langFilePath)
	if err != nil {
		return "", false
	}

	saved := strings.TrimSpace(string(data))
	if saved == "en" || saved == "ru" {
		return saved, true
	}

	_ = os.Remove(langFilePath)
	return "", false
}

// Original bash (install_remnawave.sh:39-45, 2192-2199): show_language()
// plus the top-level prompt/case block that reads LANG_OPTION and saves
// LANG_FILE.
func promptLanguage() string {
	for {
		SetLanguage("en") // menu itself always renders in English until chosen
		ui.Println("")
		ui.Println(ui.ColorGreen + T("CHOOSE_LANG") + ui.ColorReset)
		ui.Println("")
		ui.Println(ui.ColorYellow + "1. " + T("LANG_EN") + ui.ColorReset)
		ui.Println(ui.ColorYellow + "2. " + T("LANG_RU") + ui.ColorReset)
		ui.Println("")

		choice := ui.Reading("Choose option (1-2):")

		var lang string
		switch choice {
		case "1":
			lang = "en"
		case "2":
			lang = "ru"
		default:
			ui.Println(ui.ColorRed + "Invalid choice. Please select 1-2." + ui.ColorReset)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(langFilePath), 0755); err == nil {
			_ = os.WriteFile(langFilePath, []byte(lang), 0644)
		}
		return lang
	}
}
