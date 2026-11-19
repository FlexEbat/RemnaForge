package certs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	plainCmd = "/usr/bin/certbot renew --quiet"
	weekly   = "0 5 * * 0"
)

func TestPlanCronAddsTheJobToAnEmptyCrontab(t *testing.T) {
	got, action := planCron("", plainCmd, 9999, true)
	if action != cronAdd || got != weekly+" "+plainCmd+"\n" {
		t.Errorf("got %q, %v", got, action)
	}
}

func TestPlanCronKeepsOtherJobs(t *testing.T) {
	existing := "MAILTO=root\n*/5 * * * * /usr/local/bin/backup\n"
	got, action := planCron(existing, plainCmd, 9999, true)
	if action != cronAdd || !strings.HasPrefix(got, existing) || !strings.HasSuffix(got, weekly+" "+plainCmd+"\n") {
		t.Errorf("got %q, %v", got, action)
	}
}

// A commented-out job is not a job.
func TestPlanCronIgnoresCommentedRenewLines(t *testing.T) {
	existing := "# " + weekly + " " + plainCmd + "\n"
	_, action := planCron(existing, plainCmd, 9999, false)
	if action != cronAdd {
		t.Errorf("action = %v, want add", action)
	}
}

func TestPlanCronKeepsAnExistingJobWhenCertificatesWereNotIssued(t *testing.T) {
	existing := "0 3 * * 1 " + renewCronCommand("2") + "\n"
	got, action := planCron(existing, plainCmd, 9999, false)
	if action != cronKeep || got != existing {
		t.Errorf("an HTTP-01 job was replaced by the plain one: %q, %v", got, action)
	}
}

func TestPlanCronMakesTheScheduleWeeklyWhenACertificateExpiresSoon(t *testing.T) {
	existing := "0 3 1 * * " + renewCronCommand("2") + "\n"
	got, action := planCron(existing, plainCmd, 12, false)
	want := weekly + " " + renewCronCommand("2") + "\n"
	if action != cronReplace || got != want {
		t.Errorf("got %q, %v; want %q", got, action, want)
	}
}

func TestPlanCronReplacesAJobOfAnotherMethod(t *testing.T) {
	old := "0 5 * * 0 ufw allow 80 && /usr/bin/certbot renew --quiet && ufw delete allow 80\n"
	got, action := planCron("MAILTO=root\n"+old, renewCronCommand("2"), 90, true)
	if action != cronReplace || strings.Contains(got, "ufw allow 80 &&") {
		t.Errorf("the old job stayed: %q, %v", got, action)
	}
	if !strings.Contains(got, "MAILTO=root\n") || !strings.Contains(got, "--pre-hook") {
		t.Errorf("got %q", got)
	}
}

func TestPlanCronKeepsAMatchingJob(t *testing.T) {
	existing := weekly + " " + plainCmd + "\n"
	got, action := planCron(existing, plainCmd, 90, true)
	if action != cronKeep || got != existing {
		t.Errorf("got %q, %v", got, action)
	}
}

func TestRenewCronCommandHasNoComposeOrFirewallChain(t *testing.T) {
	for _, m := range []string{"1", "2", "3"} {
		c := renewCronCommand(m)
		for _, bad := range []string{"docker compose", "ufw ", "&&"} {
			if strings.Contains(c, bad) {
				t.Errorf("method %s: command %q contains %q", m, c, bad)
			}
		}
	}
	if !strings.Contains(renewCronCommand("2"), "--pre-hook") {
		t.Error("the HTTP-01 job has no pre-hook")
	}
}

func TestSyncCertRenewalCronDoesNotOverwriteWhenTheCrontabCannotBeRead(t *testing.T) {
	oldRead, oldWrite := readCrontab, writeCrontab
	defer func() { readCrontab, writeCrontab = oldRead, oldWrite }()
	readCrontab = func() (string, error) { return "", os.ErrPermission }
	wrote := false
	writeCrontab = func(string) error { wrote = true; return nil }
	if err := syncCertRenewalCron(plainCmd, 90, true); err == nil {
		t.Error("no error for an unreadable crontab")
	}
	if wrote {
		t.Error("the crontab was written although it could not be read")
	}
}

