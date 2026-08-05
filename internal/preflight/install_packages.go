package preflight

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/remnawave/remnawave-reverse-proxy-go/internal/i18n"
	"github.com/remnawave/remnawave-reverse-proxy-go/internal/ui"
)

// dirRemnawave mirrors DIR_REMNAWAVE="/usr/local/remnawave_reverse/"
// (install_remnawave.sh:5). Duplicated here (rather than importing
// internal/api, which also has this constant) to avoid a needless
// cross-package dependency for a single path string.
const dirRemnawave = "/usr/local/remnawave_reverse/"

const installMarker = dirRemnawave + "install_packages"

// aptPackages mirrors the package list at install_remnawave.sh:1237.
var aptPackages = []string{
	"ca-certificates", "curl", "jq", "ufw", "wget", "gnupg", "unzip", "nano",
	"dialog", "git", "certbot", "python3-certbot-dns-cloudflare",
	"unattended-upgrades", "locales", "dnsutils", "coreutils", "grep",
	"gawk", "python3-pip",
}

// EnsureInstalled is the Go equivalent of the guard repeated before every
// install flow in the original, e.g. (install_remnawave.sh:500-501):
//
//	if [ ! -f "${DIR_REMNAWAVE}install_packages" ] || ! command -v docker ... || ! command -v certbot ...; then
//	    install_packages || { ...; return; }
//	fi
//
// The original has several slightly different variants of this guard
// (some check certbot, some don't, depending on which install flow calls
// it). This port uses one unified check that requires the marker file,
// working docker, and certbot together, since every real install flow
// in this port ends up needing certbot anyway (see internal/certs).
// Every flow benefits from the stronger guarantee.
func EnsureInstalled() error {
	if packagesAlreadySatisfied() {
		return nil
	}
	return InstallPackages()
}

func packagesAlreadySatisfied() bool {
	if _, err := os.Stat(installMarker); err != nil {
		return false
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	if exec.Command("docker", "info").Run() != nil {
		return false
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		return false
	}
	return true
}

// Original bash (install_remnawave.sh:1229-1326): install_packages().
// Inline comments mark the corresponding original line ranges.
func InstallPackages() error {
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_PACKAGES"), ui.ColorReset)

	// Lines 1232-1235.
	if err := run("apt-get", "update", "-y"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_UPDATE_LIST"), ui.ColorReset)
		return err
	}

	// Lines 1237-1240.
	if err := run("apt-get", append([]string{"install", "-y"}, aptPackages...)...); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_PACKAGES"), ui.ColorReset)
		return err
	}

	// Lines 1242-1247: cron package.
	if !dpkgInstalled("cron") {
		if err := run("apt-get", "install", "-y", "cron"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_CRON"), ui.ColorReset)
			return err
		}
	}

	// Lines 1249-1260: cron service active + enabled.
	if !systemdActive("cron") {
		if err := run("systemctl", "start", "cron"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("START_CRON_ERROR"), ui.ColorReset)
			return err
		}
	}
	if !systemdEnabled("cron") {
		if err := run("systemctl", "enable", "cron"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("START_CRON_ERROR"), ui.ColorReset)
			return err
		}
	}

	// Lines 1262-1298: Docker.
	if err := ensureDocker(); err != nil {
		return err
	}

	// Lines 1300-1307: BBR congestion control.
	enableBBR()

	// Lines 1309-1313: UFW baseline rules.
	if err := run("ufw", "allow", "22/tcp", "comment", "SSH"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CONFIGURE_UFW"), ui.ColorReset)
		return err
	}
	if err := run("ufw", "allow", "443/tcp", "comment", "HTTPS"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CONFIGURE_UFW"), ui.ColorReset)
		return err
	}
	if err := run("ufw", "--force", "enable"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CONFIGURE_UFW"), ui.ColorReset)
		return err
	}

	// Lines 1315-1321: unattended-upgrades.
	if err := configureUnattendedUpgrades(); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_CONFIGURE_UPGRADES"), ui.ColorReset)
		return err
	}

	// Lines 1323-1325.
	if err := os.MkdirAll(dirRemnawave, 0755); err == nil {
		_ = os.WriteFile(installMarker, nil, 0644)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SUCCESS_INSTALL"), ui.ColorReset)

	return nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dpkgInstalled(pkg string) bool {
	out, err := exec.Command("dpkg", "-l").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "ii" && fields[1] == pkg {
			return true
		}
	}
	return false
}

