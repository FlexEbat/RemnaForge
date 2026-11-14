package cfgfile

import (
	"fmt"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

func defaultDecide(path, diff string, canMerge bool) Decision {
	fmt.Printf("\n%s%s%s\n", ui.ColorYellow, fmt.Sprintf(i18n.T("CFGFILE_CHANGED"), path), ui.ColorReset)
	for _, l := range strings.Split(diff, "\n") {
		color := ui.ColorGray
		switch {
		case strings.HasPrefix(l, "+ "):
			color = ui.ColorGreen
		case strings.HasPrefix(l, "- "):
			color = ui.ColorRed
		}
		fmt.Printf("%s%s%s\n", color, l, ui.ColorReset)
	}
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CFGFILE_LEGEND"), ui.ColorReset)
	choices := "k/r"
	fmt.Printf("%s  k  %s\n  r  %s\n", ui.ColorGray, i18n.T("CFGFILE_KEEP"), i18n.T("CFGFILE_REPLACE"))
	if canMerge {
		fmt.Printf("  m  %s\n", i18n.T("CFGFILE_MERGE"))
		choices = "k/m/r"
	}
	fmt.Print(ui.ColorReset)
	switch strings.ToLower(strings.TrimSpace(ui.Reading(fmt.Sprintf(i18n.T("CFGFILE_PROMPT"), choices)))) {
	case "r", "replace":
		return ReplaceIt
	case "m", "merge":
		if canMerge {
			return MergeIt
		}
	}
	return Keep
}
