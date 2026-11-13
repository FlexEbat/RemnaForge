package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/sysinfo"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// dirRemnawave holds this tool's own config/state directory. Duplicated
// here (rather than importing internal/api, which also has this
// constant) to avoid a needless cross-package dependency for a single
// path string.
const dirRemnawave = "/usr/local/remnawave_reverse/"

const installMarker = dirRemnawave + "install_packages"

// aptPackages is the list of apt packages every install flow needs.
var aptPackages = []string{
	"ca-certificates", "curl", "jq", "ufw", "wget", "gnupg", "unzip", "nano",
	"dialog", "git", "certbot", "python3-certbot-dns-cloudflare",
	"locales", "dnsutils", "coreutils", "grep",
	"gawk", "python3-pip",
}

// EnsureInstalled guards every install flow's dependency on
// packages/docker/certbot with one unified check that requires the
// marker file, working docker, and certbot together, since every real
// install flow in this project ends up needing certbot anyway (see
// internal/certs). Every flow benefits from the stronger guarantee.
func EnsureInstalled() error {
	satisfied := packagesAlreadySatisfied()
	if err := reportEnvironment(sysinfo.DefaultEnv(), dockercheck.DefaultEnv(), !satisfied); err != nil {
		return err
	}
	if satisfied {
		return nil
	}
	return InstallPackages()
}

// reportEnvironment prints the host block at the start of an install flow
// and returns an error when the host is too small to install on. The
// repository check runs only when packages are about to be installed.
func reportEnvironment(env sysinfo.Env, docker dockercheck.Env, checkRepos bool) error {
	info, _ := sysinfo.Collect(env)
	if checkRepos {
		sysinfo.CheckRepos(env, &info, aptPackages)
	}
	if err := sysinfo.Print(info); err != nil {
		return err
	}
	dockercheck.Print(dockercheck.Inspect(docker))
	if checkRepos && docker.HTTPStatus != nil {
		// Images are pulled during an install, so a blocked registry is worth
		// knowing before it.
		err := dockercheck.RegistryReachable(docker)
		color := ui.ColorGray
		if err != nil {
			color = ui.ColorYellow
		}
		fmt.Printf("%s%s%s\n", color, dockercheck.RegistryLine(err), ui.ColorReset)
	}
	fmt.Println()
	return nil
}

func packagesAlreadySatisfied() bool {
	if _, err := os.Stat(installMarker); err != nil {
		return false
	}
	if !dockercheck.Working(dockercheck.DefaultEnv()) {
		return false
	}
	if _, err := exec.LookPath("certbot"); err != nil {
		return false
	}
	return true
}

// InstallPackages bootstraps every dependency this project needs on a
// fresh server: apt packages, cron and Docker. It changes nothing else
// on the host: BBR, automatic updates and the firewall are opt-in and
// live in the System tuning menu (internal/systune).
func InstallPackages() error {
	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("INSTALL_PACKAGES"), ui.ColorReset)

	if err := run("apt-get", "update", "-y"); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_UPDATE_LIST"), ui.ColorReset)
		return err
	}

	if err := run("apt-get", append([]string{"install", "-y"}, aptPackages...)...); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_PACKAGES"), ui.ColorReset)
		return err
	}

	// Cron package.
	if !dpkgInstalled("cron") {
		if err := run("apt-get", "install", "-y", "cron"); err != nil {
			fmt.Printf("%s%s%s\n", ui.ColorRed, i18n.T("ERROR_INSTALL_CRON"), ui.ColorReset)
			return err
		}
	}

	// Cron service active + enabled.
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

	// Docker.
	if err := dockercheck.Ensure(dockercheck.DefaultEnv()); err != nil {
		return err
	}

	if err := os.MkdirAll(dirRemnawave, 0755); err == nil {
		_ = os.WriteFile(installMarker, nil, 0644)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("SUCCESS_INSTALL"), ui.ColorReset)

	return nil
}

func run(name string, args ...string) error {
	cmd := execx.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dpkgInstalled(pkg string) bool {
	out, err := execx.Command("dpkg", "-l").Output()
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
	return execx.Command("systemctl", "is-active", "--quiet", unit).Run() == nil
}

func systemdEnabled(unit string) bool {
	return execx.Command("systemctl", "is-enabled", "--quiet", unit).Run() == nil
}

// ensureDocker installs Docker via get.docker.com's install script,
// streamed from net/http directly into `sh`'s stdin rather than being
// written to a temp file first, so nothing touches disk and there's no
// leftover script file to clean up.
