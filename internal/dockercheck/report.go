package dockercheck

import (
	"fmt"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Finding is one problem found in the Docker state. Every finding is a
// warning: the install flow goes on and the operator decides.
type Finding struct {
	Message string
}

// Evaluate turns a Status into findings. A host without Docker has none,
// because the installer is about to put Docker there.
func Evaluate(st Status) []Finding {
	if !st.Installed {
		return nil
	}
	var out []Finding
	add := func(key string, args ...any) {
		out = append(out, Finding{Message: fmt.Sprintf(i18n.T(key), args...)})
	}
	if !st.Daemon {
		add("DOCKERCHECK_NO_DAEMON")
	}
	if !st.ComposeV2 {
		if st.ComposeVersion == "" {
			add("DOCKERCHECK_NO_COMPOSE")
		} else {
			add("DOCKERCHECK_OLD_COMPOSE", st.ComposeVersion)
		}
	}
	if st.Context != "" && st.Context != "default" {
		add("DOCKERCHECK_CONTEXT", st.Context)
	}
	for _, c := range st.Conflicts {
		add("DOCKERCHECK_CONFLICT", c.Name, strings.Join(c.Dirs, ", "))
	}
	return out
}

// Lines renders the Docker state for the environment block.
func Lines(st Status) []string {
	if !st.Installed {
		return []string{i18n.T("DOCKERCHECK_LINE_MISSING")}
	}
	engine, compose := st.ServerVersion, st.ComposeVersion
	if engine == "" {
		engine = "-"
	}
	if compose == "" {
		compose = "-"
	}
	lines := []string{fmt.Sprintf(i18n.T("DOCKERCHECK_LINE"), engine, compose)}
	if n := len(st.Containers) + len(st.Networks) + len(st.Volumes); n > 0 {
		lines = append(lines, fmt.Sprintf(i18n.T("DOCKERCHECK_LINE_OBJECTS"), len(st.Containers), len(st.Networks), len(st.Volumes)))
	}
	return lines
}

// Print writes the Docker lines and findings to the terminal.
func Print(st Status) {
	for _, l := range Lines(st) {
		fmt.Printf("%s%s%s\n", ui.ColorGray, l, ui.ColorReset)
	}
	for _, f := range Evaluate(st) {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, f.Message, ui.ColorReset)
	}
}

// RegistryLine renders the result of RegistryReachable for the environment
// block and warns in yellow when the registry cannot be reached.
func RegistryLine(err error) string {
	if err != nil {
		return fmt.Sprintf(i18n.T("DOCKERCHECK_REGISTRY"), fmt.Sprintf(i18n.T("DOCKERCHECK_REGISTRY_UNREACHABLE"), err))
	}
	return fmt.Sprintf(i18n.T("DOCKERCHECK_REGISTRY"), i18n.T("DOCKERCHECK_REGISTRY_OK"))
}
