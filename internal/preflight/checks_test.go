package preflight

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
)

// fakeHost scripts every command by its full command line.
type fakeHost struct {
	out    map[string]string // command line -> combined output
	fail   map[string]error  // command line -> error
	listen map[int]error     // port -> bind error
	globs  map[string][]string
	calls  []string
}

func newFakeHost() *fakeHost {
	return &fakeHost{out: map[string]string{}, fail: map[string]error{}, listen: map[int]error{}, globs: map[string][]string{}}
}

func (h *fakeHost) env() CheckEnv {
	return CheckEnv{
		Run: func(name string, args ...string) (string, error) {
			line := name + " " + strings.Join(args, " ")
			h.calls = append(h.calls, line)
			if err, ok := h.fail[line]; ok {
				return h.out[line], err
			}
			if o, ok := h.out[line]; ok {
				return o, nil
			}
			return "", errors.New("unscripted command: " + line)
		},
		Listen: func(port int) error { return h.listen[port] },
		Glob:   func(p string) ([]string, error) { return h.globs[p], nil },
	}
}

const timedatectl = "timedatectl show --property=NTPSynchronized --value"

func TestClockCheck(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		err    error
		status Status
		detail string
	}{
		{"synchronized", "yes\n", nil, OK, "synchronized"},
		{"skewed clock", "no\n", nil, Fail, "not synchronized"},
		{"no timedatectl", "", errors.New("executable file not found"), Warn, "cannot be read"},
		{"odd answer", "maybe", nil, Warn, "cannot be read"},
		{"empty answer", "", nil, Warn, "cannot be read"},
	}
	for _, c := range cases {
		h := newFakeHost()
		h.out[timedatectl] = c.out
		if c.err != nil {
			h.fail[timedatectl] = c.err
		}
		r := ClockCheck(h.env())
		if r.Status != c.status || !strings.Contains(r.Detail, c.detail) {
			t.Errorf("%s: status %v detail %q", c.name, r.Status, r.Detail)
		}
		if c.status == Fail && !strings.Contains(r.Remedy, "timedatectl set-ntp true") {
			t.Errorf("%s: remedy %q", c.name, r.Remedy)
		}
		if c.status == OK && r.Remedy != "" {
			t.Errorf("%s: a passing check carries a remedy", c.name)
		}
	}
}

func TestPortCheckFree(t *testing.T) {
	r := PortCheck(newFakeHost().env(), 80)
	if r.Status != OK || r.Name != "Port 80" || r.Detail != "free" {
		t.Errorf("%+v", r)
	}
}

func TestPortCheckBusyNamesTheOwner(t *testing.T) {
	h := newFakeHost()
	h.listen[80] = fmt.Errorf("listen tcp :80: %w", syscall.EADDRINUSE)
	h.out["ss -ltnpH sport = :80"] = `LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=812,fd=6),("nginx",pid=813,fd=6))` + "\n"
	r := PortCheck(h.env(), 80)
	if r.Status != Fail || !strings.Contains(r.Detail, "nginx") || !strings.Contains(r.Remedy, "port 80") {
		t.Errorf("%+v", r)
	}
}

func TestPortCheckBusyWithoutOwnerInfo(t *testing.T) {
	h := newFakeHost()
	h.listen[443] = syscall.EADDRINUSE // no ss output scripted
	r := PortCheck(h.env(), 443)
	if r.Status != Fail || r.Detail != "in use" {
		t.Errorf("%+v", r)
	}
}

func TestPortCheckDockerProxyOnlyWarns(t *testing.T) {
	h := newFakeHost()
	h.listen[443] = syscall.EADDRINUSE
	h.out["ss -ltnpH sport = :443"] = `LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("docker-proxy",pid=99,fd=4))`
	r := PortCheck(h.env(), 443)
	if r.Status != Warn || !strings.Contains(r.Detail, "docker-proxy") {
		t.Errorf("%+v", r)
	}
}

func TestPortCheckOtherErrorsAreNotJudged(t *testing.T) {
	h := newFakeHost()
	h.listen[80] = syscall.EACCES
	r := PortCheck(h.env(), 80)
	if r.Status != Warn || !strings.Contains(r.Detail, "cannot be tested") {
		t.Errorf("%+v", r)
	}
}

