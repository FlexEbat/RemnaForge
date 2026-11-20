package firewall

import (
	"errors"
	"strings"
	"testing"
)

// capture collects what the package prints.
func capture(t *testing.T) *[]string {
	t.Helper()
	var lines []string
	old := out
	out = func(_, msg string) { lines = append(lines, msg) }
	t.Cleanup(func() { out = old })
	return &lines
}

var httpRule = Rule{Args: []string{"allow", "80/tcp", "comment", "HTTP"}, Reason: "HTTP"}
var nodeRule = Rule{Args: []string{"allow", "from", "203.0.113.5", "to", "any", "port", "2222"}, Reason: "panel to node"}

func ufwHost(t *testing.T, status string) *host {
	h := newHost(t)
	h.tools["ufw"] = true
	h.out["ufw status"] = status
	return h
}

func TestAllowRulesChangesNothingBeforeAYes(t *testing.T) {
	lines := capture(t)
	h := ufwHost(t, ufwActive)
	s := &script{t: t, answers: []string{"y"}}
	AllowRules(h.env(), s.ask, []Rule{httpRule, nodeRule})

	want := "ufw allow 80/tcp comment HTTP|ufw allow from 203.0.113.5 to any port 2222|ufw reload"
	if got := strings.Join(h.execs, "|"); got != want {
		t.Errorf("execs = %s", got)
	}
	grantedAt, firstExec := -1, -1
	for i, l := range h.timeline {
		if strings.Contains(l, "granted: ufw rules") {
			grantedAt = i
		}
		if strings.HasPrefix(l, "exec ") && firstExec < 0 {
			firstExec = i
		}
	}
	if grantedAt < 0 || firstExec < grantedAt {
		t.Errorf("the rules ran before the consent was recorded: %v", h.timeline)
	}
	shown := strings.Join(*lines, "\n")
	for _, w := range []string{"ufw allow 80/tcp comment HTTP", "(panel to node)"} {
		if !strings.Contains(shown, w) {
			t.Errorf("the operator was not shown %q:\n%s", w, shown)
		}
	}
}

func TestAllowRulesDeclinedAddsNothingAndPrintsTheCommands(t *testing.T) {
	for _, answer := range []string{"n", "", "no", "maybe"} {
		lines := capture(t)
		h := ufwHost(t, ufwActive)
		AllowRules(h.env(), (&script{t: t, answers: []string{answer}}).ask, []Rule{httpRule})
		if len(h.execs) != 0 {
			t.Errorf("answer %q still ran %v", answer, h.execs)
		}
		if len(h.consent) != 1 || !strings.Contains(h.consent[0], "declined: ufw rules") {
			t.Errorf("answer %q: consent log = %v", answer, h.consent)
		}
		manual := 0
		for _, l := range *lines {
			if l == "  ufw allow 80/tcp comment HTTP" {
				manual++
			}
		}
		if manual != 1 {
			t.Errorf("answer %q: the manual command was printed %d times:\n%s", answer, manual, strings.Join(*lines, "\n"))
		}
	}
}

func TestAllowRulesWithoutUFWDoesNotAskOrRun(t *testing.T) {
	lines := capture(t)
	h := newHost(t)
	AllowRules(h.env(), (&script{t: t}).ask, []Rule{httpRule})
	if len(h.execs) != 0 || len(h.consent) != 0 {
		t.Errorf("execs %v consent %v", h.execs, h.consent)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "ufw is not installed") {
		t.Errorf("the missing ufw was not reported:\n%s", strings.Join(*lines, "\n"))
	}
}

func TestAllowRulesSaysWhenUFWIsNotActive(t *testing.T) {
	lines := capture(t)
	h := ufwHost(t, ufwInactive)
	AllowRules(h.env(), (&script{t: t, answers: []string{"n"}}).ask, []Rule{httpRule})
	if !strings.Contains(strings.Join(*lines, "\n"), "not active") {
		t.Errorf("an inactive ufw was not mentioned:\n%s", strings.Join(*lines, "\n"))
	}
}

func TestAllowRulesStopsAtAFailureAndPrintsTheCommands(t *testing.T) {
	lines := capture(t)
	h := ufwHost(t, ufwActive)
	h.execErr["ufw allow 80/tcp comment HTTP"] = errors.New("boom")
	AllowRules(h.env(), (&script{t: t, answers: []string{"y"}}).ask, []Rule{httpRule, nodeRule})
	if got := strings.Join(h.execs, "|"); got != "ufw allow 80/tcp comment HTTP" {
		t.Errorf("execs after a failure = %s", got)
	}
	shown := strings.Join(*lines, "\n")
	if !strings.Contains(shown, "boom") || !strings.Contains(shown, "  ufw allow from 203.0.113.5 to any port 2222") {
		t.Errorf("the failure or the manual commands are missing:\n%s", shown)
	}
	if strings.Contains(shown, "rules are added") {
		t.Errorf("a failed run reported success:\n%s", shown)
	}
}

func TestAllowRulesIsARefusalWhenTheConsentCannotBeRecorded(t *testing.T) {
	capture(t)
	h := ufwHost(t, ufwActive)
	h.logFail = true
	AllowRules(h.env(), (&script{t: t, answers: []string{"y"}}).ask, []Rule{httpRule})
	if len(h.execs) != 0 {
		t.Errorf("a change that cannot be traced was made: %v", h.execs)
	}
}

func TestAllowRulesWithNoRulesAsksNothing(t *testing.T) {
	capture(t)
	h := ufwHost(t, ufwActive)
	AllowRules(h.env(), (&script{t: t}).ask, nil)
	if len(h.execs) != 0 {
		t.Errorf("execs %v", h.execs)
	}
}