func systemdActive(unit string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
}

func systemdEnabled(unit string) bool {
	return exec.Command("systemctl", "is-enabled", "--quiet", unit).Run() == nil
}

// ensureDocker is the Go equivalent of install_remnawave.sh:1262-1298.
// BUG FIX (not a 1:1 port): the original downloads get.docker.com's
// install script to /tmp/get-docker.sh with curl, then runs `sh` on that
// file. Here the script streams from net/http directly into `sh`'s
// stdin, so nothing touches disk and there's no leftover
// /tmp/get-docker.sh file to clean up.
func ensureDocker() error {
	dockerOK := func() bool {
		if _, err := exec.LookPath("docker"); err != nil {
			return false
		}
		return exec.Command("docker", "info").Run() == nil
	}

	if !dockerOK() {
		fmt.Printf("%sInstalling Docker via get.docker.com...%s\n", ui.ColorYellow, ui.ColorReset)

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Get("https://get.docker.com")
		if err != nil || resp.StatusCode != http.StatusOK {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOWNLOAD_DOCKER_KEY"), ui.ColorReset)
			return fmt.Errorf("failed to download get-docker.sh")
		}
		script, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOWNLOAD_DOCKER_KEY"), ui.ColorReset)
			return err
		}

		cmd := exec.Command("sh")
		cmd.Stdin = strings.NewReader(string(script))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_DOCKER"), ui.ColorReset)
			return err
		}
	}

	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_INSTALLED"), ui.ColorReset)
		return err
	}

	if !systemdActive("docker") {
		if err := run("systemctl", "start", "docker"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_START_DOCKER"), ui.ColorReset)
			return err
		}
	}
	if !systemdEnabled("docker") {
		if err := run("systemctl", "enable", "docker"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_ENABLE_DOCKER"), ui.ColorReset)
			return err
		}
	}

	if !dockerOK() {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_DOCKER_NOT_WORKING"), ui.ColorReset)
		return fmt.Errorf("docker still not working after install")
	}

	return nil
}

// enableBBR is the Go equivalent of install_remnawave.sh:1300-1307
// (best-effort, matching the original's lack of error handling here).
func enableBBR() {
	const sysctlConf = "/etc/sysctl.conf"
	data, _ := os.ReadFile(sysctlConf)
	content := string(data)

	appended := false
	if !strings.Contains(content, "net.core.default_qdisc = fq") {
		content += "net.core.default_qdisc = fq\n"
		appended = true
	}
	if !strings.Contains(content, "net.ipv4.tcp_congestion_control = bbr") {
		content += "net.ipv4.tcp_congestion_control = bbr\n"
		appended = true
	}
	if appended {
		_ = os.WriteFile(sysctlConf, []byte(content), 0644)
	}
	_ = exec.Command("sysctl", "-p").Run()
}

// configureUnattendedUpgrades is the Go equivalent of
// install_remnawave.sh:1315-1321.
func configureUnattendedUpgrades() error {
	const confPath = "/etc/apt/apt.conf.d/50unattended-upgrades"
	f, err := os.OpenFile(confPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err == nil {
		_, _ = f.WriteString(`Unattended-Upgrade::Mail "root";` + "\n")
		f.Close()
	}

	debconf := exec.Command("debconf-set-selections")
	debconf.Stdin = strings.NewReader("unattended-upgrades unattended-upgrades/enable_auto_updates boolean true\n")
	_ = debconf.Run()

	if err := run("dpkg-reconfigure", "-f", "noninteractive", "unattended-upgrades"); err != nil {
		return err
	}
	return run("systemctl", "restart", "unattended-upgrades")
}
