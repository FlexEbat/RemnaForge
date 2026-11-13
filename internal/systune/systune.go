// Package systune holds the changes to the host system that go beyond what
// an install needs: BBR congestion control, automatic security updates and
// a package upgrade. None of them runs on its own. The operator turns each
// one on or off from the "System tuning" menu.
package systune

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/firewall"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Paths and command runners are variables so tests can point them at a
// temporary directory and a fake.
var (
	// bbrConfPath is the sysctl drop-in this tool owns. A file of our own
	// replaces editing /etc/sysctl.conf: matching substrings there treated a
	// commented-out line as a setting and appended duplicates when the
	// spacing differed, and a drop-in is easy to remove again.
	bbrConfPath = "/etc/sysctl.d/99-remnaforge-bbr.conf"
	// bbrRuntimePath shows the congestion control the kernel uses right now.
	bbrRuntimePath = "/proc/sys/net/ipv4/tcp_congestion_control"
	// autoUpgradesPath is the apt switch for unattended upgrades.
	autoUpgradesPath = "/etc/apt/apt.conf.d/20auto-upgrades"
	// rebootRequiredPath exists when an upgrade needs a reboot.
	rebootRequiredPath = "/var/run/reboot-required"

	// runCmd runs a command and shows its output to the operator.
	runCmd = func(name string, args ...string) error {
		cmd := execx.Command(name, args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	// outputCmd runs a command and returns its standard output.
	outputCmd = func(name string, args ...string) ([]byte, error) {
		cmd := execx.Command(name, args...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		return cmd.Output()
	}
)

const bbrConf = "net.core.default_qdisc = fq\nnet.ipv4.tcp_congestion_control = bbr\n"

// kernelDefaultCongestion is what the kernel uses when no setting asks for
// another algorithm.
const kernelDefaultCongestion = "cubic"

// ErrBBRUnavailable means the kernel did not switch to BBR after the
// setting was applied, usually because the module is missing.
var ErrBBRUnavailable = errors.New("kernel does not offer bbr")

// BBREnabled reports whether the kernel uses BBR right now.
func BBREnabled() bool {
	b, err := os.ReadFile(bbrRuntimePath)
	return err == nil && strings.TrimSpace(string(b)) == "bbr"
}

// EnableBBR writes the sysctl drop-in and applies it. It fails when the
// kernel does not end up on BBR, instead of reporting a success that did
// not happen.
func EnableBBR() error {
	if err := writeBBRConf(bbrConfPath); err != nil {
		return err
	}
	// The module is built in on many kernels; where it is not, loading it
	// is what makes the setting take effect.
	_ = runCmd("modprobe", "tcp_bbr")
	if err := runCmd("sysctl", "-p", bbrConfPath); err != nil {
		return err
	}
	if !BBREnabled() {
		return ErrBBRUnavailable
	}
	return nil
}

// DisableBBR removes the drop-in and puts the running kernel back on the
// default congestion control.
func DisableBBR() error {
	if err := os.Remove(bbrConfPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runCmd("sysctl", "-w", "net.ipv4.tcp_congestion_control="+kernelDefaultCongestion)
}

// writeBBRConf writes the drop-in unless it already holds the wanted
// content, so repeated runs leave the file alone.
func writeBBRConf(path string) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == bbrConf {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(bbrConf), 0644)
}

var unattendedOnRE = regexp.MustCompile(`(?m)^\s*APT::Periodic::Unattended-Upgrade\s+"1"\s*;`)

// UnattendedEnabled reports whether apt runs unattended upgrades daily.
func UnattendedEnabled() bool {
	b, err := os.ReadFile(autoUpgradesPath)
	return err == nil && unattendedOnRE.Match(b)
}

func autoUpgradesConf(on bool) string {
	v := "0"
	if on {
		v = "1"
	}
	return fmt.Sprintf("APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"%s\";\n", v)
}

// EnableUnattended installs unattended-upgrades when it is missing and
// switches the daily run on.
func EnableUnattended() error {
	if !packageInstalled("unattended-upgrades") {
		if err := runCmd("apt-get", "install", "-y", "unattended-upgrades"); err != nil {
			return err
		}
	}
	return writeAutoUpgrades(true)
}

// DisableUnattended switches the daily run off and keeps the package.
func DisableUnattended() error {
	return writeAutoUpgrades(false)
}

func writeAutoUpgrades(on bool) error {
	want := autoUpgradesConf(on)
	if existing, err := os.ReadFile(autoUpgradesPath); err == nil && string(existing) == want {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(autoUpgradesPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(autoUpgradesPath, []byte(want), 0644)
}

// packageInstalled asks dpkg whether pkg is fully installed.
func packageInstalled(pkg string) bool {
	out, err := outputCmd("dpkg-query", "-W", "-f=${Status}", pkg)
	return err == nil && strings.Contains(string(out), "install ok installed")
}

// ParsePending returns the package names a simulated `apt-get upgrade`
// would install. Its lines look like "Inst curl [7.1] (7.2 Ubuntu:24.04)".
func ParsePending(simulation string) []string {
	var names []string
	for _, line := range strings.Split(simulation, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "Inst" {
			names = append(names, fields[1])
		}
	}
	return names
}

// PendingUpgrades refreshes the package lists and lists what an upgrade
// would change.
func PendingUpgrades() ([]string, error) {
	if err := runCmd("apt-get", "update"); err != nil {
		return nil, err
	}
	out, err := outputCmd("apt-get", "-s", "upgrade")
	if err != nil {
		return nil, err
	}
	return ParsePending(string(out)), nil
}

// ApplyUpgrades upgrades every installed package. Config files the
// operator changed are kept.
func ApplyUpgrades() error {
	return runCmd("apt-get", "upgrade", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold")
}

// RebootRequired reports whether the system asks for a reboot.
func RebootRequired() bool {
	_, err := os.Stat(rebootRequiredPath)
	return err == nil
}

// Menu input and output are variables so tests can drive the menu.
var (
	runFirewall = firewall.Run
	readLine    = ui.Reading
	clearScreen = ui.LogClear
	out         = func(color, msg string) { fmt.Printf("%s%s%s\n", color, msg, ui.ColorReset) }
)

func state(on bool) string {
	if on {
		return i18n.T("SYSTUNE_STATE_ON")
	}
	return i18n.T("SYSTUNE_STATE_OFF")
}

func showMenu() {
	fmt.Println()
	out(ui.ColorGreen, i18n.T("SYSTUNE_MENU_TITLE"))
	fmt.Println()
	out(ui.ColorYellow, fmt.Sprintf("1. %s: %s", i18n.T("SYSTUNE_BBR_NAME"), state(BBREnabled())))
	out(ui.ColorYellow, fmt.Sprintf("2. %s: %s", i18n.T("SYSTUNE_UPGRADES_NAME"), state(UnattendedEnabled())))
	out(ui.ColorYellow, "3. "+i18n.T("SYSTUNE_UPDATE_NAME"))
	out(ui.ColorYellow, "4. "+i18n.T("SYSTUNE_FIREWALL_NAME"))
	fmt.Println()
	out(ui.ColorYellow, "0. "+i18n.T("EXIT"))
	fmt.Println()
}

// Menu shows the System tuning menu and loops until the operator leaves.
func Menu() {
	for {
		showMenu()
		switch strings.TrimSpace(readLine(i18n.T("SYSTUNE_PROMPT"))) {
		case "1":
			toggle(i18n.T("SYSTUNE_BBR_NAME"), BBREnabled(), EnableBBR, DisableBBR)
		case "2":
			toggle(i18n.T("SYSTUNE_UPGRADES_NAME"), UnattendedEnabled(), EnableUnattended, DisableUnattended)
		case "3":
			upgradePackages()
		case "4":
			runFirewall(firewall.DefaultEnv(), readLine)
		case "0":
			out(ui.ColorYellow, i18n.T("EXIT"))
			_ = clearScreen()
			return
		default:
			out(ui.ColorYellow, i18n.T("SYSTUNE_INVALID_CHOICE"))
		}
	}
}

func confirm(prompt string) bool {
	answer := strings.ToLower(strings.TrimSpace(readLine(prompt)))
	return answer == "y" || answer == "yes"
}

// toggle flips one switch after the operator confirms. Turning on a switch
// that is on, or off one that is off, is never offered.
func toggle(name string, on bool, enable, disable func() error) {
	if on {
		if !confirm(fmt.Sprintf(i18n.T("SYSTUNE_CONFIRM_DISABLE"), name)) {
			return
		}
		if err := disable(); err != nil {
			out(ui.ColorRed, fmt.Sprintf(i18n.T("SYSTUNE_FAILED"), name, err))
			return
		}
		out(ui.ColorGreen, fmt.Sprintf(i18n.T("SYSTUNE_DISABLED"), name))
		return
	}
	if !confirm(fmt.Sprintf(i18n.T("SYSTUNE_CONFIRM_ENABLE"), name)) {
		return
	}
	if err := enable(); err != nil {
		out(ui.ColorRed, fmt.Sprintf(i18n.T("SYSTUNE_FAILED"), name, err))
		return
	}
	out(ui.ColorGreen, fmt.Sprintf(i18n.T("SYSTUNE_ENABLED"), name))
}

// upgradePackages lists what would change, asks, and only then upgrades.
func upgradePackages() {
	pending, err := PendingUpgrades()
	if err != nil {
		out(ui.ColorRed, fmt.Sprintf(i18n.T("SYSTUNE_FAILED"), i18n.T("SYSTUNE_UPDATE_NAME"), err))
		return
	}
	if len(pending) == 0 {
		out(ui.ColorGreen, i18n.T("SYSTUNE_NO_UPDATES"))
		return
	}
	out(ui.ColorYellow, fmt.Sprintf(i18n.T("SYSTUNE_PENDING"), len(pending), strings.Join(pending, " ")))
	if !confirm(i18n.T("SYSTUNE_CONFIRM_UPGRADE")) {
		return
	}
	if err := ApplyUpgrades(); err != nil {
		out(ui.ColorRed, fmt.Sprintf(i18n.T("SYSTUNE_FAILED"), i18n.T("SYSTUNE_UPDATE_NAME"), err))
		return
	}
	out(ui.ColorGreen, i18n.T("SYSTUNE_UPGRADE_DONE"))
	if RebootRequired() {
		out(ui.ColorYellow, i18n.T("SYSTUNE_REBOOT_REQUIRED"))
	}
}
