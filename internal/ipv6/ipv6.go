// Package ipv6 is a line-for-line port of src/modules/ipv6.sh (89 lines).
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

// MenuNav lets the caller wire up navigation back to the parent menu
// (equivalent to calling remnawave_reverse() at line 34 of the original).
type MenuNav struct {
	ReturnToMainMenu func()
}

// Original bash (src/modules/ipv6.sh:4-13):
//
//	show_ipv6_menu() {
//	    echo -e ""
//	    echo -e "${COLOR_GREEN}${LANG[IPV6_MENU_TITLE]}${COLOR_RESET}"
//	    echo -e ""
//	    echo -e "${COLOR_YELLOW}1. ${LANG[IPV6_ENABLE]}${COLOR_RESET}"
//	    echo -e "${COLOR_YELLOW}2. ${LANG[IPV6_DISABLE]}${COLOR_RESET}"
//	    echo -e ""
//	    echo -e "${COLOR_YELLOW}0. ${LANG[EXIT]}${COLOR_RESET}"
//	    echo -e ""
//	}
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

// Original bash (src/modules/ipv6.sh:15-43):
//
//	manage_ipv6() {
//	    show_ipv6_menu
//	    reading "${LANG[IPV6_PROMPT]}" IPV6_OPTION
//	    case $IPV6_OPTION in
//	        1) enable_ipv6; sleep 2; log_clear; manage_ipv6 ;;
//	        2) disable_ipv6; sleep 2; log_clear; manage_ipv6 ;;
//	        0) echo -e "${COLOR_YELLOW}${LANG[EXIT]}${COLOR_RESET}"; log_clear; remnawave_reverse ;;
//	        *) echo -e "${COLOR_YELLOW}${LANG[IPV6_INVALID_CHOICE]}${COLOR_RESET}"; sleep 2; log_clear; manage_ipv6 ;;
//	    esac
//	}
//
// The recursive re-invocation of manage_ipv6 (the bash pattern for "redraw
// the menu") is kept as a loop in Go instead of literal recursion.
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

// firstNonLoopbackInterface is the Go equivalent of the original's:
//
//	interface_name=$(ip -o link show | awk -F': ' '{print $2}' | grep -v lo | head -n 1)
//
// (src/modules/ipv6.sh:52 and :75), done natively instead of shelling out
// to ip/awk/grep/head.
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

// sysctlGetDisableIPv6All is the Go equivalent of:
//
//	sysctl -n net.ipv6.conf.all.disable_ipv6
func sysctlGetDisableIPv6All() (int, error) {
	out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.disable_ipv6").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// setSysctlIPv6Lines replaces the sed -i / echo >> pair used four times in
// each of enable_ipv6/disable_ipv6 (src/modules/ipv6.sh:54-62, 77-85) with
// a single native read-modify-write of /etc/sysctl.conf.
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

// Original bash (src/modules/ipv6.sh:45-66):
//
//	enable_ipv6() {
//	    if [ "$(sysctl -n net.ipv6.conf.all.disable_ipv6)" -eq 0 ]; then
//	        echo -e "${COLOR_YELLOW}${LANG[IPV6_ALREADY_ENABLED]}${COLOR_RESET}"
//	        return 0
//	    fi
//	    echo -e "${COLOR_YELLOW}${LANG[ENABLE_IPV6]}${COLOR_RESET}"
//	    interface_name=$(ip -o link show | awk -F': ' '{print $2}' | grep -v lo | head -n 1)
//	    sed -i '/net.ipv6.conf.all.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i '/net.ipv6.conf.default.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i '/net.ipv6.conf.lo.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i "/net.ipv6.conf.$interface_name.disable_ipv6/d" /etc/sysctl.conf
//	    echo "net.ipv6.conf.all.disable_ipv6 = 0" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.default.disable_ipv6 = 0" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.lo.disable_ipv6 = 0" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.$interface_name.disable_ipv6 = 0" >> /etc/sysctl.conf
//	    sysctl -p > /dev/null 2>&1
//	    echo -e "${COLOR_GREEN}${LANG[IPV6_ENABLED]}${COLOR_RESET}"
//	}
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

// Original bash (src/modules/ipv6.sh:68-89):
//
//	disable_ipv6() {
//	    if [ "$(sysctl -n net.ipv6.conf.all.disable_ipv6)" -eq 1 ]; then
//	        echo -e "${COLOR_YELLOW}${LANG[IPV6_ALREADY_DISABLED]}${COLOR_RESET}"
//	        return 0
//	    fi
//	    echo -e "${COLOR_YELLOW}${LANG[DISABLING_IPV6]}${COLOR_RESET}"
//	    interface_name=$(ip -o link show | awk -F': ' '{print $2}' | grep -v lo | head -n 1)
//	    sed -i '/net.ipv6.conf.all.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i '/net.ipv6.conf.default.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i '/net.ipv6.conf.lo.disable_ipv6/d' /etc/sysctl.conf
//	    sed -i "/net.ipv6.conf.$interface_name.disable_ipv6/d" /etc/sysctl.conf
//	    echo "net.ipv6.conf.all.disable_ipv6 = 1" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.default.disable_ipv6 = 1" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.lo.disable_ipv6 = 1" >> /etc/sysctl.conf
//	    echo "net.ipv6.conf.$interface_name.disable_ipv6 = 1" >> /etc/sysctl.conf
//	    sysctl -p > /dev/null 2>&1
//	    echo -e "${COLOR_GREEN}${LANG[IPV6_DISABLED]}${COLOR_RESET}"
//	}
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
