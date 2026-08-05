// Package managepanel is a port of src/modules/manage_panel.sh (467
// lines): start/stop/update the panel or node stack, tail logs, run the
// remnawave CLI, and temporarily open/close the panel on port 8443.
//
// Caddy branches throughout the original (open_panel_access/
// close_panel_access) are stubbed, out of scope per project decision.
// Only the Nginx paths are fully ported.
package managepanel

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// findInstallDir is the Go equivalent of the repeated:
//
//	if [ -d "/opt/remnawave" ]; then dir="/opt/remnawave"
//	elif [ -d "/opt/remnanode" ]; then dir="/opt/remnanode"
//	else error DIR_NOT_FOUND; fi
func findInstallDir() (string, bool) {
	for _, d := range []string{"/opt/remnawave", "/opt/remnanode"} {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d, true
		}
	}
	fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("DIR_NOT_FOUND"), ui.ColorReset)
	return "", false
}

// dockerImageRunning is the Go equivalent of:
//
//	docker ps -q --filter "ancestor=$image" | grep -q .
func dockerImageRunning(image string) bool {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "ancestor="+image).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// anyPanelOrNodeRunning checks all three images the original checks:
// remnawave/backend:latest, remnawave/node:latest, remnawave/backend:2.
func anyPanelOrNodeRunning() bool {
	for _, img := range []string{"remnawave/backend:latest", "remnawave/node:latest", "remnawave/backend:2"} {
		if dockerImageRunning(img) {
			return true
		}
	}
	return false
}

func composeRun(dir string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	return cmd.Run()
}

// Original bash (install_remnawave.sh's show_manage_panel_menu, lines
// 4-65): the menu loop. Recursion in bash is a redraw-the-menu loop here.
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
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("MANAGE_PANEL_NODE_INVALID_CHOICE"), ui.ColorReset)
		}
		time.Sleep(1 * time.Second)
	}
}

// Original bash (install_remnawave.sh:67-86): run_remnawave_cli().
// Go doesn't need the fd-juggling (`exec 3>&1 4>&2; exec > /dev/tty`) bash
// uses to get an interactive TTY through a subshell. Wiring the child's
// stdio directly to our own os.Std{in,out,err} gives the same interactive
// `docker exec -it` session.
func runRemnawaveCLI() {
	if !dockerContainerRunning("remnawave") {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CONTAINER_NOT_RUNNING"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("RUNNING_CLI"), ui.ColorReset)
	cmd := exec.Command("docker", "exec", "-it", "-e", "TERM=xterm-256color", "remnawave", "remnawave")
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
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
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

// Original bash (install_remnawave.sh:88-110): start_panel_node().
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
	_ = composeRun(dir, "up", "-d")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_RUN"), ui.ColorReset)
}

// Original bash (install_remnawave.sh:112-133): stop_panel_node().
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
	_ = composeRun(dir, "down")
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("PANEL_STOP"), ui.ColorReset)
}

// composeImageIDs is the Go equivalent of:
//
//	docker compose config --images | sort -u | xargs -I {} docker images -q {} | sort -u
func composeImageIDs(dir string) string {
	cmd := exec.Command("docker", "compose", "config", "--images")
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
		idOut, err := exec.Command("docker", "images", "-q", img).Output()
		if err != nil {
			continue
		}
		ids = append(ids, strings.Fields(string(idOut))...)
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
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

// Original bash (install_remnawave.sh:135-184): update_panel_node().
func updatePanelNode() {
	dir, ok := findInstallDir()
	if !ok {
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATING"), ui.ColorReset)
	time.Sleep(1 * time.Second)

	before := composeImageIDs(dir)

	pullCmd := exec.Command("docker", "compose", "pull")
	pullCmd.Dir = dir
	fmt.Printf("%s%s...%s\n", ui.ColorGray, i18n.T("WAITING"), ui.ColorReset)
	pullOutput, _ := pullCmd.CombinedOutput()

	after := composeImageIDs(dir)

	if before != after || strings.Contains(string(pullOutput), "Pull complete") {
		fmt.Println()
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IMAGES_DETECTED"), ui.ColorReset)
		_ = composeRun(dir, "down")
		time.Sleep(5 * time.Second)
		_ = composeRun(dir, "up", "-d")
		time.Sleep(1 * time.Second)
		_ = exec.Command("docker", "image", "prune", "-f").Run()
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SUCCESS1"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("NO_UPDATE"), ui.ColorReset)
	}
}

// Original bash (install_remnawave.sh:186-206): view_logs().
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
	cmd := exec.Command("docker", "compose", "logs", "-f", "-t")
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
