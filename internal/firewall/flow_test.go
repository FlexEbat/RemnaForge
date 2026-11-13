package firewall

import (
	"errors"
	"strings"
	"testing"
)

// script answers prompts in order and fails the test when asked too much.
type script struct {
	t       *testing.T
	answers []string
	prompts []string
}

func (s *script) ask(prompt string) string {
	s.prompts = append(s.prompts, prompt)
	if len(s.answers) == 0 {
		s.t.Fatalf("asked more than scripted: %q", prompt)
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a
}

func quiet(t *testing.T) {
	old := out
	out = func(string, string) {}
	t.Cleanup(func() { out = old })
}

func enableCommands(execs []string) []string {
	var r []string
	for _, e := range execs {
		if strings.Contains(e, "enable") {
			r = append(r, e)
		}
	}
	return r
}

func TestRunConfiguresUFWAfterConsent(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	h.out["ufw status verbose"] = ufwInactive
	h.ssh = []int{2200}
	// start, choose ufw, extra ports, consent, no intrusion tool
	s := &script{t: t, answers: []string{"y", "1", "8443", "y", "0"}}
	Run(h.env(), s.ask)
	want := []string{"ufw allow 2200/tcp", "ufw allow 80/tcp", "ufw allow 443/tcp", "ufw allow 8443/tcp", "ufw --force enable"}
	if strings.Join(h.execs, "|") != strings.Join(want, "|") {
		t.Errorf("execs = %v", h.execs)
	}
	grantedAt, enableAt := -1, -1
	for i, l := range h.timeline {
		if strings.Contains(l, "granted: configure ufw") {
			grantedAt = i
		}
		if l == "exec ufw --force enable" {
			enableAt = i
		}
	}
	if grantedAt < 0 || enableAt < grantedAt {
		t.Errorf("enable without a recorded consent before it: %v", h.timeline)
	}
}

func TestRunDeclinedConsentEnablesNothing(t *testing.T) {
	quiet(t)
	for _, kind := range []string{"1", "2", "3", "4"} {
		h := fullServer(t)
		h.out["ufw status verbose"] = ufwInactive
		s := &script{t: t, answers: []string{"y", kind, "", "n", "0"}}
		Run(h.env(), s.ask)
		if len(h.execs) != 0 || len(h.files) != 0 {
			t.Errorf("tool %s: changes after a refusal: %v %v", kind, h.execs, h.files)
		}
		if len(h.consent) != 1 || !strings.Contains(h.consent[0], "declined") {
			t.Errorf("tool %s: the refusal was not recorded: %v", kind, h.consent)
		}
	}
}

func TestRunStartDeclinedAsksNoFurtherAndChangesNothing(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	s := &script{t: t, answers: []string{"n", "0"}}
	Run(h.env(), s.ask)
	if len(h.execs) != 0 {
		t.Errorf("execs = %v", h.execs)
	}
}

func TestRunSkipChoice(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	s := &script{t: t, answers: []string{"y", "0", "0"}}
	Run(h.env(), s.ask)
	if len(h.execs) != 0 {
		t.Errorf("execs = %v", h.execs)
	}
}

func TestRunEveryToolEnablesOnlyAfterConsent(t *testing.T) {
	quiet(t)
	for i, kind := range Kinds {
		h := newHost(t)
		for _, n := range []string{"ufw", "firewall-cmd", "nft", "iptables"} {
			h.tools[n] = true
		}
		h.out["ufw status verbose"] = ufwInactive
		h.out["firewall-cmd --state"] = "not running\n"
		h.fail["firewall-cmd --state"] = errors.New("exit 252")
		h.out["nft list ruleset"] = ""
		h.out["iptables --version"] = iptablesVersionNft
		h.out["iptables -S INPUT"] = iptablesInputOpen
		h.out["iptables -t nat -S"] = natNone
		h.out["ss -ltnpH"] = ssOutput
		s := &script{t: t, answers: []string{"y", string(rune('1' + i)), "", "y", "0"}}
		Run(h.env(), s.ask)
		consentAt, enableAt := -1, -1
		for j, l := range h.timeline {
			if strings.HasPrefix(l, "consent ") && strings.Contains(l, "granted") && consentAt < 0 {
				consentAt = j
			}
			if strings.Contains(l, "exec") && strings.Contains(l, "enable") && enableAt < 0 {
				enableAt = j
			}
		}
		if enableAt < 0 {
			t.Errorf("%s: nothing was enabled: %v", kind, h.timeline)
			continue
		}
		if consentAt < 0 || consentAt > enableAt {
			t.Errorf("%s: enable at %d, consent at %d: %v", kind, enableAt, consentAt, h.timeline)
		}
	}
}

func TestRunInstallsAMissingToolOnlyAfterItsOwnConsent(t *testing.T) {
	quiet(t)
	h := newHost(t)
	h.out["ss -ltnpH"] = ssOutput
	// start, choose ufw (not installed), consent to install, no extra ports, consent to enable
	s := &script{t: t, answers: []string{"y", "1", "y", "", "y", "0"}}
	Run(h.env(), s.ask)
	if len(h.execs) < 2 || h.execs[0] != "apt-get install -y ufw" {
		t.Fatalf("execs = %v", h.execs)
	}
	if len(h.consent) != 2 || !strings.Contains(h.consent[0], "install package ufw") {
		t.Errorf("consents = %v", h.consent)
	}

	h2 := newHost(t)
	h2.out["ss -ltnpH"] = ssOutput
	s2 := &script{t: t, answers: []string{"y", "1", "n", "0"}}
	Run(h2.env(), s2.ask)
	if len(h2.execs) != 0 {
		t.Errorf("a declined install ran: %v", h2.execs)
	}
}

func TestRunAsksAgainOnBadPortList(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	h.out["ufw status verbose"] = ufwInactive
	s := &script{t: t, answers: []string{"y", "1", "80;443", "99999", "9000", "y", "0"}}
	Run(h.env(), s.ask)
	if indexOf(h.execs, "ufw allow 9000/tcp") < 0 {
		t.Errorf("execs = %v", h.execs)
	}
}

func TestRunRefusesWithoutAnSSHPort(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	h.out["ufw status verbose"] = ufwInactive
	h.ssh = nil
	s := &script{t: t, answers: []string{"y", "1", "", "0"}}
	Run(h.env(), s.ask)
	if len(h.execs) != 0 {
		t.Errorf("a plan without SSH ran: %v", h.execs)
	}
}

func TestRunReportsAFailedCommandAndDoesNotEnable(t *testing.T) {
	quiet(t)
	h := fullServer(t)
	h.out["ufw status verbose"] = ufwInactive
	h.execErr["ufw allow 22/tcp"] = errors.New("exit 1")
	s := &script{t: t, answers: []string{"y", "1", "", "y", "0"}}
	Run(h.env(), s.ask)
	if got := enableCommands(h.execs); len(got) != 0 {
		t.Errorf("enabled after a failure: %v", got)
	}
}

func TestRunIntrusionOffers(t *testing.T) {
	quiet(t)
	// Neither a firewall nor an intrusion tool: no changes.
	h := fullServer(t)
	Run(h.env(), (&script{t: t, answers: []string{"n", "0"}}).ask)
	if len(h.execs) != 0 {
		t.Errorf("execs = %v", h.execs)
	}
	// fail2ban without a firewall, consent given.
	h = fullServer(t)
	Run(h.env(), (&script{t: t, answers: []string{"n", "1", "y"}}).ask)
	if indexOf(h.execs, "apt-get install -y fail2ban") < 0 || h.files["/etc/fail2ban/jail.d/remnaforge.local"] == "" {
		t.Errorf("execs %v files %v", h.execs, h.files)
	}
	// CrowdSec declined at the consent question.
	h = fullServer(t)
	Run(h.env(), (&script{t: t, answers: []string{"n", "2", "n"}}).ask)
	if len(h.execs) != 0 {
		t.Errorf("a declined CrowdSec install ran: %v", h.execs)
	}
	// An answer that is neither choice installs nothing.
	h = fullServer(t)
	Run(h.env(), (&script{t: t, answers: []string{"n", "7"}}).ask)
	if len(h.execs) != 0 {
		t.Errorf("execs = %v", h.execs)
	}
}
