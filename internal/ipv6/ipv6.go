// Package ipv6 enables or disables IPv6 system-wide via sysctl, and
// reports its current status.
package ipv6

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

const sysctlConf = "/etc/sysctl.conf"

// MenuNav lets the caller wire up navigation back to the parent menu.
type MenuNav struct {
	ReturnToMainMenu func()
}

func showIPv6Menu() {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("IPV6_MENU_TITLE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s1. %s%s\n", ui.ColorYellow, i18n.T("IPV6_ENABLE"), ui.ColorReset)
	fmt.Printf("%s2. %s%s\n", ui.ColorYellow, i18n.T("IPV6_DISABLE"), ui.ColorReset)
	fmt.Println()
	fmt.Printf("%s0. %s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
	fmt.Println()
}

// ManageIPv6 shows the IPv6 submenu and loops until the user exits.
func ManageIPv6(nav MenuNav) {
	for {
		showIPv6Menu()
		option := ui.Reading(i18n.T("IPV6_PROMPT"))

		switch strings.TrimSpace(option) {
		case "1":
			enableIPv6()
			_ = ui.LogClear()
		case "2":
			disableIPv6()
			_ = ui.LogClear()
		case "0":
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("EXIT"), ui.ColorReset)
			_ = ui.LogClear()
			if nav.ReturnToMainMenu != nil {
				nav.ReturnToMainMenu()
			}
			return
		default:
			fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IPV6_INVALID_CHOICE"), ui.ColorReset)
			_ = ui.LogClear()
		}
	}
}

// firstNonLoopbackInterface returns the name of the first non-loopback
// network interface, done natively via net.Interfaces() instead of
// shelling out to ip/awk/grep/head.
func firstNonLoopbackInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Name != "lo" {
			return iface.Name, nil
		}
	}
	return "", fmt.Errorf("no non-loopback interface found")
}

// sysctlGetDisableIPv6All reads net.ipv6.conf.all.disable_ipv6's
// current value via the sysctl file interface rather than shelling out
// to the sysctl binary.
func sysctlGetDisableIPv6All() (int, error) {
	out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.disable_ipv6").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// setSysctlIPv6Lines does a single native read-modify-write of
// /etc/sysctl.conf to set the disable_ipv6 lines for the "all" scope
// and the given interface.
func setSysctlIPv6Lines(interfaceName string, disable int) error {
	keys := []string{
		"net.ipv6.conf.all.disable_ipv6",
		"net.ipv6.conf.default.disable_ipv6",
		"net.ipv6.conf.lo.disable_ipv6",
		fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6", interfaceName),
	}

	existing, err := os.ReadFile(sysctlConf)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(existing), "\n")
	var kept []string
	for _, line := range lines {
		drop := false
		for _, k := range keys {
			// Equivalent to: sed -i '/<key>/d' /etc/sysctl.conf
			if matched, _ := regexp.MatchString(regexp.QuoteMeta(k), line); matched {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}

	for _, k := range keys {
		// Equivalent to: echo "<key> = <0|1>" >> /etc/sysctl.conf
		kept = append(kept, fmt.Sprintf("%s = %d", k, disable))
	}

	return os.WriteFile(sysctlConf, []byte(strings.Join(kept, "\n")), 0644)
}

// sysctlApply is the Go equivalent of: sysctl -p > /dev/null 2>&1
func sysctlApply() {
	_ = exec.Command("sysctl", "-p").Run()
}

// enableIPv6 turns IPv6 back on: sets disable_ipv6 to 0 for "all",
// "default", "lo", and the first non-loopback interface in
// /etc/sysctl.conf, then applies it. A no-op with a status message if
// IPv6 is already enabled.
func enableIPv6() {
	if current, err := sysctlGetDisableIPv6All(); err == nil && current == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IPV6_ALREADY_ENABLED"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ENABLE_IPV6"), ui.ColorReset)

	interfaceName, err := firstNonLoopbackInterface()
	if err != nil {
		ui.ErrorExit(err.Error())
		return
	}

	if err := setSysctlIPv6Lines(interfaceName, 0); err != nil {
		ui.ErrorExit(err.Error())
		return
	}

	sysctlApply()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("IPV6_ENABLED"), ui.ColorReset)
}

// disableIPv6 turns IPv6 off: sets disable_ipv6 to 1 for "all",
// "default", "lo", and the first non-loopback interface in
// /etc/sysctl.conf, then applies it. A no-op with a status message if
// IPv6 is already disabled.
func disableIPv6() {
	if current, err := sysctlGetDisableIPv6All(); err == nil && current == 1 {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("IPV6_ALREADY_DISABLED"), ui.ColorReset)
		return
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("DISABLING_IPV6"), ui.ColorReset)

	interfaceName, err := firstNonLoopbackInterface()
	if err != nil {
		ui.ErrorExit(err.Error())
		return
	}

	if err := setSysctlIPv6Lines(interfaceName, 1); err != nil {
		ui.ErrorExit(err.Error())
		return
	}

	sysctlApply()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("IPV6_DISABLED"), ui.ColorReset)
}