func TestRenewalCheck(t *testing.T) {
	t.Run("no certificates yet", func(t *testing.T) {
		r := RenewalCheck(newFakeHost().env())
		if r.Status != OK || !strings.Contains(r.Detail, "no certificates") {
			t.Errorf("%+v", r)
		}
	})
	live := func(h *fakeHost) {
		h.globs[letsencryptLiveGlob] = []string{"/etc/letsencrypt/live/example.com/fullchain.pem"}
	}
	t.Run("systemd timer", func(t *testing.T) {
		h := newFakeHost()
		live(h)
		h.out["systemctl is-active certbot.timer"] = "active\n"
		if r := RenewalCheck(h.env()); r.Status != OK || !strings.Contains(r.Detail, "certbot.timer") {
			t.Errorf("%+v", r)
		}
	})
	t.Run("cron entry", func(t *testing.T) {
		h := newFakeHost()
		live(h)
		h.out["systemctl is-active certbot.timer"] = "inactive\n"
		h.fail["systemctl is-active certbot.timer"] = errors.New("exit 3")
		h.out["crontab -l"] = "0 5 * * 1 /usr/bin/certbot renew --quiet\n"
		if r := RenewalCheck(h.env()); r.Status != OK || !strings.Contains(r.Detail, "cron") {
			t.Errorf("%+v", r)
		}
	})
	t.Run("nothing renews", func(t *testing.T) {
		h := newFakeHost()
		live(h)
		h.fail["systemctl is-active certbot.timer"] = errors.New("exit 3")
		h.out["crontab -l"] = "# nothing\n30 2 * * * /usr/bin/backup\n"
		r := RenewalCheck(h.env())
		if r.Status != Warn || r.Remedy == "" {
			t.Errorf("%+v", r)
		}
	})
	t.Run("a timer that is not active does not count", func(t *testing.T) {
		h := newFakeHost()
		live(h)
		h.out["systemctl is-active certbot.timer"] = "inactive\n"
		h.fail["crontab -l"] = errors.New("no crontab for root")
		if r := RenewalCheck(h.env()); r.Status != Warn {
			t.Errorf("%+v", r)
		}
	})
}

func TestCertExpiryResult(t *testing.T) {
	cases := []struct {
		days   int
		err    error
		status Status
		detail string
	}{
		{60, nil, OK, "60 days left"},
		{15, nil, OK, "15 days left"},
		{14, nil, Warn, "14 days left"},
		{1, nil, Warn, "1 days left"},
		{0, nil, Fail, "expired 0 days ago"},
		{-5, nil, Fail, "expired 5 days ago"},
		{0, errors.New("no cert dir"), Warn, "cannot be read"},
	}
	for _, c := range cases {
		r := CertExpiryResult("example.com", c.days, c.err)
		if r.Status != c.status || !strings.Contains(r.Detail, c.detail) || r.Name != "Certificate example.com" {
			t.Errorf("days=%d err=%v: %+v", c.days, c.err, r)
		}
		if (c.status != OK) != (r.Remedy != "") && c.err == nil {
			t.Errorf("days=%d: remedy %q for status %v", c.days, r.Remedy, r.Status)
		}
	}
}

const dryRunCmd = "certbot certonly --dry-run --standalone --non-interactive --agree-tos --register-unsafely-without-email -d example.com"

func TestCertbotDryRunPass(t *testing.T) {
	h := newFakeHost()
	h.out[dryRunCmd] = "The dry run was successful."
	r := CertbotDryRun(h.env(), "example.com")
	if r.Status != OK || r.Name != "Certificate test for example.com" {
		t.Errorf("%+v", r)
	}
}

func TestCertbotDryRunFailures(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		err    error
		status Status
		detail string
	}{
		{"firewall", "Detail: 203.0.113.1: Fetching http://example.com/.well-known/acme-challenge/x: Timeout during connect (likely firewall problem)", errors.New("exit 1"), Fail, "not reachable from the internet"},
		{"refused", "Connection refused", errors.New("exit 1"), Fail, "refuses the connection"},
		{"dns", "DNS problem: NXDOMAIN looking up A for example.com", errors.New("exit 1"), Fail, "cannot resolve"},
		{"nxdomain only", "something NXDOMAIN something", errors.New("exit 1"), Fail, "cannot resolve"},
		{"port 80 taken", "Problem binding to port 80: Could not bind TCP port 80 because it is already in use", errors.New("exit 1"), Fail, "could not use port 80"},
		{"rate limit", "too many failed authorizations (5) for \"example.com\"", errors.New("exit 1"), Warn, "rate limit"},
		{"rate limited code", "urn:ietf:params:acme:error:rateLimited", errors.New("exit 1"), Warn, "rate limit"},
		{"certbot missing", "", errors.New(`exec: "certbot": executable file not found in $PATH`), Warn, "not installed"},
		{"unknown", "Some unexpected failure\nlast useful line here\n\n", errors.New("exit 1"), Fail, "last useful line here"},
		{"unknown without output", "", errors.New("exit status 1"), Fail, "exit status 1"},
	}
	for _, c := range cases {
		h := newFakeHost()
		h.out[dryRunCmd] = c.out
		h.fail[dryRunCmd] = c.err
		r := CertbotDryRun(h.env(), "example.com")
		if r.Status != c.status || !strings.Contains(r.Detail, c.detail) {
			t.Errorf("%s: status %v detail %q", c.name, r.Status, r.Detail)
		}
		if r.Remedy == "" {
			t.Errorf("%s: no remedy", c.name)
		}
	}
}

