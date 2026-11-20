// Package managepanel handles start/stop/update of the panel or node
// stack, tailing logs, running the remnawave CLI, and temporarily
// opening/closing the panel on port 8443. The Caddy branches of
// opening/closing panel access live alongside their Nginx counterparts
// in internal/managepanel/access.go.
package managepanel

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/rotate"
	"github.com/FlexEbat/RemnaForge/internal/stack"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// installDirs is a variable so tests can use temporary directories.
var installDirs = []string{"/opt/remnawave", "/opt/remnanode"}

// findInstallDir returns whichever of /opt/remnawave or /opt/remnanode
// exists.
func findInstallDir() (string, bool) {
	for _, d := range installDirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d, true
		}
	}
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DIR_NOT_FOUND"), ui.ColorReset)
	return "", false
}

// anyPanelOrNodeRunning reports whether a container of a Remnawave panel or
// node image runs, whatever its tag. The list used to hold three exact tags
// (backend:latest, node:latest, backend:2), and every install since 3.x runs
// remnawave/backend:3, so a panel-only host looked stopped: Stop said "not
// running" and Update refused.
func anyPanelOrNodeRunning() bool {
	out, err := execx.Command("docker", "ps", "--format", "{{.Image}}").Output()
	return err == nil && hasRemnawaveImage(string(out))
}

// hasRemnawaveImage reports whether one of the image names, one per line, is
// a Remnawave backend or node image.
func hasRemnawaveImage(images string) bool {
	for _, line := range strings.Split(images, "\n") {
		img := strings.TrimSpace(line)
		if strings.HasPrefix(img, "remnawave/backend:") || strings.HasPrefix(img, "remnawave/node:") ||
			img == "remnawave/backend" || img == "remnawave/node" {
			return true
		}
	}
	return false
}

// printComposeFailure tells the operator that a docker compose command
// failed. Start and Stop used to report success whatever the command did.
func printComposeFailure(subcommand string, err error) {
	fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("COMPOSE_COMMAND_FAILED"), subcommand, err), ui.ColorReset)
}

func composeRun(dir string, args ...string) error {
	cmd := execx.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	return cmd.Run()
}

// ManagePanel is the top-level menu loop for this package.
func ManagePanel() {
	for {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MENU_3"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("START_PANEL_NODE"), ui.ColorReset)
		fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("STOP_PANEL_NODE"), ui.ColorReset)
		fmt.Printf("%s3. %s%s\n", ui.ColorYellow, i18n.T("UPDATE_PANEL_NODE"), ui.ColorReset)
		fmt.Printf("%s4. %s%s\n", ui.ColorYellow, i18n.T("VIEW_LOGS"), ui.ColorReset)
		fmt.Printf("%s5. %s%s\n", ui.ColorYellow, i18n.T("REMNAWAVE_CLI"), ui.ColorReset)
		fmt.Printf("%s6. %s%s\n", ui.ColorYellow, i18n.T("ACCESS_PANEL"), ui.ColorReset)
		fmt.Printf("%s7. %s%s\n", ui.ColorYellow, i18n.T("ROTATE_NAME"), ui.ColorReset)
		fmt.Println()
		fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
		fmt.Println()
		option := ui.Reading(i18n.T("MANAGE_PANEL_NODE_PROMPT"))

		switch option {
		case "1":
			startPanelNode()
		case "2":
			stopPanelNode()
		case "3":
			updatePanelNode()
		case "4":
			viewLogs()
		case "5":
			runRemnawaveCLI()
		case "6":
			managePanelAccess()
		case "7":
			rotate.Run(rotate.DefaultEnv("/opt/remnawave"), ui.Reading)
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("MANAGE_PANEL_NODE_INVALID_CHOICE"), ui.ColorReset)
		}
		time.Sleep(1 * time.Second)
	}
}

