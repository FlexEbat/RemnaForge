package certs

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/FlexEbat/RemnaForge/internal/cfgfile"
	"github.com/FlexEbat/RemnaForge/internal/execx"
	"github.com/FlexEbat/RemnaForge/internal/fsx"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/journal"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// HTTP01HookPath is the script that opens port 80 for an HTTP-01 challenge
// and closes it again.
const HTTP01HookPath = "/usr/local/remnawave_reverse/http01-port80.sh"

// renewSchedule is the weekly slot of the renewal job: Sunday, 05:00.
const renewSchedule = "0 5 * * 0"

// renewMarker is what finds the renewal job in the crontab.
const renewMarker = "/usr/bin/certbot renew"

// http01HookScript opens port 80 in ufw for the challenge and closes it
// afterwards. It closes only a rule it opened itself: port 80 is a base
// port of the firewall plan and the nginx redirect listens on it, so a rule
// that was there before must stay. It does nothing without an active ufw,
// because the other firewalls this tool configures keep port 80 open.
const http01HookScript = `#!/bin/sh
# Opens port 80 for the certbot HTTP-01 challenge and closes it again.
marker="${REMNAFORGE_PORT80_MARKER:-/run/remnaforge-port80}"
case "$1" in
open)
    command -v ufw >/dev/null 2>&1 || exit 0
    ufw status | grep -q '^Status: active' || exit 0
    if ufw status | grep -Eq '^80(/tcp)?[[:space:]]+ALLOW'; then
        exit 0
    fi
    ufw allow 80/tcp >/dev/null 2>&1 && : > "$marker"
    ;;
close)
    [ -f "$marker" ] || exit 0
    ufw delete allow 80/tcp >/dev/null 2>&1
    rm -f "$marker"
    ;;
esac
exit 0
`

// InstallHTTP01Hook writes the port 80 script, executable by root only.
func InstallHTTP01Hook() error {
	if err := fsx.MkdirAll(filepath.Dir(HTTP01HookPath), 0o755); err != nil {
		return err
	}
	return cfgfile.Replace(HTTP01HookPath, http01HookScript, cfgfile.Opts{Mode: 0o700})
}

// renewCronCommand is the command of the renewal job. The HTTP-01 method
// opens port 80 only when a certificate is due, through certbot's own
// hooks, and the renew_hook of each certificate restarts the proxy. The
// job used to chain ufw and docker compose commands: a failed renewal left
// port 80 open, "docker compose up" without -d never returned, and the
// panel went down every week whether or not a certificate was renewed.
func renewCronCommand(method string) string {
	if method != "2" {
		return renewMarker + " --quiet"
	}
	return fmt.Sprintf("%s --quiet --pre-hook '%s open' --post-hook '%s close'", renewMarker, HTTP01HookPath, HTTP01HookPath)
}

// openPort80 opens port 80 for a challenge and returns the function that
// closes it. It closes only what it opened.
func openPort80() (closeFn func(), err error) {
	if err := InstallHTTP01Hook(); err != nil {
		return func() {}, err
	}
	_ = execx.Command(HTTP01HookPath, "open").Run()
	return func() { _ = execx.Command(HTTP01HookPath, "close").Run() }, nil
}

var (
	// readCrontab returns root's crontab. A missing crontab is an empty one;
	// any other failure is an error, because writing back an empty string
	// would replace every job root has.
	readCrontab = func() (string, error) {
		out, err := execx.Command("crontab", "-u", "root", "-l").CombinedOutput()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && strings.Contains(strings.ToLower(string(out)), "no crontab") {
				return "", nil
			}
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	writeCrontab = func(content string) error {
		cmd := execx.Command("crontab", "-u", "root", "-")
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

type cronAction int

const (
	cronKeep cronAction = iota
	cronAdd
	cronReplace
)

// renewLine is one active line of the crontab that runs certbot renew.
type renewLine struct {
	index    int
	schedule string
	command  string
}

func parseRenewLines(lines []string) []renewLine {
	var out []renewLine
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || !strings.Contains(t, renewMarker) {
			continue
		}
		f := strings.Fields(t)
		switch {
		case strings.HasPrefix(f[0], "@") && len(f) >= 2:
			out = append(out, renewLine{i, f[0], strings.Join(f[1:], " ")})
		case len(f) >= 6:
			out = append(out, renewLine{i, strings.Join(f[:5], " "), strings.Join(f[5:], " ")})
		}
	}
	return out
}

func sameCommand(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// planCron decides what the renewal job in the crontab needs and returns the
// new crontab. When certificates were just issued (replaceCommand), the job
// takes the command of their method. Otherwise an existing job stays as it
// is: the operator's certificates may use another method than this run, and
// a job without a renew line is added with the plain command.
func planCron(existing, command string, minDaysLeft int, replaceCommand bool) (string, cronAction) {
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")
	if strings.TrimSpace(existing) == "" {
		lines = nil
	}
	renew := parseRenewLines(lines)
	rule := renewSchedule + " " + command

	build := func(drop map[int]bool, rewrite map[int]string, add bool) string {
		var out []string
		for i, l := range lines {
			if drop[i] {
				continue
			}
			if r, ok := rewrite[i]; ok {
				l = r
			}
			out = append(out, l)
		}
		if add {
			out = append(out, rule)
		}
		return strings.Join(out, "\n") + "\n"
	}

	if len(renew) == 0 {
		return build(nil, nil, true), cronAdd
	}

	if !replaceCommand {
		weekly := false
		for _, r := range renew {
			if r.schedule == renewSchedule {
				weekly = true
			}
		}
		if minDaysLeft <= 30 && !weekly {
			first := renew[0]
			return build(nil, map[int]string{first.index: renewSchedule + " " + first.command}, false), cronReplace
		}
		return existing, cronKeep
	}

	matching, other := 0, 0
	for _, r := range renew {
		if sameCommand(r.command, command) {
			matching++
		} else {
			other++
		}
	}
	if matching > 0 && other == 0 {
		weekly := false
		for _, r := range renew {
			if r.schedule == renewSchedule {
				weekly = true
			}
		}
		if weekly || minDaysLeft > 30 {
			return existing, cronKeep
		}
	}
	drop := map[int]bool{}
	for _, r := range renew {
		drop[r.index] = true
	}
	return build(drop, nil, true), cronReplace
}

// syncCertRenewalCron makes sure root's crontab has the weekly renewal job.
// It reads the crontab before it writes one, and stops when it cannot read
// it.
func syncCertRenewalCron(command string, minDaysLeft int, replaceCommand bool) error {
	journal.Add("cron", i18n.T("JOURNAL_CRON"))
	existing, err := readCrontab()
	if err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CRON_READ_FAILED"), err), ui.ColorReset)
		return err
	}
	updated, action := planCron(existing, command, minDaysLeft, replaceCommand)
	switch action {
	case cronKeep:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CRON_ALREADY_EXISTS"), ui.ColorReset)
		return nil
	case cronAdd:
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("ADDING_CRON_FOR_EXISTING_CERTS"), ui.ColorReset)
	case cronReplace:
		if minDaysLeft <= 30 {
			fmt.Printf("%s%s %d %s%s\n", ui.ColorYellow, i18n.T("CERT_EXPIRY_SOON"), minDaysLeft, i18n.T("DAYS"), ui.ColorReset)
		}
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("UPDATING_CRON"), ui.ColorReset)
	}
	if err := writeCrontab(updated); err != nil {
		fmt.Printf("%s%s%s\n", ui.ColorRed, fmt.Sprintf(i18n.T("CRON_WRITE_FAILED"), err), ui.ColorReset)
		return err
	}
	return nil
}
