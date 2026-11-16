package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/certs"
	"github.com/FlexEbat/RemnaForge/internal/dockercheck"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/preflight"
	"github.com/FlexEbat/RemnaForge/internal/ui"
	"github.com/FlexEbat/RemnaForge/internal/verify"
)

type check struct {
	name   string
	ok     bool
	detail string
}

func (c check) result() verify.Result {
	return verify.Result{Name: c.name, OK: c.ok, Detail: c.detail}
}

// Doctor runs the read-only health checks against the local installation,
// prints one line per check, and saves the same lines as a report. The
// container, database, application, proxy, TLS and Telegram checks come
// from internal/verify, the same ones an install flow ends with.
func Doctor() {
	fmt.Println()
	results := runChecks()
	for _, r := range results {
		r.Print()
	}
	failed := verify.Failed(results)
	fmt.Println()
	if failed == 0 {
		fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("MAINT_DOCTOR_OK"), ui.ColorReset)
	} else {
		fmt.Printf("%s%s: %d%s\n", ui.ColorYellow, i18n.T("MAINT_DOCTOR_PROBLEMS"), failed, ui.ColorReset)
	}
	if path, err := verify.Save(verify.ReportDir, results, time.Now()); err == nil && path != "" {
		fmt.Printf("%s%s %s%s\n", ui.ColorGray, i18n.T("REPORT_SAVED"), path, ui.ColorReset)
	} else {
		fmt.Printf("%s%s: %v%s\n", ui.ColorYellow, i18n.T("REPORT_NOT_SAVED"), err, ui.ColorReset)
	}
}

func runChecks() []verify.Result {
	var results []verify.Result
	add := func(cs ...check) {
		for _, c := range cs {
			results = append(results, c.result())
		}
	}

	add(dockerChecks(dockercheck.Inspect(dockercheck.DefaultEnv()))...)

	var plan verify.Plan
	if exists(filepath.Join(panelDir, "docker-compose.yml")) {
		plan.PanelDir = panelDir
	}
	if exists(filepath.Join(nodeDir, "docker-compose.yml")) {
		plan.NodeDir = nodeDir
	}
	plan.Domains = discoverDomains(panelDir, nodeDir)
	results = append(results, verify.Run(verify.DefaultEnv(), plan)...)

	if exists(panelDir) {
		add(envCheck())
	}
	add(certChecks()...)
	add(diskCheck())
	return results
}

// dockerChecks turns the Docker state into doctor checks: engine, Compose
// v2, the context in use, and a compose project name that another
// directory already owns.
func dockerChecks(st dockercheck.Status) []check {
	if !st.Installed {
		return []check{{"docker", false, "not installed"}}
	}
	var checks []check
	if st.Daemon {
		checks = append(checks, check{"docker engine", true, st.ServerVersion})
	} else {
		checks = append(checks, check{"docker engine", false, "the daemon does not answer"})
	}
	switch {
	case st.ComposeV2:
		checks = append(checks, check{"docker compose", true, st.ComposeVersion})
	case st.ComposeVersion != "":
		checks = append(checks, check{"docker compose", false, st.ComposeVersion + ", version 2 is required"})
	default:
		checks = append(checks, check{"docker compose", false, "the plugin is missing"})
	}
	if st.Context != "" && st.Context != "default" {
		checks = append(checks, check{"docker context", false, st.Context + " (not the default one)"})
	}
	for _, dir := range []string{panelDir, nodeDir} {
		if c := dockercheck.ConflictFor(st.Projects, dir); c != nil {
			checks = append(checks, check{"compose project " + c.Name, false, "also used by " + strings.Join(c.Dirs[:len(c.Dirs)-1], ", ")})
		}
	}
	return checks
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
	var results []preflight.Result
	for _, f := range files {
		domain := filepath.Base(filepath.Dir(f))
		days, err := certs.CheckCertExpiry(domain)
		results = append(results, preflight.CertExpiryResult(domain, days, err))
	}
	env := preflight.DefaultCheckEnv()
	results = append(results, preflight.ClockCheck(env), preflight.RenewalCheck(env))
	var checks []check
	for _, r := range results {
		checks = append(checks, check{r.Name, r.Status == preflight.OK, r.Detail})
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

var (
	serverNameRE = regexp.MustCompile(`(?m)^\s*server_name\s+([^\s;]+)\s*;`)
	caddySiteRE  = regexp.MustCompile(`(?m)^(?:https?://)?([A-Za-z0-9{}$_.\-]+\.[A-Za-z0-9{}$_.\-]+|\{\$[A-Z_]+\})\s*\{`)
	envRefRE     = regexp.MustCompile(`\{\$([A-Z0-9_]+)\}`)
)

// discoverDomains returns the host names the installed reverse proxies
// serve: server_name lines of nginx.conf and the site addresses of a
// Caddyfile, where a {$NAME} placeholder is resolved from the .env of the
// same directory. Names it cannot resolve are left out.
func discoverDomains(dirs ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || name == "_" || strings.ContainsAny(name, "{}$*") || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, dir := range dirs {
		if data, err := os.ReadFile(filepath.Join(dir, "nginx.conf")); err == nil {
			for _, m := range serverNameRE.FindAllStringSubmatch(string(data), -1) {
				add(m[1])
			}
		}
		data, err := os.ReadFile(filepath.Join(dir, "Caddyfile"))
		if err != nil {
			continue
		}
		env := map[string]string{}
		if e, err := os.ReadFile(filepath.Join(dir, ".env")); err == nil {
			for _, l := range strings.Split(string(e), "\n") {
				if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
					env[k] = strings.Trim(v, `"'`)
				}
			}
		}
		for _, m := range caddySiteRE.FindAllStringSubmatch(string(data), -1) {
			add(envRefRE.ReplaceAllStringFunc(m[1], func(ref string) string {
				return env[envRefRE.FindStringSubmatch(ref)[1]]
			}))
		}
	}
	sort.Strings(out)
	return out
}
