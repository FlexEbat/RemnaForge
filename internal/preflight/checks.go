package preflight

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// Status is the outcome of one check.
type Status int

const (
	// OK means the check passed.
	OK Status = iota
	// Warn means something is off but an install can work.
	Warn
	// Fail means an install is likely to fail.
	Fail
)

// Result is one line of the pre-flight table.
type Result struct {
	Name   string
	Status Status
	Detail string
	// Remedy says what to do about a Warn or a Fail.
	Remedy string
}

// CheckEnv holds every way the checks reach the host. DefaultCheckEnv uses
// the real one.
type CheckEnv struct {
	// Run runs a command and returns its combined output.
	Run func(name string, args ...string) (string, error)
	// Listen tries to bind a TCP port on all addresses and closes it again.
	Listen func(port int) error
	// Glob lists files.
	Glob func(pattern string) ([]string, error)
}

// DefaultCheckEnv returns the CheckEnv of the running host.
func DefaultCheckEnv() CheckEnv {
	return CheckEnv{
		Run: func(name string, args ...string) (string, error) {
			out, err := execx.Command(name, args...).CombinedOutput()
			return string(out), err
		},
		Listen: func(port int) error {
			l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			return l.Close()
		},
		Glob: filepath.Glob,
	}
}

const letsencryptLiveGlob = "/etc/letsencrypt/live/*/fullchain.pem"

func tr(key string, args ...any) string {
	if len(args) == 0 {
		return i18n.T(key)
	}
	return fmt.Sprintf(i18n.T(key), args...)
}

// ClockCheck asks systemd whether the clock is synchronized. Certificate
// issuance and validation compare timestamps, so a skewed clock breaks both.
func ClockCheck(env CheckEnv) Result {
	r := Result{Name: tr("PREFLIGHT_CLOCK")}
	out, err := env.Run("timedatectl", "show", "--property=NTPSynchronized", "--value")
	if err != nil {
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_CLOCK_UNKNOWN", err)
		return r
	}
	switch strings.TrimSpace(out) {
	case "yes":
		r.Detail = tr("PREFLIGHT_CLOCK_SYNCED")
	case "no":
		r.Status = Fail
		r.Detail = tr("PREFLIGHT_CLOCK_SKEW")
		r.Remedy = tr("PREFLIGHT_CLOCK_REMEDY")
	default:
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_CLOCK_UNKNOWN", strings.TrimSpace(out))
	}
	return r
}

var ssUsersRE = regexp.MustCompile(`users:\(\("([^"]+)"`)

// PortCheck binds the port locally. A port held by docker-proxy is most
// likely a stack of this tool and only warns; any other holder fails.
func PortCheck(env CheckEnv, port int) Result {
	r := Result{Name: tr("PREFLIGHT_PORT", port)}
	err := env.Listen(port)
	if err == nil {
		r.Detail = tr("PREFLIGHT_PORT_FREE")
		return r
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_PORT_UNKNOWN", err)
		return r
	}
	owner := ""
	if out, e := env.Run("ss", "-ltnpH", fmt.Sprintf("sport = :%d", port)); e == nil {
		if m := ssUsersRE.FindStringSubmatch(out); m != nil {
			owner = m[1]
		}
	}
	r.Status = Fail
	r.Detail = tr("PREFLIGHT_PORT_BUSY_UNKNOWN")
	if owner != "" {
		r.Detail = tr("PREFLIGHT_PORT_BUSY", owner)
	}
	if owner == "docker-proxy" {
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_PORT_BUSY_DOCKER")
	}
	r.Remedy = tr("PREFLIGHT_PORT_REMEDY", port)
	return r
}

// RenewalCheck verifies that something renews certificates: the certbot
// systemd timer or a crontab entry. A server without certificates has
// nothing to renew yet and passes.
func RenewalCheck(env CheckEnv) Result {
	r := Result{Name: tr("PREFLIGHT_RENEWAL")}
	certs, _ := env.Glob(letsencryptLiveGlob)
	if len(certs) == 0 {
		r.Detail = tr("PREFLIGHT_RENEWAL_NONE")
		return r
	}
	if out, err := env.Run("systemctl", "is-active", "certbot.timer"); err == nil && strings.TrimSpace(out) == "active" {
		r.Detail = tr("PREFLIGHT_RENEWAL_TIMER")
		return r
	}
	if out, err := env.Run("crontab", "-l"); err == nil && strings.Contains(out, "certbot renew") {
		r.Detail = tr("PREFLIGHT_RENEWAL_CRON")
		return r
	}
	r.Status = Warn
	r.Detail = tr("PREFLIGHT_RENEWAL_MISSING")
	r.Remedy = tr("PREFLIGHT_RENEWAL_REMEDY")
	return r
}

// CertExpiryResult turns the days left that certs.CheckCertExpiry reports
// into a table line. It takes the numbers instead of reading the file, so
// the expiry logic of the certs package stays the only one.
func CertExpiryResult(domain string, days int, err error) Result {
	r := Result{Name: tr("PREFLIGHT_EXPIRY", domain)}
	switch {
	case err != nil:
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_EXPIRY_ERR", err)
	case days <= 0:
		r.Status = Fail
		r.Detail = tr("PREFLIGHT_EXPIRY_EXPIRED", -days)
		r.Remedy = tr("PREFLIGHT_EXPIRY_REMEDY")
	case days <= 14:
		r.Status = Warn
		r.Detail = tr("PREFLIGHT_EXPIRY_DAYS", days)
		r.Remedy = tr("PREFLIGHT_EXPIRY_REMEDY")
	default:
		r.Detail = tr("PREFLIGHT_EXPIRY_DAYS", days)
	}
	return r
}