// The child's stdio is wired directly to our own os.Std{in,out,err},
// which gives an interactive `docker exec -it` session.
func runRemnawaveCLI() {
	if !dockerContainerRunning("remnawave") {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONTAINER_NOT_RUNNING"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RUNNING_CLI"), ui.ColorReset)
	cmd := execx.Interactive("docker", "exec", "-it", "-e", "TERM=xterm-256color", "remnawave", "remnawave")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err == nil {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CLI_SUCCESS"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CLI_FAILED"), ui.ColorReset)
	}
}

func dockerContainerRunning(name string) bool {
	out, err := execx.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func startPanelNode() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	if anyPanelOrNodeRunning() {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_RUNNING"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s...%s\n", ui.ColorYellow, i18n.T("STARTING_PANEL_NODE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	if err := composeRun(dir, "up", "-d"); err != nil {
		printComposeFailure("up", err)
		return
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_RUN"), ui.ColorReset)
}

func stopPanelNode() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	if !anyPanelOrNodeRunning() {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_STOPPED"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s...%s\n", ui.ColorYellow, i18n.T("STOPPING_REMNAWAVE"), ui.ColorReset)
	time.Sleep(1 * time.Second)
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	if err := composeRun(dir, "down"); err != nil {
		printComposeFailure("down", err)
		return
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_STOP"), ui.ColorReset)
}

// composeImageIDs returns the deduplicated, sorted image IDs backing
// this compose project's services.
func composeImageIDs(dir string) string {
	cmd := execx.Command("docker", "compose", "config", "--images")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	images := uniqueSortedLines(string(out))
	if len(images) == 0 {
		return ""
	}

	var ids []string
	for _, img := range images {
		idOut, err := execx.Command("docker", "images", "-q", img).Output()
		if err != nil {
			continue
		}
		ids = append(ids, strings.Fields(string(idOut))...)
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}

// lastLines returns the last n non-empty lines of s.
func lastLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func uniqueSortedLines(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func updatePanelNode() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATING"), ui.ColorReset)

	if execx.Command("docker", "inspect", "-f", "{{.State.Running}}", "remnawave-db").Run() == nil {
		dest, err := stack.BackupPanel(dir)
		if err != nil {
			fmt.Printf("%s%s: %v%s\n", ui.ColorRed, i18n.T("BACKUP_FAILED"), err, ui.ColorReset)
			return
		}
		fmt.Printf("%s"+i18n.T("BACKUP_DONE")+"%s\n", ui.ColorGreen, dest, ui.ColorReset)
	}

	before := composeImageIDs(dir)

	pullCmd := execx.Command("docker", "compose", "pull")
	pullCmd.Dir = dir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	pullOutput, pullErr := pullCmd.CombinedOutput()
	if pullErr != nil {
		// A failed pull leaves the images as they were, which used to be
		// reported as "no update available".
		printComposeFailure("pull", pullErr)
		if tail := lastLines(string(pullOutput), 3); tail != "" {
			fmt.Println(tail)
		}
		return
	}

	after := composeImageIDs(dir)

	if before != after || strings.Contains(string(pullOutput), "Pull complete") {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IMAGES_DETECTED"), ui.ColorReset)
		if err := composeRun(dir, "up", "-d", "--remove-orphans"); err != nil {
			fmt.Printf("%s%v%s\n", ui.ColorRed, err, ui.ColorReset)
			return
		}
		fmt.Printf("%s%s%s\n", ui.ColorGray, i18n.T("PRUNE_IMAGES_NOTICE"), ui.ColorReset)
		_ = execx.Command("docker", "image", "prune", "-f").Run()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SUCCESS1"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_UPDATE"), ui.ColorReset)
	}
}

func viewLogs() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	if !anyPanelOrNodeRunning() {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("CONTAINER_NOT_RUNNING"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("VIEW_LOGS"), ui.ColorReset)
	cmd := execx.Interactive("docker", "compose", "logs", "-f", "-t")
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
