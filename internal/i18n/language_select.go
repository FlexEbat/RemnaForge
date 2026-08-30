package i18n

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// langFilePath is where the chosen interface language is persisted
// across runs.
const langFilePath = "/usr/local/remnawave_reverse/selected_language"

// EnsureLanguageSelected loads the saved language choice, or asks for
// one and saves it if none exists yet. Call this once from main(),
// before showing the main menu. An invalid saved value is treated as
// corrupt: the file is removed and the user is prompted again.
//
// An invalid interactive choice re-prompts instead of exiting the
// program, so a single mistyped key on first run doesn't abort setup.
func EnsureLanguageSelected() {
	if lang, ok := loadSavedLanguage(); ok {
		SetLanguage(lang)
		return
	}
	SetLanguage(promptLanguage())
}

// loadSavedLanguage stores "en" or "ru" directly, self-documenting
// instead of a magic digit.
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

// promptLanguage shows the language picker and persists the choice.
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