func TestSyncCertRenewalCronWritesTheJob(t *testing.T) {
	oldRead, oldWrite := readCrontab, writeCrontab
	defer func() { readCrontab, writeCrontab = oldRead, oldWrite }()
	readCrontab = func() (string, error) { return "MAILTO=root\n", nil }
	var written string
	writeCrontab = func(s string) error { written = s; return nil }
	if err := syncCertRenewalCron(plainCmd, 90, true); err != nil {
		t.Fatal(err)
	}
	if written != "MAILTO=root\n"+weekly+" "+plainCmd+"\n" {
		t.Errorf("written %q", written)
	}
}

// runHook runs the port 80 script with a fake ufw in front of PATH and
// returns what ufw was asked to do.
func runHook(t *testing.T, arg, ufwStatus, marker string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "ufw.log")
	fake := "#!/bin/sh\nif [ \"$1\" = status ]; then printf '%s\\n' \"$UFW_STATUS\"; exit 0; fi\necho \"$@\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ufw"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte(http01HookScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script, arg)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "UFW_STATUS="+ufwStatus, "REMNAFORGE_PORT80_MARKER="+marker)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook failed: %v: %s", err, out)
	}
	b, _ := os.ReadFile(log)
	return string(b)
}

func TestHTTP01HookClosesOnlyWhatItOpened(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	closed := "Status: active\n\nTo   Action  From\n22/tcp  ALLOW  Anywhere"
	if got := runHook(t, "open", closed, marker); !strings.Contains(got, "allow 80/tcp") {
		t.Fatalf("port 80 was not opened: %q", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("no marker after opening")
	}
	if got := runHook(t, "close", closed, marker); !strings.Contains(got, "delete allow 80/tcp") {
		t.Errorf("the rule it opened was not closed: %q", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the marker stayed after closing")
	}
}

func TestHTTP01HookLeavesAnExistingRuleAlone(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	open80 := "Status: active\n\nTo   Action  From\n80/tcp  ALLOW  Anywhere\n443/tcp  ALLOW  Anywhere"
	if got := runHook(t, "open", open80, marker); got != "" {
		t.Errorf("ufw changed although port 80 was open: %q", got)
	}
	if got := runHook(t, "close", open80, marker); got != "" {
		t.Errorf("the rule of the firewall plan was deleted: %q", got)
	}
}

func TestHTTP01HookDoesNothingWithAnInactiveUfw(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	if got := runHook(t, "open", "Status: inactive", marker); got != "" {
		t.Errorf("ufw was touched while inactive: %q", got)
	}
}

func TestCloudflareCredentialKind(t *testing.T) {
	cases := map[string]bool{
		"0123456789abcdef0123456789abcdef01234":  false, // 37 hex digits: Global API Key
		"abcdefghijklmnopqrstuvwxyz_-0123456789": true,  // a token without an uppercase letter
		"Abc123_tokenWithUpper-and-more-chars00": true,
	}
	for key, token := range cases {
		if got := isCloudflareToken(key); got != token {
			t.Errorf("isCloudflareToken(%q) = %v, want %v", key, got, token)
		}
	}
}

// The flows compared the method with "1" alone: a Gcore wildcard certificate
// (method 3) is issued under the base domain as well, and the proxy
// configuration pointed at a directory that does not exist.
func TestResultCertNameFollowsTheMethod(t *testing.T) {
	const sub, base = "panel.example.com", "example.com"
	cases := []struct {
		name string
		r    Result
		want string
	}{
		{"Cloudflare wildcard", Result{Method: "1"}, base},
		{"Gcore wildcard", Result{Method: "3"}, base},
		{"HTTP-01 per domain", Result{Method: "2"}, sub},
	}
	for _, c := range cases {
		if got := c.r.CertName(sub); got != c.want {
			t.Errorf("%s: CertName = %q, want %q", c.name, got, c.want)
		}
	}
}

// With certificates already in place the answer comes from the disk. A host
// without a wildcard certificate under the base domain gets the domain itself.
func TestResultCertNameForExistingCertificatesWithoutAWildcard(t *testing.T) {
	r := Result{Method: "1", Skipped: true}
	if got := r.CertName("panel.example.com"); got != "panel.example.com" {
		t.Errorf("CertName = %q, want the domain itself", got)
	}
}