func TestRunCoversTheDomainFreeChecks(t *testing.T) {
	h := newFakeHost()
	h.out[timedatectl] = "yes\n"
	rs := Run(h.env())
	if len(rs) != 4 {
		t.Fatalf("got %d results: %+v", len(rs), rs)
	}
	names := []string{rs[0].Name, rs[1].Name, rs[2].Name, rs[3].Name}
	want := []string{"System clock", "Port 80", "Port 443", "Certificate renewal"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("result %d is %q, want %q", i, names[i], want[i])
		}
	}
	if f := Failures(rs); len(f) != 0 {
		t.Errorf("failures on a clean host: %+v", f)
	}
}

func TestGateAsksOnlyAfterAFailure(t *testing.T) {
	never := func(string) string { t.Fatal("asked although every check passed"); return "" }
	h := newFakeHost()
	h.out[timedatectl] = "yes\n"
	if !Gate(h.env(), never) {
		t.Error("a clean host was stopped")
	}

	// A skewed clock fails the gate unless the operator accepts.
	h.out[timedatectl] = "no\n"
	for answer, want := range map[string]bool{"y": true, "Y": true, "yes": true, " y ": true, "n": false, "": false, "no": false, "sure": false} {
		asked := 0
		got := Gate(h.env(), func(string) string { asked++; return answer })
		if got != want || asked != 1 {
			t.Errorf("answer %q: got %v after %d questions, want %v", answer, got, asked, want)
		}
	}
}

func TestGatePortConflictAndSkewTogether(t *testing.T) {
	h := newFakeHost()
	h.out[timedatectl] = "no\n"
	h.listen[80] = syscall.EADDRINUSE
	rs := Run(h.env())
	if f := Failures(rs); len(f) != 2 {
		t.Fatalf("failures = %+v", f)
	}
	asked := 0
	Confirm(rs, func(string) string { asked++; return "n" })
	if asked != 1 {
		t.Errorf("asked %d times for one table", asked)
	}
}

func TestConfirmDryRunChecksEachDomainOnce(t *testing.T) {
	h := newFakeHost()
	for _, d := range []string{"a.example.com", "b.example.com"} {
		h.out[strings.Replace(dryRunCmd, "example.com", d, 1)] = "The dry run was successful."
	}
	never := func(string) string { t.Fatal("asked although every dry run passed"); return "" }
	if !ConfirmDryRun(h.env(), []string{"a.example.com", "b.example.com", "a.example.com"}, never) {
		t.Error("passing dry runs stopped the flow")
	}
	if len(h.calls) != 2 {
		t.Errorf("certbot ran %d times: %v", len(h.calls), h.calls)
	}
}

func TestConfirmDryRunFailureAsks(t *testing.T) {
	h := newFakeHost()
	cmd := strings.Replace(dryRunCmd, "example.com", "a.example.com", 1)
	h.out[cmd] = "Timeout during connect"
	h.fail[cmd] = errors.New("exit 1")
	if ConfirmDryRun(h.env(), []string{"a.example.com"}, func(string) string { return "n" }) {
		t.Error("a failed dry run was accepted without confirmation")
	}
	if !ConfirmDryRun(h.env(), []string{"a.example.com"}, func(string) string { return "y" }) {
		t.Error("an operator override was ignored")
	}
}

func TestPrintCountsFailuresAndShowsRemedies(t *testing.T) {
	rs := []Result{
		{Name: "System clock", Status: OK, Detail: "synchronized"},
		{Name: "Port 80", Status: Fail, Detail: "in use by nginx", Remedy: "Stop it."},
		{Name: "Certificate renewal", Status: Warn, Detail: "missing", Remedy: "Add cron."},
	}
	if n := Print(rs); n != 1 {
		t.Errorf("failures = %d", n)
	}
	if got := Failures(rs); len(got) != 1 || got[0].Name != "Port 80" {
		t.Errorf("Failures = %+v", got)
	}
}

func TestEveryPreflightKeyExistsInBothLanguages(t *testing.T) {
	// A key used by the checks but missing in a language would print the key
	// itself. The i18n tests cover parity; this covers the checks reading
	// real text for every outcome.
	for _, r := range []Result{
		ClockCheck(CheckEnv{Run: func(string, ...string) (string, error) { return "no", nil }}),
		CertExpiryResult("x.com", -1, nil),
	} {
		if strings.Contains(r.Detail, "PREFLIGHT_") || strings.Contains(r.Remedy, "PREFLIGHT_") || strings.Contains(r.Name, "PREFLIGHT_") {
			t.Errorf("untranslated text: %+v", r)
		}
	}
}
