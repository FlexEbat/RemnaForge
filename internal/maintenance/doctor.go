package maintenance

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

type check struct {
	name   string
	ok     bool
	detail string
}

func (c check) print() {
	mark, color := "[ ok ]", ui.ColorGreen
	if !c.ok {
		mark, color = "[fail]", ui.ColorRed
	}
	fmt.Printf("%s%s%s %s", color, mark, ui.ColorReset, c.name)
	if c.detail != "" {
		fmt.Printf(" %s- %s%s", ui.ColorGray, c.detail, ui.ColorReset)
	}
	fmt.Println()
}

// Doctor runs a set of read-only health checks against the local
// installation and prints one line per check.
func Doctor() {
	fmt.Println()
	var failed int
	for _, c := range runChecks() {
		c.print()
		if !c.ok {
			failed++
		}
	}
	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_DOCTOR_OK"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s: %d%s\n", ui.ColorYellow, i18n.T("MAINT_DOCTOR_PROBLEMS"), failed, ui.ColorReset)
	}
}

func runChecks() []check {
	var checks []check

	out, err := exec.Command("docker", "compose", "version", "--short").Output()
	checks = append(checks, check{"docker compose", err == nil, strings.TrimSpace(string(out))})

	for _, dir := range []string{panelDir, nodeDir} {
		if exists(filepath.Join(dir, "docker-compose.yml")) {
			checks = append(checks, stackCheck(dir))
		}
	}

	if exists(panelDir) {
		checks = append(checks, panelAPICheck())
		checks = append(checks, envCheck())
	}

	checks = append(checks, certChecks()...)
	checks = append(checks, diskCheck())
	return checks
}

// stackCheck verifies every compose service of dir is running.
func stackCheck(dir string) check {
	all := composeServices(dir, false)
	running := composeServices(dir, true)
	var missing []string
	for _, s := range all {
		found := false
		for _, r := range running {
			if r == s {
				found = true
			}
		}
		if !found {
			missing = append(missing, s)
		}
	}
	name := "containers " + dir
	if len(all) == 0 {
		return check{name, false, "no services found"}
	}
	if len(missing) > 0 {
		return check{name, false, "not running: " + strings.Join(missing, ", ")}
	}
	return check{name, true, fmt.Sprintf("%d services running", len(all))}
}

func composeServices(dir string, runningOnly bool) []string {
	args := []string{"compose", "ps", "--services"}
	if runningOnly {
		args = append(args, "--status", "running")
	} else {
		args = append(args, "-a")
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var res []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

// panelAPICheck asks the panel for its auth status the same way the
// installer does, with the proxy headers the panel insists on.
func panelAPICheck() check {
	req, _ := http.NewRequest("GET", "http://127.0.0.1:3000/api/auth/status", nil)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return check{"panel API 127.0.0.1:3000", false, err.Error()}
	}
	resp.Body.Close()
	return check{"panel API 127.0.0.1:3000", resp.StatusCode < 500, resp.Status}
}

// envCheck flags a panel .env that still uses the pre-3.0 variable names.
func envCheck() check {
	data, err := os.ReadFile(filepath.Join(panelDir, ".env"))
	if err != nil {
		return check{"panel .env", false, err.Error()}
	}
	env := string(data)
	if strings.Contains(env, "JWT_AUTH_SECRET=") || !strings.Contains(env, "APP_SECRET=") {
		return check{"panel .env", false, i18n.T("MAINT_NEEDS_MIGRATION")}
	}
	return check{"panel .env", true, "APP_SECRET present"}
}

// certChecks reports the remaining validity of every certbot certificate.
func certChecks() []check {
	files, _ := filepath.Glob("/etc/letsencrypt/live/*/fullchain.pem")
	var checks []check
	for _, f := range files {
		domain := filepath.Base(filepath.Dir(f))
		data, err := os.ReadFile(f)
		if err != nil {
			checks = append(checks, check{"certificate " + domain, false, err.Error()})
			continue
		}
		block, _ := pem.Decode(data)
		if block == nil {
			checks = append(checks, check{"certificate " + domain, false, "not a PEM file"})
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			checks = append(checks, check{"certificate " + domain, false, err.Error()})
			continue
		}
		days := int(time.Until(cert.NotAfter).Hours() / 24)
		checks = append(checks, check{"certificate " + domain, days > 14, fmt.Sprintf("%d days left", days)})
	}
	return checks
}

func diskCheck() check {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return check{"disk space", false, err.Error()}
	}
	freeGB := float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
	return check{"disk space /", freeGB >= 1, fmt.Sprintf("%.1f GB free", freeGB)}
}