// CertbotDryRun asks the Let's Encrypt staging server for a certificate for
// domain over HTTP-01 without keeping it. Staging connects to port 80 of the
// domain from the internet, so a pass proves DNS, the firewall and the port
// together, which a local check cannot. Port 80 must be free for certbot's
// own listener.
func CertbotDryRun(env CheckEnv, domain string) Result {
	r := Result{Name: tr("PREFLIGHT_DRYRUN", domain)}
	out, err := env.Run("certbot", "certonly", "--dry-run", "--standalone", "--non-interactive",
		"--agree-tos", "--register-unsafely-without-email", "-d", domain)
	if err == nil {
		r.Detail = tr("PREFLIGHT_DRYRUN_OK")
		return r
	}
	r.Status, r.Detail, r.Remedy = classifyCertbotFailure(out, err)
	return r
}

// classifyCertbotFailure maps the output of a failed dry run to a line.
func classifyCertbotFailure(out string, err error) (Status, string, string) {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "could not bind tcp port 80"):
		return Fail, tr("PREFLIGHT_DRYRUN_BIND"), tr("PREFLIGHT_DRYRUN_BIND_R")
	case strings.Contains(low, "timeout during connect"):
		return Fail, tr("PREFLIGHT_DRYRUN_TIMEOUT"), tr("PREFLIGHT_DRYRUN_TIMEOUT_R")
	case strings.Contains(low, "connection refused"):
		return Fail, tr("PREFLIGHT_DRYRUN_REFUSED"), tr("PREFLIGHT_DRYRUN_TIMEOUT_R")
	case strings.Contains(low, "nxdomain") || strings.Contains(low, "dns problem"):
		return Fail, tr("PREFLIGHT_DRYRUN_DNS"), tr("PREFLIGHT_DRYRUN_DNS_R")
	case strings.Contains(low, "ratelimited") || strings.Contains(low, "too many"):
		return Warn, tr("PREFLIGHT_DRYRUN_RATE"), tr("PREFLIGHT_DRYRUN_RATE_R")
	case errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) ||
		strings.Contains(low, "command not found") || strings.Contains(strings.ToLower(err.Error()), "executable file not found"):
		return Warn, tr("PREFLIGHT_DRYRUN_NOCERTBOT"), tr("PREFLIGHT_DRYRUN_NOCERTBOT_R")
	}
	return Fail, tr("PREFLIGHT_DRYRUN_OTHER", lastLine(out, err)), tr("PREFLIGHT_DRYRUN_OTHER_R")
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return err.Error()
}

// Run runs every check that needs no domain: the clock, ports 80 and 443,
// and certificate renewal.
func Run(env CheckEnv) []Result {
	return []Result{ClockCheck(env), PortCheck(env, 80), PortCheck(env, 443), RenewalCheck(env)}
}

// Failures returns the results with status Fail.
func Failures(rs []Result) []Result {
	var out []Result
	for _, r := range rs {
		if r.Status == Fail {
			out = append(out, r)
		}
	}
	return out
}

func label(s Status) (string, string) {
	switch s {
	case OK:
		return tr("PREFLIGHT_OK"), ui.ColorGreen
	case Warn:
		return tr("PREFLIGHT_WARN"), ui.ColorYellow
	}
	return tr("PREFLIGHT_FAIL"), ui.ColorRed
}

// Print writes the table with a remedy under every line that has one and
// returns the number of failures.
func Print(rs []Result) int {
	fmt.Println()
	fmt.Printf("%s%s%s\n", ui.ColorGreen, tr("PREFLIGHT_TITLE"), ui.ColorReset)
	width := 0
	for _, r := range rs {
		if n := len([]rune(r.Name)); n > width {
			width = n
		}
	}
	for _, r := range rs {
		text, color := label(r.Status)
		pad := strings.Repeat(" ", width-len([]rune(r.Name)))
		fmt.Printf("%s%s%s  %s%-8s%s %s\n", ui.ColorGray, r.Name+pad, ui.ColorReset, color, text, ui.ColorReset, r.Detail)
		if r.Remedy != "" {
			fmt.Printf("%s  -> %s%s\n", ui.ColorYellow, r.Remedy, ui.ColorReset)
		}
	}
	fmt.Printf("%s%s%s\n\n", ui.ColorGray, tr("PREFLIGHT_LIMIT"), ui.ColorReset)
	return len(Failures(rs))
}

// Confirm prints the table and, when something failed, asks whether to go
// on. Nothing but an explicit yes continues.
func Confirm(rs []Result, ask func(string) string) bool {
	if Print(rs) == 0 {
		return true
	}
	a := strings.ToLower(strings.TrimSpace(ask(tr("PREFLIGHT_CONTINUE"))))
	return a == "y" || a == "yes"
}

// Gate runs the checks that need no domain, prints one table and asks
// whether to go on when one fails. Install flows call it once, before the
// first question.
func Gate(env CheckEnv, ask func(string) string) bool {
	return Confirm(Run(env), ask)
}

// ConfirmDryRun runs the certbot dry run for each domain and asks whether to
// go on when one fails.
func ConfirmDryRun(env CheckEnv, domains []string, ask func(string) string) bool {
	var rs []Result
	seen := map[string]bool{}
	for _, d := range domains {
		if !seen[d] {
			seen[d] = true
			rs = append(rs, CertbotDryRun(env, d))
		}
	}
	return Confirm(rs, ask)
}
