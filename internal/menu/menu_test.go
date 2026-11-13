package menu

import (
	"testing"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
)

func TestMainMenuLabelsResolve(t *testing.T) {
	for lang := range map[string]bool{"en": true, "ru": true} {
		i18n.SetLanguage(lang)
		seen := map[string]bool{}
		for i, item := range mainMenuItems {
			l := item.label()
			if l == "" {
				t.Errorf("%s: item %d has an empty label", lang, i+1)
			}
			if seen[l] {
				t.Errorf("%s: label %q appears twice", lang, l)
			}
			seen[l] = true
		}
	}
	i18n.SetLanguage("en")
}

func TestMainMenuHasSystemTuning(t *testing.T) {
	i18n.SetLanguage("en")
	for _, item := range mainMenuItems {
		if item.label() == "System tuning" {
			return
		}
	}
	t.Error("main menu has no System tuning entry")
}
