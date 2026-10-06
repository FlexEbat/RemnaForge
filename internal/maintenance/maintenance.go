// Package maintenance groups the operator-facing diagnostics and
// upgrade helpers: installed versions, a health check, and the
// migration of a 2.x panel install to 3.x.
package maintenance

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

const (
	panelDir = "/opt/remnawave"
	nodeDir  = "/opt/remnanode"

	announcementsURL = "https://f.docs.rw/c/announces/19"
)

// Menu is the entry point of the "versions, diagnostics and migration"
// main-menu item.
func Menu() {
	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_TITLE"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("MAINT_VERSIONS"), ui.ColorReset)
		fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("MAINT_DOCTOR"), ui.ColorReset)
		fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("MAINT_MIGRATE"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		fmt.Println()

		switch ui.Reading(i18n.T("MAINT_PROMPT")) {
		case "1":
			ShowVersions()
		case "2":
			Doctor()
		case "3":
			MigrateV2ToV3()
		case "0":
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INVALID_CHOICE"), ui.ColorReset)
		}
	}
}

// ShowVersions prints every running Remnawave container with the image
// it was created from and, when the image declares one, its version.
func ShowVersions() {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}",
		"--filter", "name=remnawave", "--filter", "name=remnanode").Output()
	if err != nil {
		fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONTAINER_NOT_RUNNING"), ui.ColorReset)
		return
	}

	fmt.Println()
	for _, line := range lines {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		version := imageVersion(f[1])
		if version != "" {
			version = " (" + version + ")"
		}
		fmt.Printf("%s%-22s%s %s%s  %s%s%s\n", ui.ColorGreen, f[0], ui.ColorReset, f[1], version, ui.ColorGray, f[2], ui.ColorReset)
	}
	fmt.Println()
	fmt.Printf("%s%s %s%s\n", ui.ColorGray, i18n.T("MAINT_ANNOUNCEMENTS"), announcementsURL, ui.ColorReset)
}

func imageVersion(image string) string {
	out, err := exec.Command("docker", "image", "inspect", image, "--format",
		`{{index .Config.Labels "org.opencontainers.image.version"}}`).Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "<no value>" {
		return ""
	}
	return v
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
